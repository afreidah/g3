// -------------------------------------------------------------------------------
// Tracing Tests
//
// Author: Alex Freidah
//
// Covers tracer initialization (disabled no-op and the enabled path across
// sampler branches), the span-creation helpers, and the attribute builders.
// -------------------------------------------------------------------------------

package telemetry

import (
	"context"
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func TestInitTracer_Disabled(t *testing.T) {
	shutdown, err := InitTracer(context.Background(), TracingConfig{Enabled: false})
	if err != nil {
		t.Fatalf("InitTracer err = %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown is nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown err = %v", err)
	}
}

func TestInitTracer_EnabledSamplers(t *testing.T) {
	// The OTLP gRPC exporter is non-blocking by default, so New succeeds
	// without a live collector. Exercise each sampler branch.
	for _, rate := range []float64{1.0, 0.0, 0.5} {
		cfg := TracingConfig{
			Enabled:    true,
			Endpoint:   "127.0.0.1:4317",
			Insecure:   true,
			SampleRate: rate,
		}
		shutdown, err := InitTracer(context.Background(), cfg)
		if err != nil {
			t.Fatalf("InitTracer(rate=%v) err = %v", rate, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := shutdown(ctx); err != nil {
			t.Errorf("shutdown(rate=%v) err = %v", rate, err)
		}
		cancel()
	}
}

func TestSpanHelpers(t *testing.T) {
	if Tracer() == nil {
		t.Fatal("Tracer() = nil")
	}

	ctx := context.Background()

	if _, span := StartSpan(ctx, "op", AttrBucket.String("b")); span == nil {
		t.Error("StartSpan span = nil")
	} else {
		span.End()
	}
	if _, span := StartServerSpan(ctx, "srv"); span == nil {
		t.Error("StartServerSpan span = nil")
	} else {
		span.End()
	}
	if _, span := StartClientSpan(ctx, "cli"); span == nil {
		t.Error("StartClientSpan span = nil")
	} else {
		span.End()
	}
}

func TestRequestAttributes(t *testing.T) {
	attrs := RequestAttributes("GET", "/bucket/key", "bucket", "key", "1.2.3.4")
	if len(attrs) != 5 {
		t.Fatalf("len = %d, want 5", len(attrs))
	}
	if attrs[0].Key != semconv.HTTPRequestMethodKey {
		t.Errorf("attrs[0].Key = %v, want HTTP method", attrs[0].Key)
	}
	if attrs[2] != AttrBucket.String("bucket") {
		t.Errorf("bucket attr = %v", attrs[2])
	}
}

func TestGmailAttributes(t *testing.T) {
	attrs := GmailAttributes("PutObject", "bucket", "key")
	if len(attrs) != 3 {
		t.Fatalf("len = %d, want 3", len(attrs))
	}
	if attrs[0] != AttrOperation.String("PutObject") {
		t.Errorf("operation attr = %v", attrs[0])
	}
	if attrs[1] != AttrBucket.String("bucket") {
		t.Errorf("bucket attr = %v", attrs[1])
	}
	if attrs[2] != AttrObjectKey.String("key") {
		t.Errorf("key attr = %v", attrs[2])
	}
}
