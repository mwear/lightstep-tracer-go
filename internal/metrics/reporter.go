package metrics

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/collectorpb"
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/metricspb"
)

const (
	// DefaultReporterAddress             = "https://metricingest.lightstep.com"
	DefaultReporterAddress             = "http://localhost:9876"
	DefaultReporterTimeout             = time.Second * 5
	DefaultReporterMeasurementDuration = time.Second * 30
)

var (
	acceptHeader      = http.CanonicalHeaderKey("Accept")
	contentTypeHeader = http.CanonicalHeaderKey("Content-Type")
	accessTokenHeader = http.CanonicalHeaderKey("Lightstep-Access-Token")
)

const (
	reporterPath = "/metrics"

	idempotencyKeyByteLength = 30
	protoContentType         = "application/octet-stream"

	ReporterPlatformKey        = "lightstep.reporter_platform"
	ReporterPlatformVersionKey = "lightstep.reporter_platform_version"
	ReporterVersionKey         = "lightstep.reporter_version"
)

type Reporter struct {
	client            *http.Client
	tracerID          uint64
	attributes        map[string]string
	address           string
	timeout           time.Duration
	accessToken       string
	stored            Metrics
	intervals         int
	collectorReporter *collectorpb.Reporter
	labels            []*collectorpb.KeyValue
	Start             time.Time
	End               time.Time
	MetricsCount      int
}

func attributesToTags(attributes map[string]string) []*collectorpb.KeyValue {
	tags := []*collectorpb.KeyValue{}
	for k, v := range attributes {
		tags = append(tags, &collectorpb.KeyValue{Key: k, Value: &collectorpb.KeyValue_StringValue{StringValue: v}})
	}
	return tags
}

func getLabels(attributes map[string]string) []*collectorpb.KeyValue {
	labels := []*collectorpb.KeyValue{}
	filters := []string{
		"lightstep.component_name",
		"service.version",
		"lightstep.hostname",
	}
	for k, v := range attributes {
		for _, l := range filters {
			if k == l {
				labels = append(labels, &collectorpb.KeyValue{Key: k, Value: &collectorpb.KeyValue_StringValue{StringValue: v}})
				break
			}
		}
	}
	return labels
}

func NewReporter(opts ...ReporterOption) *Reporter {
	c := newConfig(opts...)

	return &Reporter{
		client:      &http.Client{},
		tracerID:    c.tracerID,
		attributes:  c.attributes,
		address:     fmt.Sprintf("%s%s", c.address, reporterPath),
		timeout:     c.timeout,
		accessToken: c.accessToken,
		intervals:   1,
		collectorReporter: &collectorpb.Reporter{
			ReporterId: c.tracerID,
			Tags:       attributesToTags(c.attributes),
		},
		labels: getLabels(c.attributes),
	}
}

func (r *Reporter) prepareRequest(m Metrics) (*metricspb.IngestRequest, error) {
	idempotencyKey, err := generateIdempotencyKey()
	if err != nil {
		return nil, err
	}
	return &metricspb.IngestRequest{
		IdempotencyKey: idempotencyKey,
		Reporter:       r.collectorReporter,
	}, nil
}

func (r *Reporter) addFloat(key string, value float64, kind metricspb.MetricKind) *metricspb.MetricPoint {
	return &metricspb.MetricPoint{
		Kind:       kind,
		MetricName: key,
		Labels:     r.labels,
		Value: &metricspb.MetricPoint_DoubleValue{
			DoubleValue: value,
		},
		Start: &types.Timestamp{
			Seconds: r.Start.Unix(),
			Nanos:   int32(r.Start.Nanosecond()),
		},
		Duration: &types.Duration{
			Seconds: int64(DefaultReporterMeasurementDuration.Seconds()), // TODO: set duration to number of retries * flush interval
		},
	}
}

