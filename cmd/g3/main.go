// -------------------------------------------------------------------------------
// g3 - S3-Compatible Gateway Backed by Gmail
//
// Author: Alex Freidah
//
// Entry point for the g3 server. Loads configuration, initializes structured
// logging with trace correlation, sets up OpenTelemetry tracing, connects to
// the Gmail API, and starts the S3-compatible HTTP server with graceful
// shutdown on SIGINT/SIGTERM.
// -------------------------------------------------------------------------------

package main

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
	"github.com/afreidah/g3/internal/config"
	"github.com/afreidah/g3/internal/server"
	"github.com/afreidah/g3/internal/store"
	"github.com/afreidah/g3/internal/telemetry"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() { // codecov:ignore -- process entry point
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			runVersion()
			return
		case "auth":
			runAuth()
			return
		case "validate":
			runValidate()
			return
		case "help":
			printUsage()
			return
		}
	}
	runServe()
}

// runValidate loads and validates the config file without starting the server.
func runValidate() { // codecov:ignore -- os.Exit wrapper
	configPath := flag.String("config", "config.yaml", "path to config file")
	_ = flag.CommandLine.Parse(os.Args[2:])

	_, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validation failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("configuration is valid")
}

// printUsage prints available subcommands.
func printUsage() {
	fmt.Println(`Usage: g3 [command]

Commands:
  serve      Start the S3 gateway server (default)
  auth       Obtain a Gmail API refresh token via device code flow
  validate   Validate configuration file
  version    Print version information
  help       Show this help message

Flags:
  -config string   Path to config file (default "config.yaml")`)
}

// -------------------------------------------------------------------------
// SERVER
// -------------------------------------------------------------------------

// runServe starts the g3 server.
func runServe() { // codecov:ignore -- process entry point
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	ctx := context.Background()

	// --- Load configuration ---
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// --- Initialize structured logging ---
	var logLevel slog.LevelVar
	logLevel.Set(config.ParseLogLevel(cfg.Server.LogLevel))
	logBuffer := telemetry.NewLogBuffer()
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &logLevel})
	traceHandler := telemetry.NewTraceHandler(jsonHandler)
	slog.SetDefault(slog.New(telemetry.NewTeeHandler(traceHandler, logBuffer)))

	slog.InfoContext(ctx, "Starting g3",
		"version", telemetry.Version,
		"go_version", runtime.Version(),
		"listen", cfg.Server.ListenAddr,
	)

	// --- Initialize tracing ---
	shutdownTracer, err := telemetry.InitTracer(ctx, cfg.Telemetry.Tracing)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to initialize tracer", "error", err)
		os.Exit(1)
	}

	// --- Wire audit metrics ---
	audit.OnEvent = func(event string) {
		telemetry.AuditEventsTotal.WithLabelValues(event).Inc()
	}

	// --- Set build info metric ---
	telemetry.BuildInfo.WithLabelValues(telemetry.Version, runtime.Version()).Set(1)

	// --- Initialize metadata store ---
	metadataStore, err := store.New(cfg.Database.Path)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to initialize metadata store", "error", err)
		os.Exit(1)
	}
	defer metadataStore.Close()

	slog.InfoContext(ctx, "Metadata store initialized", "path", cfg.Database.Path)

	// --- Initialize Gmail backend ---
	gmailBackend, err := backend.NewGmailBackend(ctx, &cfg.Gmail, metadataStore)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to initialize Gmail backend", "error", err)
		os.Exit(1)
	}

	// --- Build bucket registry for auth ---
	registry := auth.NewBucketRegistry(cfg.Buckets)

	// --- Create S3 server ---
	s3Server := server.New(gmailBackend, registry)

	// --- Start multipart cleanup loop ---
	s3Server.StartMultipartCleanup(ctx)

	// --- Readiness gate ---
	var ready atomic.Bool

	// --- HTTP multiplexer ---
	mux := http.NewServeMux()

	// Liveness: always 200
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})

	// Readiness: 503 until startup complete
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"status":"not ready"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	})

	// Metrics endpoint
	if cfg.Telemetry.Metrics.Enabled {
		mux.Handle(cfg.Telemetry.Metrics.Path, promhttp.Handler())
		slog.InfoContext(ctx, "Metrics endpoint enabled", "path", cfg.Telemetry.Metrics.Path)
	}

	// S3 API handler (catch-all)
	mux.Handle("/", s3Server)

	// --- HTTP server ---
	httpServer := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	// --- Signal handling ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// --- Start server ---
	go func() {
		slog.InfoContext(ctx, "HTTP server listening", "addr", cfg.Server.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.ErrorContext(ctx, "HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	// --- Mark ready ---
	ready.Store(true)
	slog.InfoContext(ctx, "Server ready")

	// --- Wait for shutdown signal ---
	sig := <-sigChan
	slog.InfoContext(ctx, "Shutdown signal received", "signal", sig.String())

	// --- Graceful shutdown ---
	ready.Store(false)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(ctx, "HTTP server shutdown error", "error", err)
	}

	traceCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer traceCancel()
	if err := shutdownTracer(traceCtx); err != nil {
		slog.ErrorContext(ctx, "Tracer shutdown error", "error", err)
	}

	slog.InfoContext(ctx, "Shutdown complete")
}
