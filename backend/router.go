package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/f1nn17/vinaire/backend/internal/api"
	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/config"
	"github.com/f1nn17/vinaire/backend/internal/provision"
)

// routerDeps is everything the HTTP routes need. Building the router is its own
// function so a test can walk every route (Roadmap Phase 50): which need a session,
// which are rate-limited, which are open.
type routerDeps struct {
	cfg  config.Config
	auth *authn.Manager
	h    *api.Handler

	streamHandler   http.HandlerFunc
	logsHandler     http.HandlerFunc
	execHandler     http.HandlerFunc
	provisionStream http.HandlerFunc
	writeLimiter    *api.WriteRateLimiter
}

func newRouter(d routerDeps) *chi.Mux {
	cfg, auth, workloadsHandler := d.cfg, d.auth, d.h
	streamHandler, logsHandler, execHandler, provisionStream, writeLimiter := d.streamHandler, d.logsHandler, d.execHandler, d.provisionStream, d.writeLimiter
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
		protected.Get("/requests-limits", workloadsHandler.RequestsLimits)
		// "All resources": any kind the cluster serves, read as the caller.
		protected.Get("/api-resources", workloadsHandler.APIResources)
		// The logs stack (Phase 45): status and the install plan are reads; the install itself is the Marketplace's.
		protected.Get("/logging/status", workloadsHandler.LoggingStatus)
		protected.Get("/logging/plan", workloadsHandler.LoggingPlan)
		// The audit viewer is gated by requireVinaireRight(audit.vinaire.io, get) — see internal/api/audit_view.go.
		protected.Get("/audit", workloadsHandler.AuditLog)
		protected.Get("/api-resources/{group}/{version}/{resource}/objects", workloadsHandler.ResourceObjects)
		protected.Get("/api-resources/{group}/{version}/{resource}/objects/{name}", workloadsHandler.ResourceObject)
		protected.Get("/forms/{resourceType}", workloadsHandler.FormSchemaByType)
		protected.Get("/namespaces", workloadsHandler.Namespaces)
		protected.Get("/namespaces/{name}", workloadsHandler.NamespaceDetail)
		// Roadmap Phase 53: projects are namespaces with a label; these two only read.
		protected.Get("/projects", workloadsHandler.Projects)
		protected.Get("/projects/{name}", workloadsHandler.ProjectDetail)
		// Roadmap Phase 54 (light): read-only security checks, as the caller.
		protected.Get("/security/checks", workloadsHandler.SecurityChecks)
		// Roadmap Phase 28: browsing the Helm catalog and reading releases;
		// the writes are in the writeLimiter group below.
		protected.Get("/catalog/repos", workloadsHandler.CatalogRepos)
		protected.Get("/catalog/charts", workloadsHandler.CatalogSearch)
		protected.Get("/catalog/charts/{repo}/{chart}", workloadsHandler.CatalogChart)
		protected.Get("/catalog/releases", workloadsHandler.CatalogReleases)
		protected.Get("/catalog/install-jobs/{id}", workloadsHandler.CatalogInstallJob)
		protected.Get("/catalog/icons/{repo}/{chart}", workloadsHandler.CatalogIcon)
		protected.Get("/images/tags", workloadsHandler.ImageTags)
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
		protected.Get("/nodes/{name}/drain-plan", workloadsHandler.DrainPlan)
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
		protected.Get("/apps/detected", workloadsHandler.DetectedApps)
		protected.Get("/instances", workloadsHandler.Instances)
		protected.Get("/backups/config", workloadsHandler.BackupConfig)
		protected.Get("/instances/{namespace}/{name}/backups", workloadsHandler.InstanceBackups)
		protected.Get("/install/environment", workloadsHandler.InstallEnvironment)
		protected.Get("/ingresses", workloadsHandler.Ingresses)
		protected.Get("/gateway-routes", workloadsHandler.GatewayRoutes)
		protected.Get("/clusters", workloadsHandler.ListClusters)
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
		protected.Get("/rbac/subject", workloadsHandler.SubjectBindings)
		protected.Post("/rbac/check", workloadsHandler.CheckAccess)
		protected.Get("/crds", workloadsHandler.CRDs)
		protected.Get("/crds/{crdName}/instances", workloadsHandler.CRDInstances)
		protected.Get("/crds/{crdName}/instances/{name}", workloadsHandler.CRDInstanceDetail)
		protected.Get("/auth/sessions", workloadsHandler.Sessions)
		protected.Get("/auth/keycloak", workloadsHandler.KeycloakStatus)
		protected.Get("/auth/me", workloadsHandler.WhoAmI)
		protected.Get("/metrics/status", workloadsHandler.MetricsStatus)
		protected.Get("/metrics/plan", workloadsHandler.MetricsPlan)
		protected.Get("/metrics/query", workloadsHandler.MetricsQuery)
		protected.Get("/metrics/query_range", workloadsHandler.MetricsQueryRange)
		// Roadmap Phase 32: per-resource metric series for the Metrics tab.
		protected.Get("/pods/{namespace}/{name}/metrics", workloadsHandler.PodMetrics)
		protected.Get("/deployments/{namespace}/{name}/metrics", workloadsHandler.DeploymentMetrics)
		protected.Get("/statefulsets/{namespace}/{name}/metrics", workloadsHandler.StatefulSetMetrics)
		protected.Get("/daemonsets/{namespace}/{name}/metrics", workloadsHandler.DaemonSetMetrics)
		protected.Get("/jobs/{namespace}/{name}/metrics", workloadsHandler.JobMetrics)
		protected.Get("/nodes/{name}/metrics", workloadsHandler.NodeMetrics)
		protected.Get("/cluster/metrics", workloadsHandler.ClusterMetrics)
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
			// Roadmap Phase 40: node lifecycle - see internal/api/node_actions.go and AGENTS.md.
			// Roadmap Phase 44: RoleBindings / ClusterRoleBindings - see internal/api/rbac_actions.go and AGENTS.md.
			// Roadmap Phase 77b: backups (Jobs, CronJobs, VolumeSnapshots) - see internal/api/backups.go and AGENTS.md.
			w.Post("/instances/{namespace}/{name}/backups", workloadsHandler.CreateBackup)
			// Roadmap Phase 77d: version upgrade of an instance installed from an image.
			w.Post("/instances/{namespace}/{name}/upgrade", workloadsHandler.UpgradeInstance)
			w.Post("/instances/{namespace}/{name}/restore", workloadsHandler.RestoreBackup)
			w.Post("/instances/{namespace}/{name}/restore-snapshot", workloadsHandler.RestoreSnapshot)
			w.Delete("/backups/{namespace}/{kind}/{name}", workloadsHandler.DeleteBackup)
			w.Post("/rbac/bindings", workloadsHandler.CreateBinding)
			w.Delete("/rbac/rolebindings/{namespace}/{name}", workloadsHandler.DeleteRoleBinding)
			w.Delete("/rbac/clusterrolebindings/{name}", workloadsHandler.DeleteClusterRoleBinding)
			w.Post("/nodes/{name}/cordon", workloadsHandler.CordonNode)
			w.Post("/nodes/{name}/uncordon", workloadsHandler.UncordonNode)
			w.Post("/nodes/{name}/drain", workloadsHandler.DrainNode)
			w.Put("/nodes/{name}/labels", workloadsHandler.SetNodeLabels)
			w.Put("/nodes/{name}/taints", workloadsHandler.SetNodeTaints)
			w.Delete("/nodes/{name}", workloadsHandler.DeleteNodeObject)
			w.Post("/deployments/{namespace}/{name}/restart", workloadsHandler.RestartDeployment)
			w.Delete("/deployments/{namespace}/{name}", workloadsHandler.DeleteDeployment)
			w.Post("/deployments/{namespace}/{name}/rollback", workloadsHandler.RollbackDeployment)
			w.Post("/deployments/{namespace}/{name}/scale", workloadsHandler.ScaleDeployment)
			w.Post("/deployments/{namespace}/{name}/pause", workloadsHandler.PauseDeploymentRollout)
			w.Post("/deployments/{namespace}/{name}/resume", workloadsHandler.ResumeDeploymentRollout)
			w.Post("/statefulsets/{namespace}/{name}/scale", workloadsHandler.ScaleStatefulSet)
			// the owner's decision of 2026-10-09: delete works on every Workloads tab (see AGENTS.md)
			w.Delete("/statefulsets/{namespace}/{name}", workloadsHandler.DeleteStatefulSet)
			w.Delete("/daemonsets/{namespace}/{name}", workloadsHandler.DeleteDaemonSet)
			w.Delete("/jobs/{namespace}/{name}", workloadsHandler.DeleteJob)
			// owner's decision of 2026-10-10: Services & Ingress get a multi-select and a Delete
			w.Delete("/services/{namespace}/{name}", workloadsHandler.DeleteService)
			w.Delete("/ingresses/{namespace}/{name}", workloadsHandler.DeleteIngress)
			w.Delete("/pods/{namespace}/{name}", workloadsHandler.DeletePod)
			// the volume claims Helm leaves behind after an uninstall — see AGENTS.md
			w.Delete("/pvcs/{namespace}/{name}", workloadsHandler.DeletePVC)
			// what an uninstalled chart leaves behind (a database's password) — see AGENTS.md
			w.Delete("/secrets/{namespace}/{name}", workloadsHandler.DeleteSecret)
			// Roadmap Phase 27: irreversible, cascading — see AGENTS.md and
			// internal/k8s/namespaces.go.
			w.Delete("/namespaces/{name}", workloadsHandler.DeleteNamespace)
			// Roadmap Phase 30: SSH provisioning — see internal/api/provision.go.
			w.Post("/clusters/import-kubeconfig", workloadsHandler.ImportKubeconfig)
			w.Post("/clusters/add-discovered", workloadsHandler.AddDiscoveredCluster)
			w.Put("/clusters/{name}/type", workloadsHandler.SetClusterType)
			w.Put("/clusters/{name}/credentials", workloadsHandler.RotateClusterCredentials)
			// cluster registry, sign-in and sessions: Vinaire's own configuration (Phase 50: now rate limited too)
			w.Post("/clusters", workloadsHandler.AddCluster)
			w.Delete("/clusters/{name}", workloadsHandler.RemoveCluster)
			w.Delete("/auth/sessions/{ref}", workloadsHandler.RevokeSession)
			w.Post("/auth/keycloak/setup", workloadsHandler.KeycloakSetup)
			w.Put("/auth/keycloak/token-login", workloadsHandler.KeycloakTokenLogin)
			w.Delete("/auth/keycloak", workloadsHandler.KeycloakDisable)
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
			w.Post("/catalog/install/preflight", workloadsHandler.CatalogInstallPreflight)
			w.Post("/catalog/install/manifest", workloadsHandler.CatalogInstallManifest)
			w.Post("/catalog/install-jobs", workloadsHandler.CatalogStartInstallJob)
			w.Delete("/catalog/install-jobs/{id}", workloadsHandler.CatalogCancelInstallJob)
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
	return r
}