// Measure takes a snapshot of system metrics and sends them
// to a LightStep endpoint.
func (r *Reporter) Measure(ctx context.Context) error {
	start := time.Now()
	r.Start = start
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	m, err := Measure(ctx, 0*time.Second)
	if err != nil {
		return err
	}

	pb, err := r.prepareRequest(m)
	if err != nil {
		return err
	}

	pb.Points = append(pb.Points, r.addFloat("runtime.go.cpu.user", m.ProcessCPU.User-r.stored.ProcessCPU.User, metricspb.MetricKind_COUNTER))
	pb.Points = append(pb.Points, r.addFloat("runtime.go.cpu.sys", m.ProcessCPU.System-r.stored.ProcessCPU.System, metricspb.MetricKind_COUNTER))
	pb.Points = append(pb.Points, r.addFloat("runtime.go.gc.count", float64(m.GarbageCollector.NumGC-r.stored.GarbageCollector.NumGC), metricspb.MetricKind_COUNTER))

	pb.Points = append(pb.Points, r.addFloat("mem.available", float64(m.Memory.Available), metricspb.MetricKind_GAUGE))
	pb.Points = append(pb.Points, r.addFloat("mem.total", float64(m.Memory.Used), metricspb.MetricKind_GAUGE))
	pb.Points = append(pb.Points, r.addFloat("runtime.go.mem.heap_alloc", float64(m.Memory.HeapAlloc), metricspb.MetricKind_GAUGE))

	for label, cpu := range m.CPU {
		pb.Points = append(pb.Points, r.addFloat("cpu.sys", cpu.System-r.stored.CPU[label].System, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, r.addFloat("cpu.user", cpu.User-r.stored.CPU[label].User, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, r.addFloat("cpu.total", cpu.Total-r.stored.CPU[label].Total, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, r.addFloat("cpu.usage", cpu.Usage-r.stored.CPU[label].Usage, metricspb.MetricKind_COUNTER))
	}
	for label, nic := range m.NIC {
		pb.Points = append(pb.Points, r.addFloat("net.bytes_recv", float64(nic.BytesReceived-r.stored.NIC[label].BytesReceived), metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, r.addFloat("net.bytes_sent", float64(nic.BytesSent-r.stored.NIC[label].BytesSent), metricspb.MetricKind_COUNTER))
	}

	// fmt.Println(proto.MarshalTextString(pb))
	b, err := proto.Marshal(pb)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, r.address, bytes.NewReader(b))
	if err != nil {
		return err
	}

	req = req.WithContext(ctx)

	req.Header.Set(contentTypeHeader, protoContentType)
	req.Header.Set(acceptHeader, protoContentType)
	req.Header.Set(accessTokenHeader, r.accessToken)

	res, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	r.stored = m
	r.MetricsCount = len(pb.Points)
	r.End = time.Now()
	return nil
}

type ReporterOption func(*config)

func WithReporterTracerID(tracerID uint64) ReporterOption {
	return func(c *config) {
		c.tracerID = tracerID
	}
}

func WithReporterAttributes(attributes map[string]string) ReporterOption {
	return func(c *config) {
		c.attributes = make(map[string]string, len(attributes))
		for k, v := range attributes {
			c.attributes[k] = v
		}
	}
}

// WithReporterAddress sets the address of the LightStep endpoint
func WithReporterAddress(address string) ReporterOption {
	return func(c *config) {
		c.address = address
	}
}

func WithReporterTimeout(timeout time.Duration) ReporterOption {
	return func(c *config) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

func WithReporterMeasurementDuration(measurementDuration time.Duration) ReporterOption {
	return func(c *config) {
		if measurementDuration > 0 {
			c.measurementDuration = measurementDuration
		}
	}
}

// WithReporterAccessToken sets an access token for communicating with LightStep
func WithReporterAccessToken(accessToken string) ReporterOption {
	return func(c *config) {
		c.accessToken = accessToken
	}
}

type config struct {
	tracerID            uint64
	attributes          map[string]string
	address             string
	timeout             time.Duration
	measurementDuration time.Duration
	accessToken         string
}

func newConfig(opts ...ReporterOption) config {
	var c config

	defaultOpts := []ReporterOption{
		WithReporterAttributes(make(map[string]string)),
		WithReporterAddress(DefaultReporterAddress),
		WithReporterTimeout(DefaultReporterTimeout),
		WithReporterMeasurementDuration(DefaultReporterMeasurementDuration),
	}

	for _, opt := range append(defaultOpts, opts...) {
		opt(&c)
	}

	return c
}

func generateIdempotencyKey() (string, error) {
	b := make([]byte, idempotencyKeyByteLength)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}

	return strings.ToLower(base32.StdEncoding.EncodeToString(b)), nil
}
