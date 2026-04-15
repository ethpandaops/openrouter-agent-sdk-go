package observability

import (
	promclient "github.com/prometheus/client_golang/prometheus"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// NewPrometheusMeterProvider creates an OTel MeterProvider backed by
// a Prometheus registerer. This bridges OTel metrics into Prometheus
// format for consumers that already have a prometheus.Registerer.
func NewPrometheusMeterProvider(reg promclient.Registerer) (metric.MeterProvider, error) {
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(reg),
	)
	if err != nil {
		return nil, err
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
	), nil
}
