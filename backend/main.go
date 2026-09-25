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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/shiraku/vinaire/backend/internal/api"
	"github.com/shiraku/vinaire/backend/internal/authz"
	"github.com/shiraku/vinaire/backend/internal/clusters"
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

	registry := buildClusterRegistry(cfg)

	verifier, err := authz.NewVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID)
	if err != nil {
		// Non-fatal like the per-cluster clients below, but security-critical:
		// authMiddleware (see below) fails every /api/v1 request closed
		// rather than letting them through unauthenticated.
		slog.Warn("oidc provider unavailable — all /api/v1 requests will be rejected until it's reachable", "issuer", cfg.OIDCIssuerURL, "error", err)
	} else {
		slog.Info("oidc verifier initialized", "issuer", cfg.OIDCIssuerURL)
	}

	policy, err := authz.LoadPolicy(cfg.PolicyPath)
	if err != nil {
		slog.Warn("policy file unavailable — denying all namespace access until it's fixed", "path", cfg.PolicyPath, "error", err)
		policy = &authz.Policy{Groups: map[string]authz.GroupPolicy{}}
	}

	hubs := make(map[string]*stream.Hub, len(registry.Names()))
	for _, name := range registry.Names() {
		entry, _ := registry.Get(name)
		hub := stream.NewHub()
		hubs[name] = hub
		go stream.StartWorkloadInformers(ctx, entry.Clientset, hub)
	}

	workloadsHandler := api.NewHandler(registry, policy)

	streamHandler := unavailableWSHandler
	logsHandler := unavailableWSHandler
	execHandler := unavailableWSHandler
	if !registry.Empty() && verifier != nil {
		streamHandler = stream.ServeWS(hubs, registry.Default().Name, cfg.CORSAllowedOrigins, verifier, policy)
		logsHandler = stream.ServeLogs(registry, verifier, policy, cfg.CORSAllowedOrigins)
		execHandler = stream.ServeExec(registry, verifier, policy, cfg.CORSAllowedOrigins)
	}

	authMiddleware := func(next http.Handler) http.Handler {
		if verifier == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
			})
		}
		return verifier.Middleware(next)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.CORSAllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "X-Cluster"},
		MaxAge:         300,
	}))

	r.Get("/healthz", handleHealthz)

	// Every /api/v1 route requires a verified OIDC token (Roadmap Phase 10)
	// — see AGENTS.md's core rule and internal/authz for what that gates.
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authMiddleware)

		api.Get("/pods", workloadsHandler.Pods)
		api.Get("/deployments", workloadsHandler.Deployments)
		api.Get("/statefulsets", workloadsHandler.StatefulSets)
		api.Get("/daemonsets", workloadsHandler.DaemonSets)
		api.Get("/jobs", workloadsHandler.Jobs)
		api.Get("/stream/workloads", streamHandler)
		api.Get("/pods/{namespace}/{name}", workloadsHandler.PodDetail)
		api.Get("/pods/{namespace}/{name}/logs", logsHandler)
		api.Get("/pods/{namespace}/{name}/exec", execHandler)
		api.Get("/configmaps", workloadsHandler.ConfigMaps)
		api.Get("/secrets", workloadsHandler.Secrets)
		api.Post("/secrets/{namespace}/{name}/reveal", workloadsHandler.RevealSecretKey)
		api.Get("/topology", workloadsHandler.Topology)
		api.Get("/ingresses", workloadsHandler.Ingresses)
		api.Get("/gateway-routes", workloadsHandler.GatewayRoutes)
		api.Get("/clusters", workloadsHandler.ListClusters)

		// Write actions (Roadmap Phase 9) — the deliberate, narrow, audited
		// exception to the read-only rule. See AGENTS.md's core rule and
		// internal/k8s/deployment_actions.go before adding another one.
		api.Post("/deployments/{namespace}/{name}/restart", workloadsHandler.RestartDeployment)
		api.Delete("/deployments/{namespace}/{name}", workloadsHandler.DeleteDeployment)
		api.Post("/deployments/{namespace}/{name}/rollback", workloadsHandler.RollbackDeployment)
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("vinaire backend listening",
			"addr", server.Addr,
			"cors_allowed_origins", cfg.CORSAllowedOrigins,
			"clusters", registry.Names(),
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

// buildClusterRegistry builds one clusters.Entry per configured kubeconfig
// context (Roadmap Phase 11's CLUSTER_CONTEXTS), or a single "default"
// entry from KubeconfigPath/in-cluster config when none are configured —
// preserving pre-Phase-11 single-cluster behavior exactly. A context that
// fails to build a clientset is skipped (logged, non-fatal), same
// resilience pattern as the per-cluster metrics/Gateway API clients below.
func buildClusterRegistry(cfg config.Config) *clusters.Registry {
	registry := clusters.NewRegistry()

	if len(cfg.ClusterContexts) == 0 {
		clientset, restConfig, err := k8s.NewClientset(cfg.KubeconfigPath)
		if err != nil {
			slog.Warn("kubernetes client unavailable", "error", err)
			return registry
		}
		registry.Add(newClusterEntry("default", clientset, restConfig))
		slog.Info("kubernetes client initialized", "cluster", "default")
		return registry
	}

	for _, name := range cfg.ClusterContexts {
		clientset, restConfig, err := k8s.NewClientsetForContext(cfg.KubeconfigPath, name)
		if err != nil {
			slog.Warn("kubernetes client unavailable for cluster", "cluster", name, "error", err)
			continue
		}
		registry.Add(newClusterEntry(name, clientset, restConfig))
		slog.Info("kubernetes client initialized", "cluster", name)
	}
	return registry
}

func newClusterEntry(name string, clientset *kubernetes.Clientset, restConfig *rest.Config) *clusters.Entry {
	entry := &clusters.Entry{Name: name, Clientset: clientset, RESTConfig: restConfig}

	if mc, err := metricsclientset.NewForConfig(restConfig); err != nil {
		// Non-fatal: pod CPU/memory usage just degrades to "—" without it.
		slog.Warn("metrics client unavailable", "cluster", name, "error", err)
	} else {
		entry.MetricsClientset = mc
	}

	if gc, err := gatewayclientset.NewForConfig(restConfig); err != nil {
		// Non-fatal: the Network page's Gateway API tab just reports
		// "unavailable" without it. Whether the CRDs themselves are
		// installed is checked per-request (see GatewayRoutes).
		slog.Warn("gateway API client unavailable", "cluster", name, "error", err)
	} else {
		entry.GatewayClientset = gc
	}

	return entry
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// unavailableWSHandler is used in place of a real stream handler when the
// backend booted without a reachable cluster or a working OIDC verifier —
// mirrors api.Handler's resolveCluster behavior for the REST endpoints.
func unavailableWSHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "kubernetes client or authentication unavailable", http.StatusServiceUnavailable)
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
