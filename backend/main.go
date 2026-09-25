package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/shiraku/vinaire/backend/internal/api"
	"github.com/shiraku/vinaire/backend/internal/config"
	"github.com/shiraku/vinaire/backend/internal/k8s"
	"github.com/shiraku/vinaire/backend/internal/stream"
)

const shutdownTimeout = 5 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	clientset, restConfig, err := k8s.NewClientset(cfg.KubeconfigPath)
	if err != nil {
		// Non-fatal: the server can still boot (e.g. /healthz) without a
		// reachable cluster during local frontend development.
		slog.Warn("kubernetes client unavailable", "error", err)
	} else {
		slog.Info("kubernetes client initialized")
	}

	var metricsClientset *metricsclientset.Clientset
	if restConfig != nil {
		metricsClientset, err = metricsclientset.NewForConfig(restConfig)
		if err != nil {
			// Non-fatal: pod CPU/memory usage just degrades to "—" without it.
			slog.Warn("metrics client unavailable", "error", err)
			metricsClientset = nil
		}
	}

	workloadsHandler := api.NewHandler(clientset, metricsClientset)

	hub := stream.NewHub()
	streamHandler := unavailableWSHandler
	if clientset != nil {
		go stream.StartWorkloadInformers(ctx, clientset, hub)
		streamHandler = stream.ServeWS(hub, cfg.CORSAllowedOrigins)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSAllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         300,
	}))

	r.Get("/healthz", handleHealthz)

	r.Get("/api/v1/pods", workloadsHandler.Pods)
	r.Get("/api/v1/deployments", workloadsHandler.Deployments)
	r.Get("/api/v1/statefulsets", workloadsHandler.StatefulSets)
	r.Get("/api/v1/daemonsets", workloadsHandler.DaemonSets)
	r.Get("/api/v1/jobs", workloadsHandler.Jobs)
	r.Get("/api/v1/stream/workloads", streamHandler)
	r.Get("/api/v1/pods/{namespace}/{name}", workloadsHandler.PodDetail)

	// TODO(vinaire): read-only REST endpoints, backed by internal/k8s
	// (later roadmap phases):
	//   GET /api/v1/deployments/{namespace}/{name}
	//   GET /api/v1/configmaps?namespace=
	//   GET /api/v1/secrets?namespace=        (metadata only)
	//   GET /api/v1/ingresses?namespace=

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("vinaire backend listening",
			"addr", server.Addr,
			"cors_allowed_origins", cfg.CORSAllowedOrigins,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received, draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// unavailableWSHandler is used in place of stream.ServeWS when the backend
// booted without a reachable cluster — mirrors api.Handler's
// requireClientset behavior for the REST endpoints.
func unavailableWSHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "kubernetes client unavailable", http.StatusServiceUnavailable)
}

// requestLogger logs one structured line per request via slog, replacing
// chi's default text logger.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}
