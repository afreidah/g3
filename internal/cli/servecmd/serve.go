// -------------------------------------------------------------------------------
// g3 - Serve Subcommand
//
// Author: Alex Freidah
//
// Starts the S3 gateway server: loads configuration, initializes logging and
// tracing, connects the Gmail backend, and serves the HTTP API with graceful
// shutdown on SIGINT/SIGTERM. The health/readiness handlers and mux assembly
// are split into pure functions so they are unit-testable; Run is the daemon
// orchestration.
// -------------------------------------------------------------------------------

package servecmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/afreidah/g3/internal/audit"
	"github.com/afreidah/g3/internal/auth"
	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/cli"
	"github.com/afreidah/g3/internal/config"
	"github.com/afreidah/g3/internal/server"
	"github.com/afreidah/g3/internal/telemetry"
)

// Run starts the g3 server and blocks until a shutdown signal, returning the
// process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", cli.DefaultConfigPath, "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "failed to load config: %v\n", err)
		return 1
	}

	// Structured logging with trace correlation.
	var logLevel slog.LevelVar
	logLevel.Set(config.ParseLogLevel(cfg.Server.LogLevel))
	logBuffer := telemetry.NewLogBuffer()
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &logLevel})
	slog.SetDefault(slog.New(telemetry.NewTeeHandler(telemetry.NewTraceHandler(jsonHandler), logBuffer)))

	slog.InfoContext(ctx, "Starting g3", "version", telemetry.Version, "go_version", runtime.Version(), "listen", cfg.Server.ListenAddr)

	shutdownTracer, err := telemetry.InitTracer(ctx, cfg.Telemetry.Tracing)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize tracer: %v\n", err)
		return 1
	}

	audit.OnEvent = func(event string) {
		telemetry.AuditEventsTotal.WithLabelValues(event).Inc()
	}
	telemetry.BuildInfo.WithLabelValues(telemetry.Version, runtime.Version()).Set(1)

	metadataStore, closeStore, err := cli.InitMetadataStore(ctx, &cfg.Database)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize metadata store: %v\n", err)
		return 1
	}
	defer closeStore()

	gmailBackend, err := backend.NewGmailBackend(ctx, &cfg.Gmail, metadataStore)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize Gmail backend: %v\n", err)
		return 1
	}

	s3Server := server.New(gmailBackend, auth.NewBucketRegistry(cfg.Buckets))
	s3Server.StartMultipartCleanup(ctx)

	var ready atomic.Bool
	mux := buildMux(s3Server, cfg.Telemetry.Metrics.Enabled, cfg.Telemetry.Metrics.Path, &ready)

	httpServer := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	serveErr := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "HTTP server listening", "addr", cfg.Server.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	ready.Store(true)
	slog.InfoContext(ctx, "Server ready")

	select {
	case err := <-serveErr:
		fmt.Fprintf(stderr, "HTTP server failed: %v\n", err)
		return 1
	case sig := <-sigChan:
		slog.InfoContext(ctx, "Shutdown signal received", "signal", sig.String())
	}

	ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "HTTP server shutdown error", "error", err)
	}

	traceCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer traceCancel()
	if err := shutdownTracer(traceCtx); err != nil {
		slog.ErrorContext(ctx, "Tracer shutdown error", "error", err)
	}

	fmt.Fprintln(stdout, "shutdown complete")
	return 0
}
