package metrics_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/lightstep/lightstep-tracer-common/golang/gogo/metricspb"
	"github.com/lightstep/lightstep-tracer-go/internal/metrics"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var ingestRequest metricspb.IngestRequest
var server *httptest.Server
var url string
var statusCode int

var _ = BeforeSuite(func() {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		if statusCode != 0 {
			status = statusCode
		}
		w.WriteHeader(status)
		body, _ := ioutil.ReadAll(r.Body)
		err := proto.Unmarshal(body, &ingestRequest)
		if !Expect(err).To(BeNil()) {
			return
		}
	})
	server = httptest.NewServer(h)
	url = fmt.Sprintf("http://%s", server.Listener.Addr().String())
})

var _ = AfterSuite(func() {
	server.Close()
})

var _ = Describe("Reporter", func() {
	var reporter *metrics.Reporter

	BeforeEach(func() {
		reporter = metrics.NewReporter(
			metrics.WithReporterAddress(url),
		)
		ingestRequest = metricspb.IngestRequest{}
	})

	Describe("Measure", func() {
		It("should return an IngestRequest", func() {
			err := reporter.Measure(context.Background(), 1)
			if !Expect(err).To(BeNil()) {
				return
			}
			// check expected metrics are present and of the right type
			points := ingestRequest.GetPoints()

			expected := map[string]interface{}{
				"cpu.user":                  metricspb.MetricKind_COUNTER,
				"cpu.sys":                   metricspb.MetricKind_COUNTER,
				"cpu.usage":                 metricspb.MetricKind_COUNTER,
				"cpu.total":                 metricspb.MetricKind_COUNTER,
				"net.bytes_sent":            metricspb.MetricKind_COUNTER,
				"net.bytes_recv":            metricspb.MetricKind_COUNTER,
				"mem.total":                 metricspb.MetricKind_GAUGE,
				"mem.available":             metricspb.MetricKind_GAUGE,
				"runtime.go.cpu.user":       metricspb.MetricKind_COUNTER,
				"runtime.go.cpu.sys":        metricspb.MetricKind_COUNTER,
				"runtime.go.mem.heap_alloc": metricspb.MetricKind_GAUGE,
				"runtime.go.gc.count":       metricspb.MetricKind_COUNTER,
				"runtime.go.goroutine":      metricspb.MetricKind_GAUGE,
			}
			Expect(points).To(HaveLen(len(expected)))
			for _, point := range points {
				name := point.GetMetricName()
				Expect(point.Kind).To(Equal(expected[name]))
			}
		})
	})
	Describe("Measure fails unretryably", func() {
		It("should return an error", func() {
			var unretryable = []struct {
				code     int
				expected string
			}{
				{http.StatusBadRequest, fmt.Sprintf("%d", http.StatusBadRequest)},
				{http.StatusUnauthorized, fmt.Sprintf("%d", http.StatusUnauthorized)},
				{http.StatusForbidden, fmt.Sprintf("%d", http.StatusForbidden)},
				{http.StatusNotFound, fmt.Sprintf("%d", http.StatusNotFound)},
				{http.StatusNotImplemented, fmt.Sprintf("%d", http.StatusNotImplemented)},
			}
			for _, t := range unretryable {
				statusCode = t.code
				err := reporter.Measure(context.Background(), 1)
				Expect(err).To(Not(BeNil()))
				Expect(err.Error()).To(ContainSubstring(t.expected))
			}
		})
	})
	Describe("Measure fails retryably", func() {
		It("should return after retrying", func() {
			var retryable = []struct {
				code     int
				expected string
			}{
				{http.StatusTooManyRequests, "context deadline exceeded"},
				{http.StatusBadGateway, "context deadline exceeded"},
				{http.StatusGatewayTimeout, "context deadline exceeded"},
				{http.StatusServiceUnavailable, "context deadline exceeded"},
				{http.StatusRequestTimeout, "context deadline exceeded"},
			}
			reporter = metrics.NewReporter(
				metrics.WithReporterAddress(url),
				metrics.WithReporterTimeout(10*time.Millisecond),
			)
			for _, t := range retryable {
				statusCode = t.code
				err := reporter.Measure(context.Background(), 1)
				Expect(err).To(Not(BeNil()))
				Expect(err.Error()).To(ContainSubstring(t.expected))
			}
		})
	})
	Describe("Duration exceeds max", func() {
		It("should not send a report", func() {
			tenMinutesOfIntervals := int64(21) // 21 * 30s > 10min
			err := reporter.Measure(context.Background(), tenMinutesOfIntervals)
			if !Expect(err).To(BeNil()) {
				return
			}
			// check expected metrics are present and of the right type
			points := ingestRequest.GetPoints()
			Expect(points).To(HaveLen(0))
		})
	})
})

func TestLightstepMetricsGo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LightstepMetricsGo Suite")
}
