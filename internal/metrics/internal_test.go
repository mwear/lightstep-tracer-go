package metrics

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("the reporter", func() {
	ginkgo.Describe("configuration", func() {
		ginkgo.It("uses default values", func() {
			defaultReporter := NewReporter()
			gomega.Expect(defaultReporter.tracerID).To(gomega.Equal(uint64(0)))
			gomega.Expect(defaultReporter.attributes).To(gomega.Equal(map[string]string{}))
			gomega.Expect(defaultReporter.address).To(gomega.Equal(fmt.Sprintf("%s%s", defaultReporterAddress, reporterPath)))
			gomega.Expect(defaultReporter.timeout).To(gomega.Equal(defaultReporterTimeout))
			gomega.Expect(defaultReporter.measurementDuration).To(gomega.Equal(defaultReporterMeasurementDuration))
			gomega.Expect(defaultReporter.accessToken).To(gomega.Equal(""))
		})
		ginkgo.It("uses configured values", func() {
			duration := time.Second * 1
			accessToken := "token-1234"
			tracerID := uint64(1234)
			attributes := map[string]string{
				"key1": "val1",
				"key2": "val2",
			}
			address := "http://localhost:8080"
			defaultReporter := NewReporter(
				WithReporterTracerID(tracerID),
				WithReporterAttributes(attributes),
				WithReporterAddress(address),
				WithReporterTimeout(duration),
				WithReporterMeasurementDuration(duration),
				WithReporterAccessToken(accessToken),
			)
			gomega.Expect(defaultReporter.tracerID).To(gomega.Equal(tracerID))
			gomega.Expect(defaultReporter.attributes).To(gomega.Equal(attributes))
			gomega.Expect(defaultReporter.address).To(gomega.Equal(fmt.Sprintf("%s%s", address, reporterPath)))
			gomega.Expect(defaultReporter.timeout).To(gomega.Equal(duration))
			gomega.Expect(defaultReporter.measurementDuration).To(gomega.Equal(duration))
			gomega.Expect(defaultReporter.accessToken).To(gomega.Equal(accessToken))
		})
	})
})
