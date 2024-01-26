package main

import (
	"context"
	"fmt"
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

	pongEndpoint := os.Getenv("PONG_ENDPOINT")
	address := os.Getenv("ADDRESS")
	traceBackendEndpoint := os.Getenv("JAEGER_ADDRESS")
	ctx := context.Background()

	// Create the resource to be observed
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("Ping"),
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

	tracer := traceProvider.Tracer("Ping")

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
		"wesovilabs.com/tutorial/opentelemetry/ping/manual-instrumentation",
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
	http.HandleFunc("/ping", func(w http.ResponseWriter, req *http.Request) {
		ctx, span := tracer.Start(context.Background(), "ping-request")
		span.SetAttributes(attribute.String("environment", "staging"))
		defer span.End()
		span.AddEvent("Start request processing")
		requestStartTime := time.Now()
		span.AddEvent("Invoke external endpoint")
		if _, err := http.Get(fmt.Sprintf("http://%s/pong", pongEndpoint)); err != nil {
			span.AddEvent("Response with error")
			w.Write([]byte(err.Error()))
		} else {
			span.AddEvent("Response success")
			w.Write([]byte("ok"))
		}
		elapsedTime := float64(time.Since(requestStartTime)) / float64(time.Millisecond)
		// Record measurements
		attrs := metric.WithAttributes(attribute.String("remoteAddr", req.RemoteAddr), attribute.String("userAgent", req.UserAgent()))
		span.AddEvent("Update metrics")
		counter.Add(ctx, 1, attrs)
		hist.Record(ctx, elapsedTime, attrs)
	})

	http.ListenAndServe(address, nil)

}
