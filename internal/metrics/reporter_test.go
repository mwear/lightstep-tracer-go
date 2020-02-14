package metrics_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

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
			// metric := metrics.Metrics{}
			err := reporter.Measure(context.Background())
			if !Expect(err).To(BeNil()) {
				return
			}
			// check expected metrics are present and of the right type
			points := ingestRequest.GetPoints()

			expected := map[string]interface{}{
				"cpu.user":                  metricspb.MetricKind_COUNTER,
				"cpu.sys":                   metricspb.MetricKind_COUNTER,
				"cpu.idle":                  metricspb.MetricKind_COUNTER,
				"cpu.nice":                  metricspb.MetricKind_COUNTER,
				"cpu.steal":                 metricspb.MetricKind_COUNTER,
				"cpu.percent":               metricspb.MetricKind_GAUGE,
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

			// TODO: check that we're sending a delta
		})
	})
})

func TestLightstepMetricsGo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LightstepMetricsGo Suite")
}
