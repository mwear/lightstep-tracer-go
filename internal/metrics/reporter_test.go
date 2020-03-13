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

var _ = Describe("Reporter", func() {
	var reporter *metrics.Reporter
	var ingestRequest metricspb.IngestRequest

	JustBeforeEach(func() {
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := ioutil.ReadAll(r.Body)
			err := proto.Unmarshal(body, &ingestRequest)
			if !Expect(err).To(BeNil()) {
				return
			}
		})
		s := httptest.NewServer(h)
		url := fmt.Sprintf("http://%s", s.Listener.Addr().String())
		reporter = metrics.NewReporter(
			metrics.WithReporterAddress(url),
		)
	})
	Describe("Measure", func() {
		It("should return an IngestRequest", func() {
			// initial report always gets skipped
			err := reporter.Measure(context.Background(), 1)
			Expect(err).To(BeNil())
			Expect(ingestRequest.GetPoints()).To(HaveLen(0))

			err = reporter.Measure(context.Background(), 1)
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
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})
			s := httptest.NewServer(h)
			url := fmt.Sprintf("http://%s", s.Listener.Addr().String())
			reporter = metrics.NewReporter(
				metrics.WithReporterAddress(url),
			)
			reporter.Measure(context.Background(), 1)
			err := reporter.Measure(context.Background(), 1)
			Expect(err).To(Not(BeNil()))
			Expect(err.Error()).To(ContainSubstring("404"))
		})
	})
	Describe("Measure fails retryably", func() {
		It("should return after retrying", func() {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			})
			s := httptest.NewServer(h)
			url := fmt.Sprintf("http://%s", s.Listener.Addr().String())
			reporter = metrics.NewReporter(
				metrics.WithReporterAddress(url),
				metrics.WithReporterTimeout(200*time.Millisecond),
			)
			reporter.Measure(context.Background(), 1)
			err := reporter.Measure(context.Background(), 1)
			Expect(err).To(Not(BeNil()))
			Expect(err.Error()).To(ContainSubstring("context deadline exceeded"))
		})
	})
})

func TestLightstepMetricsGo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LightstepMetricsGo Suite")
}
