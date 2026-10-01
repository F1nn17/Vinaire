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
	"golang.org/x/time/rate"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/shiraku/vinaire/backend/internal/api"
	"github.com/shiraku/vinaire/backend/internal/audit"
	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/clusters"
	"github.com/shiraku/vinaire/backend/internal/config"
	"github.com/shiraku/vinaire/backend/internal/k8s"
	"github.com/shiraku/vinaire/backend/internal/stream"
)

// writeRateLimit/writeRateBurst bound the three Phase 9 write endpoints
// (Roadmap Phase 17) — generous enough that normal interactive use (a
// restart here, a rollback there) never trips it, tight enough that a
// scripted hammering of the endpoint gets a real 429 within a few requests.
const (
	writeRateLimit = rate.Limit(1) // 1/sec sustained, per identity
	writeRateBurst = 5
)

const shutdownTimeout = 5 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry, hubRegistry := buildClusterRegistry(ctx, cfg)

	// Unlike the per-cluster clients below, a misconfigured AUTH_MODE=oidc
	// is fatal at boot — this phase's own risk note: fail loudly here, not
	// silently break every login later.
	auth, err := authn.NewManager(ctx, authn.ManagerConfig{
		AuthMode:         cfg.AuthMode,
		CookieSecure:     cfg.CookieSecure,
		CookieSameSite:   cfg.CookieSameSite,
		FrontendURL:      cfg.FrontendURL,
		OIDCIssuerURL:    cfg.OIDCIssuerURL,
		OIDCClientID:     cfg.OIDCClientID,
		OIDCClientSecret: cfg.OIDCClientSecret,
		OIDCRedirectURL:  cfg.OIDCRedirectURL,
	})
	if err != nil {
		slog.Error("auth provider initialization failed", "auth_mode", cfg.AuthMode, "error", err)
		os.Exit(1)
	}
	slog.Info("auth provider initialized", "mode", auth.Mode)

	// Cluster management via the web UI: a Secret in this backend's own
	// namespace, written with its own in-cluster identity (not available at
	// all outside a cluster — see internal/clusters.Store). Loaded now so
	// clusters added in a previous run survive this restart.
	clusterStore, clusterStoreOK := clusters.NewStore()
	if !clusterStoreOK {
		slog.Info("cluster management via the web UI is unavailable (not running in-cluster)")
	} else if stored, err := clusterStore.Load(ctx); err != nil {
		slog.Warn("failed to load persisted clusters", "error", err)
	} else {
		for _, sc := range stored {
			entry, err := clusters.EntryFromStored(sc)
			if err != nil {
				slog.Warn("failed to restore persisted cluster", "cluster", sc.Name, "error", err)
				continue
			}
			registry.AddDynamic(entry)
			slog.Info("restored persisted cluster", "cluster", sc.Name)
		}
	}

	workloadsHandler := api.NewHandler(registry, auth, api.ObservabilityConfig{
		PrometheusURL:   cfg.PrometheusURL,
		LokiURL:         cfg.LokiURL,
		AlertmanagerURL: cfg.AlertmanagerURL,
	})
	workloadsHandler.Audit = audit.NewLogger(cfg.AuditLogPath)
	if clusterStoreOK {
		workloadsHandler.ClusterStore = clusterStore
	}
	writeLimiter := api.NewWriteRateLimiter(writeRateLimit, writeRateBurst)

	streamHandler := unavailableWSHandler
	logsHandler := unavailableWSHandler
	execHandler := unavailableWSHandler
	if !registry.Empty() {
		streamHandler = stream.ServeWS(hubRegistry, registry.Default().Name, cfg.CORSAllowedOrigins, auth)
		logsHandler = stream.ServeLogs(registry, auth, cfg.CORSAllowedOrigins)
		execHandler = stream.ServeExec(registry, auth, cfg.CORSAllowedOrigins)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-Cluster"},
		AllowCredentials: true, // the session cookie (Roadmap Phase 10) needs this
		MaxAge:           300,
	}))

	r.Get("/healthz", handleHealthz)

	// Unprotected — these ARE the login flow (Roadmap Phase 10).
	r.Get("/api/v1/auth/config", auth.HandleConfig)
	r.Post("/api/v1/auth/login", auth.HandleTokenLogin)
	r.Post("/api/v1/auth/logout", auth.HandleLogout)
	r.Get("/api/v1/auth/oidc/login", auth.HandleOIDCLogin)
	r.Get("/api/v1/auth/oidc/callback", auth.HandleOIDCCallback)

	// Every other /api/v1 route requires a session (Roadmap Phase 10) — see
	// internal/authn.Manager.Middleware. Authorization itself is whatever
	// the caller's own Kubernetes RBAC decides on each call (their own
	// pasted token or OIDC identity), not a separate check in this backend.
	r.Route("/api/v1", func(protected chi.Router) {
		protected.Use(auth.Middleware)

		protected.Get("/search", workloadsHandler.Search)
		protected.Get("/capacity", workloadsHandler.Capacity)
		protected.Get("/forms/{resourceType}", workloadsHandler.FormSchemaByType)
		protected.Get("/namespaces", workloadsHandler.Namespaces)
		protected.Get("/namespaces/{name}", workloadsHandler.NamespaceDetail)
		protected.Get("/nodes", workloadsHandler.Nodes)
		protected.Get("/nodes/{name}", workloadsHandler.NodeDetail)
		protected.Get("/pods", workloadsHandler.Pods)
		protected.Get("/deployments", workloadsHandler.Deployments)
		protected.Get("/statefulsets", workloadsHandler.StatefulSets)
		protected.Get("/daemonsets", workloadsHandler.DaemonSets)
		protected.Get("/jobs", workloadsHandler.Jobs)
		protected.Get("/stream/workloads", streamHandler)
		protected.Get("/pods/{namespace}/{name}", workloadsHandler.PodDetail)
		protected.Get("/pods/{namespace}/{name}/logs", logsHandler)
		protected.Get("/pods/{namespace}/{name}/logs/search", workloadsHandler.LogsSearch)
		protected.Get("/pods/{namespace}/{name}/exec", execHandler)
		protected.Get("/deployments/{namespace}/{name}", workloadsHandler.DeploymentDetail)
		protected.Get("/statefulsets/{namespace}/{name}", workloadsHandler.StatefulSetDetail)
		protected.Get("/daemonsets/{namespace}/{name}", workloadsHandler.DaemonSetDetail)
		protected.Get("/jobs/{namespace}/{name}", workloadsHandler.JobDetail)
		protected.Get("/configmaps", workloadsHandler.ConfigMaps)
		protected.Get("/secrets", workloadsHandler.Secrets)
		protected.Post("/secrets/{namespace}/{name}/reveal", workloadsHandler.RevealSecretKey)
		protected.Get("/topology", workloadsHandler.Topology)
		protected.Get("/ingresses", workloadsHandler.Ingresses)
		protected.Get("/gateway-routes", workloadsHandler.GatewayRoutes)
		protected.Get("/clusters", workloadsHandler.ListClusters)
		protected.Post("/clusters", workloadsHandler.AddCluster)
		protected.Delete("/clusters/{name}", workloadsHandler.RemoveCluster)
		protected.Get("/pvcs", workloadsHandler.PersistentVolumeClaims)
		protected.Get("/pvcs/{namespace}/{name}", workloadsHandler.PersistentVolumeClaimDetail)
		protected.Get("/pvs", workloadsHandler.PersistentVolumes)
		protected.Get("/storageclasses", workloadsHandler.StorageClasses)
		protected.Get("/roles", workloadsHandler.Roles)
		protected.Get("/clusterroles", workloadsHandler.ClusterRoles)
		protected.Get("/rolebindings", workloadsHandler.RoleBindings)
		protected.Get("/clusterrolebindings", workloadsHandler.ClusterRoleBindings)
		protected.Get("/serviceaccounts", workloadsHandler.ServiceAccounts)
		protected.Get("/rbac/summary", workloadsHandler.RBACSummary)
		protected.Get("/crds", workloadsHandler.CRDs)
		protected.Get("/crds/{crdName}/instances", workloadsHandler.CRDInstances)
		protected.Get("/crds/{crdName}/instances/{name}", workloadsHandler.CRDInstanceDetail)
		protected.Get("/metrics/query", workloadsHandler.MetricsQuery)
		protected.Get("/metrics/query_range", workloadsHandler.MetricsQueryRange)
		protected.Get("/alerts", workloadsHandler.Alerts)
		protected.Get("/dashboard/summary", workloadsHandler.DashboardSummary)
		protected.Get("/events", workloadsHandler.Events)

		// Write actions (Roadmap Phase 9, extended by Phase 33) — the
		// deliberate, narrow, audited exception to the read-only rule. See
		// AGENTS.md's core rule and internal/k8s/deployment_actions.go /
		// scale_actions.go / pod_actions.go before adding another one.
		// Rate-limited per identity (Roadmap Phase 17) — see writeLimiter.
		// Dry-run persists nothing, so it stays outside the write limiter —
		// a user iterating on a manifest shouldn't burn their write budget.
		protected.Post("/resources/dry-run", workloadsHandler.ApplyDryRun)
		protected.Group(func(w chi.Router) {
			w.Use(writeLimiter.Middleware)
			w.Post("/deployments/{namespace}/{name}/restart", workloadsHandler.RestartDeployment)
			w.Delete("/deployments/{namespace}/{name}", workloadsHandler.DeleteDeployment)
			w.Post("/deployments/{namespace}/{name}/rollback", workloadsHandler.RollbackDeployment)
			w.Post("/deployments/{namespace}/{name}/scale", workloadsHandler.ScaleDeployment)
			w.Post("/deployments/{namespace}/{name}/pause", workloadsHandler.PauseDeploymentRollout)
			w.Post("/deployments/{namespace}/{name}/resume", workloadsHandler.ResumeDeploymentRollout)
			w.Post("/statefulsets/{namespace}/{name}/scale", workloadsHandler.ScaleStatefulSet)
			w.Delete("/pods/{namespace}/{name}", workloadsHandler.DeletePod)
			// Roadmap Phase 27: irreversible, cascading — see AGENTS.md and
			// internal/k8s/namespaces.go.
			w.Delete("/namespaces/{name}", workloadsHandler.DeleteNamespace)
			// Roadmap Phase 25's generic YAML create/update (any kind the
			// cluster serves; the caller's own RBAC decides) — see
			// internal/k8s/apply.go and AGENTS.md.
			w.Post("/resources", workloadsHandler.CreateResource)
			w.Put("/resources", workloadsHandler.UpdateResource)
		})
	})

	// Single combined image (Roadmap Phase 10's second half — see
	// AGENTS.md's "Rendering model"): if a static export was built into
	// this directory, serve it for everything else. Absent in local dev
	// (STATIC_DIR unset), which behaves exactly as every phase before this.
	if cfg.StaticDir != "" {
		if info, err := os.Stat(cfg.StaticDir); err == nil && info.IsDir() {
			r.Handle("/*", staticFileHandler(cfg.StaticDir))
			slog.Info("serving frontend static export", "dir", cfg.StaticDir)
		} else {
			slog.Warn("STATIC_DIR set but not a usable directory", "dir", cfg.StaticDir, "error", err)
		}
	}

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
//
// It also wires the Registry's lifecycle hook (cluster management via the
// web UI): every cluster — these env-configured ones and any added later,
// live or restored from Store — starts its live-update informers through
// this exact same hook, so there's one code path for "a cluster just
// became known to this Registry," not two.
func buildClusterRegistry(ctx context.Context, cfg config.Config) (*clusters.Registry, *stream.HubRegistry) {
	registry := clusters.NewRegistry()
	hubRegistry := stream.NewHubRegistry()

	registry.SetLifecycleHook(func(e *clusters.Entry) func() {
		hub := stream.NewHub()
		hubRegistry.Set(e.Name, hub)
		clusterCtx, cancel := context.WithCancel(ctx)
		go stream.StartWorkloadInformers(clusterCtx, e.Clientset, hub)
		return func() {
			cancel()
			hubRegistry.Delete(e.Name)
		}
	})

	if len(cfg.ClusterContexts) == 0 {
		clientset, restConfig, err := k8s.NewClientset(cfg.KubeconfigPath)
		if err != nil {
			slog.Warn("kubernetes client unavailable", "error", err)
			return registry, hubRegistry
		}
		name := cfg.ClusterName
		if name == "" {
			name = k8s.CurrentContextName(cfg.KubeconfigPath)
		}
		if name == "" {
			name = "default"
		}
		registry.AddDynamic(newClusterEntry(name, clientset, restConfig))
		slog.Info("kubernetes client initialized", "cluster", name)
		return registry, hubRegistry
	}

	for _, name := range cfg.ClusterContexts {
		clientset, restConfig, err := k8s.NewClientsetForContext(cfg.KubeconfigPath, name)
		if err != nil {
			slog.Warn("kubernetes client unavailable for cluster", "cluster", name, "error", err)
			continue
		}
		registry.AddDynamic(newClusterEntry(name, clientset, restConfig))
		slog.Info("kubernetes client initialized", "cluster", name)
	}
	return registry, hubRegistry
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
// backend booted without a single reachable cluster.
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
