package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	api "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func main() {
	address := os.Getenv("ADDRESS")
	traceBackendEndpoint := os.Getenv("JAEGER_ADDRESS")

	ctx := context.Background()

	// Create the resource to be observed
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("Pong"),
			semconv.ServiceVersion("v0.0.1"),
		),
	)
	if err != nil {
		panic(err)
	}

	// Tracing configuration

	traceClient := otlptracehttp.NewClient(otlptracehttp.WithEndpoint(traceBackendEndpoint), otlptracehttp.WithInsecure(), otlptracehttp.WithCompression(otlptracehttp.NoCompression))
	traceExporter, err := otlptrace.New(ctx, traceClient)
	if err != nil {
		panic(err)
	}
	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter, trace.WithBatchTimeout(2*time.Second)),
		trace.WithResource(res),
	)
	defer func() { _ = traceProvider.Shutdown(ctx) }()
	otel.SetTracerProvider(traceProvider)

	tracer := traceProvider.Tracer("Pong")

	// Metric configuration

	prometheusExporter, err := prometheus.New()
	if err != nil {
		panic(err)
	}
	meterProvider := api.NewMeterProvider(
		api.WithResource(res),
		api.WithReader(prometheusExporter),
	)
	meter := otel.Meter(
		"wesovilabs.com/tutorial/opentelemetry/pong/manual-instrumentation",
		metric.WithInstrumentationVersion("v0.0.1"),
	)
	counter, err := meter.Int64Counter(
		"request_count",
		metric.WithDescription("Incoming request count"),
		metric.WithUnit("request"),
	)
	if err != nil {
		log.Fatalln(err)
	}
	hist, err := meter.Float64Histogram(
		"duration",
		metric.WithDescription("Incoming end to end duration"),
		metric.WithUnit("milliseconds"),
	)
	if err != nil {
		log.Fatalln(err)
	}

	defer func() { _ = meterProvider.Shutdown(ctx) }()
	otel.SetMeterProvider(meterProvider)

	// HTTP Endpoints
	//Used to expose metrics in prometheus format
	http.Handle("/metrics", promhttp.Handler())
	// Endpoint to be observer
	http.HandleFunc("/pong", func(w http.ResponseWriter, req *http.Request) {
		ctx, span := tracer.Start(context.Background(), "pong-request")
		span.SetAttributes(attribute.String("environment", "staging"))
		defer span.End()
		requestStartTime := time.Now()
		span.AddEvent("Response success")
		_, _ = w.Write([]byte("pong"))

		elapsedTime := float64(time.Since(requestStartTime)) / float64(time.Millisecond)

		// Record measurements
		span.AddEvent("Update metrics")
		attrs := metric.WithAttributes(attribute.String("remoteAddr", req.RemoteAddr), attribute.String("userAgent", req.UserAgent()))
		counter.Add(ctx, 1, attrs)
		hist.Record(ctx, elapsedTime, attrs)
	})

	http.ListenAndServe(address, nil)

}
