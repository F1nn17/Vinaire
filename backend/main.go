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
	"github.com/shiraku/vinaire/backend/internal/gitops"
	"github.com/shiraku/vinaire/backend/internal/helm"
	"github.com/shiraku/vinaire/backend/internal/k8s"
	"github.com/shiraku/vinaire/backend/internal/netguard"
	"github.com/shiraku/vinaire/backend/internal/provision"
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
	var logLevel slog.LevelVar
	if err := logLevel.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		logLevel.Set(slog.LevelInfo)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: &logLevel})))

	cfg := config.Load()
	if err := config.ValidateCORSOrigins(cfg.CORSAllowedOrigins); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

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

		SessionMaxTTL:     cfg.SessionMaxTTL,
		TrustForwardedFor: cfg.TrustForwardedFor,
	})
	if err != nil {
		slog.Error("auth provider initialization failed", "auth_mode", cfg.AuthMode, "error", err)
		os.Exit(1)
	}
	if auth.Mode == authn.ModeToken {
		// a pasted token must be accepted by the cluster before it gets a session
		auth.ValidateToken = tokenValidator(registry)
	}
	slog.Info("auth provider initialized", "mode", auth.Mode)

	// Cluster management via the web UI. The registry of added clusters is
	// sealed (AES-GCM) and persisted in a Secret in this backend's own
	// namespace when running in-cluster, or in an encrypted file under
	// VINAIRE_DATA_DIR when running on a home machine/server (Roadmap
	// Phase 34). With neither, cluster management is unavailable. Loaded now
	// so clusters added in a previous run survive this restart.
	clusterStore, clusterStoreOK := buildClusterStore(ctx, cfg)
	if !clusterStoreOK {
		slog.Info("cluster management via the web UI is unavailable (not running in-cluster and VINAIRE_DATA_DIR is not set)")
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
	workloadsHandler.DiscoveryPaths = cfg.DiscoveryKubeconfigPaths

	// Roadmap Phase 29: GitOps. Its configuration (modes, sources and their
	// tokens) is a sealed document next to the cluster registry, so it needs the
	// same store; without one GitOps is unavailable. Git is fetched through the
	// guarded HTTP client (no loopback/link-local targets).
	if os.Getenv("GITOPS_ALLOW_INSECURE_GIT") == "true" {
		gitops.AllowInsecureGit = true // local development only: plain http://, loopback
		slog.Warn("GITOPS_ALLOW_INSECURE_GIT is set: git sources may use http:// on loopback. Never set this on a real deployment.")
	}
	var gitopsDocs clusters.DocStore
	if ds, ok := clusterStore.(clusters.DocStore); ok && clusterStoreOK {
		gitopsDocs = ds
	}
	var devGit *http.Client
	if gitops.AllowInsecureGit {
		devGit = netguard.Client(netguard.Options{AllowLoopback: true, Timeout: 2 * time.Minute})
	}
	gitops.InstallTransport(netguard.Client(netguard.Options{Timeout: 2 * time.Minute}), devGit)
	workloadsHandler.GitOps = gitops.NewManager(gitopsDocs, registry, gitops.GitFetcher{}, workloadsHandler.AuditBackground)
	if err := workloadsHandler.GitOps.Start(ctx); err != nil {
		slog.Error("gitops: couldn't load its configuration — it stays off", "error", err)
	}
	go detectClusterTypes(ctx, registry)
	writeLimiter := api.NewWriteRateLimiter(writeRateLimit, writeRateBurst)

	// Roadmap Phase 30: node provisioning (SSH). In-memory job registry; the
	// WebSocket streams a run's progress to the Add-node dialog.
	workloadsHandler.Provisions = provision.NewRegistry()
	if path := os.Getenv("KUBEADM_CONFIG"); path != "" {
		if kc, err := provision.LoadKubeadmConfig(path); err != nil {
			slog.Error("KUBEADM_CONFIG is invalid — using the built-in kubeadm configuration", "path", path, "error", err)
		} else {
			provision.Configure(kc)
		}
	}
	workloadsHandler.ProvisionAllowInsecure = os.Getenv("PROVISION_ALLOW_INSECURE") == "true"

	// Roadmap Phase 28: the Helm catalog. Its repositories come only from
	// configuration (catalog.json built in, or CATALOG_CONFIG), never from a request.
	catalogCfg := helm.DefaultConfig()
	if path := os.Getenv("CATALOG_CONFIG"); path != "" {
		if c, err := helm.LoadConfig(path); err != nil {
			slog.Error("CATALOG_CONFIG is invalid — using the built-in catalog", "path", path, "error", err)
		} else {
			catalogCfg = c
		}
	}
	workloadsHandler.Catalog = helm.NewCatalog(catalogCfg)
	// Load every repository's index in the background so the first search is quick.
	workloadsHandler.Catalog.Warm()
	workloadsHandler.Releases = &helm.Releases{Catalog: workloadsHandler.Catalog}
	provisionStream := stream.ServeProvision(workloadsHandler.Provisions, auth, cfg.CORSAllowedOrigins)

	streamHandler := unavailableWSHandler
	logsHandler := unavailableWSHandler
	execHandler := unavailableWSHandler
	if !registry.Empty() {
		streamHandler = stream.ServeWS(hubRegistry, registry, registry.Default().Name, cfg.CORSAllowedOrigins, auth)
		logsHandler = stream.ServeLogs(registry, auth, cfg.CORSAllowedOrigins)
		execHandler = stream.ServeExec(registry, auth, cfg.CORSAllowedOrigins, workloadsHandler.AuditBackground)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	// state-changing requests must come from this site or an allowed origin (CSRF defence in depth)
	r.Use(api.OriginGuard(cfg.CORSAllowedOrigins))
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
	// Roadmap Phase 26: browser-based port-forward — an authenticated reverse
	// HTTP proxy into a pod, outside /api/v1 because the browser navigates to
	// it directly. See internal/api/portforward.go.
	r.Group(func(p chi.Router) {
		p.Use(auth.Middleware)
		p.Get("/proxy/{cluster}/{namespace}/{pod}/{target}", workloadsHandler.PortForwardRedirect)
		p.HandleFunc("/proxy/{cluster}/{namespace}/{pod}/{target}/*", workloadsHandler.PortForwardProxy)
	})

	r.Route("/api/v1", func(protected chi.Router) {
		protected.Use(auth.Middleware)

		protected.Get("/search", workloadsHandler.Search)
		protected.Get("/capacity", workloadsHandler.Capacity)
		protected.Get("/forms/{resourceType}", workloadsHandler.FormSchemaByType)
		protected.Get("/namespaces", workloadsHandler.Namespaces)
		protected.Get("/namespaces/{name}", workloadsHandler.NamespaceDetail)
		// Roadmap Phase 28: browsing the Helm catalog and reading releases;
		// the writes are in the writeLimiter group below.
		protected.Get("/catalog/repos", workloadsHandler.CatalogRepos)
		protected.Get("/catalog/charts", workloadsHandler.CatalogSearch)
		protected.Get("/catalog/charts/{repo}/{chart}", workloadsHandler.CatalogChart)
		protected.Get("/catalog/releases", workloadsHandler.CatalogReleases)
		protected.Get("/catalog/releases/{namespace}/{name}/history", workloadsHandler.CatalogReleaseHistory)
		protected.Get("/catalog/releases/{namespace}/{name}/values", workloadsHandler.CatalogReleaseValues)
		// Roadmap Phase 29: GitOps reads; the writes are in the writeLimiter group.
		protected.Get("/gitops", workloadsHandler.GitOpsOverview)
		protected.Get("/gitops/sources/{id}", workloadsHandler.GitOpsSource)
		protected.Get("/gitops/argocd/applications", workloadsHandler.GitOpsArgoApplications)
		protected.Get("/nodes/provision/defaults", workloadsHandler.ProvisionDefaults)
		protected.Get("/provision/options", workloadsHandler.ProvisionOptionsHandler)
		protected.Get("/nodes/provision/{id}", workloadsHandler.ProvisionStatus)
		protected.Get("/nodes/provision/{id}/stream", provisionStream)
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
		// Roadmap Phase 34: more ways to add a cluster, local discovery, types.
		protected.Get("/clusters/management", workloadsHandler.ClusterManagementInfo)
		protected.Get("/clusters/discover", workloadsHandler.DiscoverClusters)
		protected.Post("/clusters/import-kubeconfig/preview", workloadsHandler.PreviewKubeconfig)
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
		// Roadmap Phase 32: per-resource metric series for the Metrics tab.
		protected.Get("/pods/{namespace}/{name}/metrics", workloadsHandler.PodMetrics)
		protected.Get("/deployments/{namespace}/{name}/metrics", workloadsHandler.DeploymentMetrics)
		protected.Get("/nodes/{name}/metrics", workloadsHandler.NodeMetrics)
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
			// Roadmap Phase 30: SSH provisioning — see internal/api/provision.go.
			w.Post("/clusters/import-kubeconfig", workloadsHandler.ImportKubeconfig)
			w.Post("/clusters/add-discovered", workloadsHandler.AddDiscoveredCluster)
			w.Put("/clusters/{name}/type", workloadsHandler.SetClusterType)
			w.Post("/clusters/ssh/hostkey", workloadsHandler.ProvisionHostKey)
			w.Post("/clusters/import-ssh", workloadsHandler.ClusterSSHStart(provision.ModeImport))
			w.Post("/clusters/create-ssh", workloadsHandler.ClusterSSHStart(provision.ModeCreate))
			w.Post("/nodes/reset", workloadsHandler.ProvisionReset)
			w.Post("/nodes/provision/hostkey", workloadsHandler.ProvisionHostKey)
			w.Post("/nodes/provision", workloadsHandler.ProvisionStart)
			// Roadmap Phase 25's generic YAML create/update (any kind the
			// cluster serves; the caller's own RBAC decides) — see
			// internal/k8s/apply.go and AGENTS.md.
			// Roadmap Phase 29: GitOps — see internal/gitops, internal/api/gitops.go and AGENTS.md.
			w.Put("/gitops/mode", workloadsHandler.GitOpsSetMode)
			w.Post("/gitops/sources", workloadsHandler.GitOpsAddSource)
			w.Delete("/gitops/sources/{id}", workloadsHandler.GitOpsRemoveSource)
			w.Post("/gitops/sources/{id}/sync", workloadsHandler.GitOpsSyncSource)
			w.Post("/gitops/argocd/applications/{namespace}/{name}/sync", workloadsHandler.GitOpsArgoSync)
			// Roadmap Phase 28: Helm releases — see internal/api/catalog.go and AGENTS.md.
			w.Post("/catalog/install", workloadsHandler.CatalogInstall)
			w.Post("/catalog/releases/{namespace}/{name}/upgrade", workloadsHandler.CatalogUpgrade)
			w.Post("/catalog/releases/{namespace}/{name}/rollback", workloadsHandler.CatalogRollback)
			w.Delete("/catalog/releases/{namespace}/{name}", workloadsHandler.CatalogUninstall)
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

// buildClusterStore picks where added clusters persist: a Secret when running
// in-cluster, otherwise an encrypted file in VINAIRE_DATA_DIR.
func buildClusterStore(ctx context.Context, cfg config.Config) (clusters.Store, bool) {
	if s, ok := clusters.NewSecretStore(ctx, cfg.ClusterStoreKey, cfg.ClusterStorePrevious...); ok {
		return s, true
	}
	if cfg.DataDir == "" {
		return nil, false
	}
	s, err := clusters.NewFileStore(cfg.DataDir, cfg.ClusterStoreKey, cfg.ClusterStorePrevious...)
	if err != nil {
		slog.Error("cluster store: couldn't open the data directory", "dir", cfg.DataDir, "error", err)
		return nil, false
	}
	return s, true
}

// detectClusterTypes fills in the type of every registered cluster that
// doesn't have one yet (Roadmap Phase 34) — env-configured clusters at
// startup, and any restored from a store written before types existed. It runs
// in the background: detection makes a few API calls per cluster and must not
// delay startup or break one unreachable cluster's neighbours.
func detectClusterTypes(ctx context.Context, registry *clusters.Registry) {
	for _, name := range registry.Names() {
		entry, ok := registry.Get(name)
		if !ok || entry.Type() != clusters.TypeUnknown {
			continue
		}
		entry.SetType(clusters.DetectType(ctx, entry.Clientset, name))
		slog.Info("detected cluster type", "cluster", name, "type", entry.Type())
	}
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
