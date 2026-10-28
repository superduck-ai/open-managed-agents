package telemetry

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func LatencyBuckets() []float64 {
	return []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
}

func LongWaitBuckets() []float64 {
	return []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 7200, 21600, 86400}
}

func TokenBuckets() []float64 {
	return []float64{16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576}
}

type Instruments[T any] struct {
	build func(metric.Meter) T
	bound atomic.Pointer[boundInstruments[T]]
}

type boundInstruments[T any] struct {
	provider metric.MeterProvider
	value    T
}

func NewInstruments[T any](build func(metric.Meter) T) *Instruments[T] {
	return &Instruments[T]{build: build}
}

func (i *Instruments[T]) Get() T {
	provider := otel.GetMeterProvider()
	if bound := i.bound.Load(); bound != nil && bound.provider == provider {
		return bound.value
	}
	bound := &boundInstruments[T]{provider: provider, value: i.build(provider.Meter(InstrumentationName))}
	i.bound.Store(bound)
	return bound.value
}

func Counter(meter metric.Meter, name, unit, description string) metric.Int64Counter {
	instrument, err := meter.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		otel.Handle(err)
		return noop.Int64Counter{}
	}
	return instrument
}

func UpDownCounter(meter metric.Meter, name, unit, description string) metric.Int64UpDownCounter {
	instrument, err := meter.Int64UpDownCounter(name, metric.WithUnit(unit), metric.WithDescription(description))
	if err != nil {
		otel.Handle(err)
		return noop.Int64UpDownCounter{}
	}
	return instrument
}

func Histogram(meter metric.Meter, name, unit, description string, buckets []float64) metric.Float64Histogram {
	instrument, err := meter.Float64Histogram(name,
		metric.WithUnit(unit),
		metric.WithDescription(description),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		otel.Handle(err)
		return noop.Float64Histogram{}
	}
	return instrument
}

func Int64Histogram(meter metric.Meter, name, unit, description string, buckets []float64) metric.Int64Histogram {
	instrument, err := meter.Int64Histogram(name,
		metric.WithUnit(unit),
		metric.WithDescription(description),
		metric.WithExplicitBucketBoundaries(buckets...),
	)
	if err != nil {
		otel.Handle(err)
		return noop.Int64Histogram{}
	}
	return instrument
}

func RecordWait(ctx context.Context, histogram metric.Float64Histogram, wait time.Duration, options ...metric.RecordOption) {
	if wait < 0 {
		return
	}
	histogram.Record(ctx, wait.Seconds(), options...)
}
