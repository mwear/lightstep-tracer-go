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
}

func attributesToTags(attributes map[string]string) []*collectorpb.KeyValue {
	tags := []*collectorpb.KeyValue{}
	for k, v := range attributes {
		tags = append(tags, &collectorpb.KeyValue{Key: k, Value: &collectorpb.KeyValue_StringValue{StringValue: v}})
	}
	return tags
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

func addFloat(labels []*collectorpb.KeyValue, key string, value float64, start time.Time, kind metricspb.MetricKind) *metricspb.MetricPoint {
	return &metricspb.MetricPoint{
		Kind:       kind,
		MetricName: key,
		Labels:     labels,
		Value: &metricspb.MetricPoint_DoubleValue{
			DoubleValue: value,
		},
		Start: &types.Timestamp{
			Seconds: start.Unix(),
			Nanos:   int32(start.Nanosecond()),
		},
		Duration: &types.Duration{
			Seconds: 0, // TODO: set duration to number of retries * flush interval
		},
	}
}

func addUint(labels []*collectorpb.KeyValue, key string, value uint64, start time.Time, kind metricspb.MetricKind) *metricspb.MetricPoint {
	return &metricspb.MetricPoint{
		Kind:       kind,
		MetricName: key,
		Labels:     labels,
		Value: &metricspb.MetricPoint_Uint64Value{
			Uint64Value: value,
		},
		Start: &types.Timestamp{
			Seconds: start.Unix(),
			Nanos:   int32(start.Nanosecond()),
		},
		Duration: &types.Duration{
			Seconds: 0, // TODO: set duration to number of retries * flush interval
		},
	}
}

// Measure takes a snapshot of system metrics and sends them
// to a LightStep endpoint.
func (r *Reporter) Measure(ctx context.Context) error {
	start := time.Now()
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

	labels := []*collectorpb.KeyValue{
		&collectorpb.KeyValue{
			Key:   "name",
			Value: &collectorpb.KeyValue_StringValue{StringValue: "process.cpu"},
		},
	}
	pb.Points = append(pb.Points, addFloat(labels, "runtime.go.cpu.user", m.ProcessCPU.User-r.stored.ProcessCPU.User, start, metricspb.MetricKind_COUNTER))
	pb.Points = append(pb.Points, addFloat(labels, "runtime.go.cpu.sys", m.ProcessCPU.System-r.stored.ProcessCPU.System, start, metricspb.MetricKind_COUNTER))
	pb.Points = append(pb.Points, addUint(labels, "runtime.go.gc.count", m.GarbageCollector.NumGC-r.stored.GarbageCollector.NumGC, start, metricspb.MetricKind_COUNTER))

	labels = []*collectorpb.KeyValue{
		&collectorpb.KeyValue{
			Key:   "name",
			Value: &collectorpb.KeyValue_StringValue{StringValue: "mem"},
		},
	}
	pb.Points = append(pb.Points, addUint(labels, "mem.available", m.Memory.Available, start, metricspb.MetricKind_GAUGE))
	pb.Points = append(pb.Points, addUint(labels, "mem.total", m.Memory.Used, start, metricspb.MetricKind_GAUGE))
	pb.Points = append(pb.Points, addUint(labels, "runtime.go.mem.heap_alloc", m.Memory.HeapAlloc, start, metricspb.MetricKind_GAUGE))
	pb.Points = append(pb.Points, addFloat(labels, "cpu.percent", m.CPUPercent, start, metricspb.MetricKind_GAUGE))

	for label, cpu := range m.CPU {
		labels = []*collectorpb.KeyValue{
			&collectorpb.KeyValue{
				Key:   "name",
				Value: &collectorpb.KeyValue_StringValue{StringValue: label},
			},
		}

		pb.Points = append(pb.Points, addFloat(labels, "cpu.sys", cpu.System-r.stored.CPU[label].System, start, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, addFloat(labels, "cpu.user", cpu.User-r.stored.CPU[label].User, start, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, addFloat(labels, "cpu.idle", cpu.Idle-r.stored.CPU[label].Idle, start, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, addFloat(labels, "cpu.steal", cpu.Steal-r.stored.CPU[label].Steal, start, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, addFloat(labels, "cpu.nice", cpu.Nice-r.stored.CPU[label].Nice, start, metricspb.MetricKind_COUNTER))
	}
	for label, nic := range m.NIC {
		labels = []*collectorpb.KeyValue{
			&collectorpb.KeyValue{
				Key:   "name",
				Value: &collectorpb.KeyValue_StringValue{StringValue: label},
			},
		}
		pb.Points = append(pb.Points, addUint(labels, "net.bytes_recv", nic.BytesReceived-r.stored.NIC[label].BytesReceived, start, metricspb.MetricKind_COUNTER))
		pb.Points = append(pb.Points, addUint(labels, "net.bytes_sent", nic.BytesSent-r.stored.NIC[label].BytesSent, start, metricspb.MetricKind_COUNTER))
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
