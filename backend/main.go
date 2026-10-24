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

	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/f1nn17/vinaire/backend/internal/api"
	"github.com/f1nn17/vinaire/backend/internal/audit"
	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/backup"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/config"
	"github.com/f1nn17/vinaire/backend/internal/gitops"
	"github.com/f1nn17/vinaire/backend/internal/helm"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
	"github.com/f1nn17/vinaire/backend/internal/keyprovider"
	"github.com/f1nn17/vinaire/backend/internal/leader"
	"github.com/f1nn17/vinaire/backend/internal/netguard"
	"github.com/f1nn17/vinaire/backend/internal/provision"
	"github.com/f1nn17/vinaire/backend/internal/stream"
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
		SessionTokenTTL:   cfg.SessionTokenTTL,
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
	// Roadmap Phase 48: sessions shared by every replica (and surviving a restart).
	switch cfg.SessionStore {
	case "memory":
	case "secret":
		ss, isSecret := clusterStore.(*clusters.SecretStore)
		if auth.Mode == authn.ModeDisabled {
			slog.Info("SESSION_STORE=secret has no effect with AUTH_MODE=disabled (there are no sessions)")
			break
		}
		if !clusterStoreOK || !isSecret {
			slog.Error("SESSION_STORE=secret needs Vinaire to run in a cluster (it keeps the sessions in a Secret of its own namespace)")
			os.Exit(1)
		}
		backend, sealer, sealed := ss.SessionBackend()
		if !sealed {
			slog.Error("SESSION_STORE=secret needs an encryption key — set CLUSTER_STORE_KEY or let Vinaire manage the Secret vinaire-clusters-key; a session holds a Kubernetes token and is never stored unsealed")
			os.Exit(1)
		}
		shared := authn.NewSharedStore(backend, sealer)
		shared.StartJanitor(ctx)
		auth.UseStore(shared)
		slog.Info("sessions are kept in a sealed Secret, shared by every replica")
	default:
		slog.Error("unknown SESSION_STORE (want memory or secret)", "value", cfg.SessionStore)
		os.Exit(1)
	}
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
	workloadsHandler.AuthDocs = gitopsDocs
	// Roadmap Phase 80: people who sign in through Keycloak can be given a ServiceAccount of their own, in one
	// namespace, using Vinaire's own in-cluster identity (the chart grants a Role there and nowhere else).
	if ss, ok := clusterStore.(*clusters.SecretStore); ok && clusterStoreOK {
		workloadsHandler.BrokerClientset = ss.Clientset()
		workloadsHandler.BrokerNamespace = os.Getenv("OIDC_USER_NAMESPACE")
		workloadsHandler.BrokerTTL = cfg.SessionMaxTTL
	}
	workloadsHandler.RolePrefix = os.Getenv("ROLE_PREFIX")
	workloadsHandler.Install = installationFacts(cfg)
	if os.Getenv("OIDC_ALLOW_HTTP_PRIVATE_NETWORK") == "true" {
		workloadsHandler.OIDCAllowHTTPPrivate = true
		slog.Warn("OIDC_ALLOW_HTTP_PRIVATE_NETWORK is set: Keycloak may be reached over plain http, and its administrator's password may be sent over plain http, on a private network. Use it for a home server without a certificate; never across the internet.")
	}
	if os.Getenv("OIDC_ALLOW_INSECURE") == "true" {
		workloadsHandler.OIDCAllowInsecure = true // local development only: an http:// Keycloak on loopback
		slog.Warn("OIDC_ALLOW_INSECURE is set: the Keycloak setup may use http:// on loopback. Never set this on a real deployment.")
	}
	workloadsHandler.GitOps = gitops.NewManager(gitopsDocs, registry, gitops.GitFetcher{}, workloadsHandler.AuditBackground)
	// Roadmap Phase 48: with several replicas only the elected one polls git.
	if os.Getenv("LEADER_ELECTION") == "true" {
		if ss, ok := clusterStore.(*clusters.SecretStore); ok && clusterStoreOK {
			id, _ := os.Hostname()
			workloadsHandler.GitOps.SetLeader(leader.Run(ctx, ss.Clientset(), ss.Namespace(), id).IsLeader)
		} else {
			slog.Warn("LEADER_ELECTION=true needs Vinaire to run in a cluster — ignored")
		}
	}
	if err := workloadsHandler.GitOps.Start(ctx); err != nil {
		slog.Error("gitops: couldn't load its configuration — it stays off", "error", err)
	}
	workloadsHandler.RestoreOIDC(ctx) // Roadmap Phase 47: a saved Keycloak sign-in comes back after a restart
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
	// Roadmap Phase 59: the operator's own install recipes, on top of the built-in ones.
	if path := os.Getenv("CATALOG_RECIPES"); path != "" {
		if n, err := helm.LoadExtraRecipes(path); err != nil {
			slog.Error("CATALOG_RECIPES is invalid — using the built-in recipes only", "path", path, "error", err)
		} else {
			slog.Info("loaded install recipes", "path", path, "count", n)
		}
	}
	// Roadmap Phase 77b: where dumps may be uploaded, from the operator's file only.
	if bc, err := backup.LoadConfig(os.Getenv("BACKUP_TARGETS")); err != nil {
		slog.Error("BACKUP_TARGETS is invalid — backups go to a volume claim only", "error", err)
		workloadsHandler.Backup = &backup.Config{}
	} else {
		workloadsHandler.Backup = bc
	}
	workloadsHandler.Catalog = helm.NewCatalog(catalogCfg)
	// Load every repository's index in the background so the first search is quick.
	workloadsHandler.Catalog.Warm()
	workloadsHandler.Releases = &helm.Releases{Catalog: workloadsHandler.Catalog}
	workloadsHandler.InstallJobs = helm.NewJobs()
	provisionStream := stream.ServeProvision(workloadsHandler.Provisions, auth, cfg.CORSAllowedOrigins)

	streamHandler := unavailableWSHandler
	logsHandler := unavailableWSHandler
	execHandler := unavailableWSHandler
	if !registry.Empty() {
		streamHandler = stream.ServeWS(hubRegistry, registry, registry.Default().Name, cfg.CORSAllowedOrigins, auth)
		logsHandler = stream.ServeLogs(registry, auth, cfg.CORSAllowedOrigins)
		execHandler = stream.ServeExec(registry, auth, cfg.CORSAllowedOrigins, workloadsHandler.AuditBackground)
	}

	r := newRouter(routerDeps{
		cfg: cfg, auth: auth, h: workloadsHandler,
		streamHandler: streamHandler, logsHandler: logsHandler, execHandler: execHandler,
		provisionStream: provisionStream, writeLimiter: writeLimiter,
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
	// Roadmap Phase 49: the store's data key can live outside the cluster (Vault Transit).
	// A misconfigured or unreachable provider is fatal: the alternative would be a store
	// on a different key, or unencrypted.
	opts := clusters.StoreOptions{
		EnvKey: cfg.ClusterStoreKey, Previous: cfg.ClusterStorePrevious,
		Rewrap: os.Getenv("KEY_REWRAP_ON_START") == "true",
	}
	prov, err := keyprovider.FromEnv()
	switch {
	case err == nil:
		opts.Provider = prov
		slog.Info("the store key is kept by a key provider", "provider", prov.Name())
	case errors.Is(err, keyprovider.ErrNotConfigured):
	default:
		slog.Error("key provider configuration is invalid", "error", err)
		os.Exit(1)
	}
	open := func() (clusters.Store, bool, error) {
		if s, ok, err := clusters.NewSecretStoreOpts(ctx, opts); err != nil || ok {
			if err != nil {
				return nil, true, err
			}
			return s, true, nil
		}
		if cfg.DataDir == "" {
			return nil, false, nil
		}
		s, err := clusters.NewFileStoreOpts(ctx, cfg.DataDir, opts)
		if err != nil {
			return nil, true, err
		}
		return s, true, nil
	}
	var st clusters.Store
	var ok bool
	for attempt := 1; ; attempt++ {
		st, ok, err = open()
		if err == nil {
			break
		}
		if prov == nil || attempt >= 6 || errors.Is(err, clusters.ErrKeyUnavailable) {
			slog.Error("cluster store: couldn't open it", "error", err)
			if prov != nil || errors.Is(err, clusters.ErrKeyUnavailable) {
				os.Exit(1)
			}
			return nil, false
		}
		slog.Warn("cluster store: waiting for the key provider", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			os.Exit(1)
		case <-time.After(time.Duration(attempt) * 5 * time.Second):
		}
	}
	return st, ok
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
