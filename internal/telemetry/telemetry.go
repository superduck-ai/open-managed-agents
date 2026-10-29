package telemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const InstrumentationName = "github.com/superduck-ai/open-managed-agents"

type Runtime struct {
	loggerProvider *sdklog.LoggerProvider
	shutdowns      []func(context.Context) error
}

func Setup(ctx context.Context, cfg config.TelemetryConfig, environment string, errorLogger *slog.Logger) (*Runtime, error) {
	runtime := &Runtime{}
	if !cfg.Enabled {
		return runtime, nil
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		errorLogger.WarnContext(context.Background(), "telemetry export failed", "error", err)
	}))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	res := newResource(cfg, environment)
	if err := runtime.setupProviders(ctx, cfg, res); err != nil {
		return nil, errors.Join(err, runtime.Shutdown(ctx))
	}
	return runtime, nil
}

func (r *Runtime) setupProviders(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) error {
	if cfg.Traces.Enabled {
		provider, err := newTracerProvider(ctx, cfg, res)
		if err != nil {
			return err
		}
		otel.SetTracerProvider(provider)
		r.shutdowns = append(r.shutdowns, provider.Shutdown)
	}
	if cfg.Metrics.Enabled {
		provider, err := newMeterProvider(ctx, cfg, res)
		if err != nil {
			return err
		}
		otel.SetMeterProvider(provider)
		r.shutdowns = append(r.shutdowns, provider.Shutdown)
	}
	if cfg.Logs.Enabled {
		provider, err := newLoggerProvider(ctx, cfg, res)
		if err != nil {
			return err
		}
		global.SetLoggerProvider(provider)
		r.loggerProvider = provider
		r.shutdowns = append(r.shutdowns, provider.Shutdown)
	}
	return nil
}

func (r *Runtime) LogHandler() slog.Handler {
	if r.loggerProvider == nil {
		return nil
	}
	return newScalarValueHandler(otelslog.NewHandler(InstrumentationName, otelslog.WithLoggerProvider(r.loggerProvider)))
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	var errs []error
	for index := len(r.shutdowns) - 1; index >= 0; index-- {
		errs = append(errs, r.shutdowns[index](ctx))
	}
	r.shutdowns = nil
	return errors.Join(errs...)
}

func newTracerProvider(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(otlpURL(cfg.OTLP, "traces")),
		otlptracehttp.WithHeaders(otlpHeaders(cfg.OTLP, cfg.OTLP.TracesStream)),
		otlptracehttp.WithTimeout(cfg.OTLP.Timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	), nil
}

func newMeterProvider(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(otlpURL(cfg.OTLP, "metrics")),
		otlpmetrichttp.WithHeaders(otlpHeaders(cfg.OTLP, "")),
		otlpmetrichttp.WithTimeout(cfg.OTLP.Timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}
	return newRuntimeMeterProvider(sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(cfg.Metrics.ExportInterval),
		sdkmetric.WithProducer(otelruntime.NewProducer()),
	), res)
}

func newRuntimeMeterProvider(reader sdkmetric.Reader, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
	if err := otelruntime.Start(otelruntime.WithMeterProvider(provider)); err != nil {
		return nil, errors.Join(fmt.Errorf("start runtime metrics: %w", err), provider.Shutdown(context.Background()))
	}
	return provider, nil
}

func newLoggerProvider(ctx context.Context, cfg config.TelemetryConfig, res *resource.Resource) (*sdklog.LoggerProvider, error) {
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(otlpURL(cfg.OTLP, "logs")),
		otlploghttp.WithHeaders(otlpHeaders(cfg.OTLP, cfg.OTLP.LogsStream)),
		otlploghttp.WithTimeout(cfg.OTLP.Timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("create log exporter: %w", err)
	}
	return sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	), nil
}

func otlpURL(cfg config.TelemetryOTLPConfig, signal string) string {
	return strings.TrimRight(cfg.BaseURL, "/") + "/api/" + url.PathEscape(cfg.Organization) + "/v1/" + signal
}

func otlpHeaders(cfg config.TelemetryOTLPConfig, stream string) map[string]string {
	headers := map[string]string{}
	if cfg.Username != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		headers["Authorization"] = "Basic " + credentials
	}
	if stream != "" {
		headers["stream-name"] = stream
	}
	return headers
}

func newResource(cfg config.TelemetryConfig, environment string) *resource.Resource {
	return resource.NewWithAttributes("",
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", buildVersion()),
		attribute.String("service.instance.id", instanceID()),
		attribute.String("deployment.environment.name", environment),
	)
}

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value
		}
	}
	return "unknown"
}

func instanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return host + ":" + strconv.Itoa(os.Getpid())
}
