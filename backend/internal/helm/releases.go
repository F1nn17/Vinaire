package helm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/yaml"
)

// Errors the HTTP layer turns into statuses.
var (
	ErrInvalid   = errors.New("invalid request")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)

const (
	// RepoLabel records which catalog repository a release was installed from.
	// Helm itself doesn't remember, and an upgrade needs to know.
	RepoLabel = "vinaire.io/catalog-repo"

	maxValuesBytes = 256 << 10
	maxNotesBytes  = 16 << 10
	// Install/upgrade run synchronously inside one HTTP request, so a chart
	// that never settles can't hold it open forever.
	opTimeout = 5 * time.Minute
)

var (
	releaseNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,51}[a-z0-9])?$`)
	dnsLabelRe    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	nodeNameRe    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
)

// Releases runs Helm operations against a cluster as whoever's *rest.Config it
// is given — the caller's own identity — so the cluster's RBAC decides what a
// chart may create, not anything Vinaire holds. Helm keeps a release as a
// Secret in the release's namespace, so the caller needs access to Secrets there.
type Releases struct {
	Catalog *Catalog
}

// restGetter satisfies Helm's RESTClientGetter for an in-memory *rest.Config
// (there is no kubeconfig file) and a fixed default namespace.
type restGetter struct {
	cfg       *rest.Config
	namespace string
}

func (g restGetter) ToRESTConfig() (*rest.Config, error) { return rest.CopyConfig(g.cfg), nil }

func (g restGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(g.cfg))
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g restGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (g restGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	c := g.cfg
	raw := clientcmdapi.NewConfig()
	raw.Clusters["c"] = &clientcmdapi.Cluster{
		Server:                   c.Host,
		TLSServerName:            c.TLSClientConfig.ServerName,
		InsecureSkipTLSVerify:    c.TLSClientConfig.Insecure,
		CertificateAuthority:     c.TLSClientConfig.CAFile,
		CertificateAuthorityData: c.TLSClientConfig.CAData,
	}
	raw.AuthInfos["u"] = &clientcmdapi.AuthInfo{
		Token:                 c.BearerToken,
		TokenFile:             c.BearerTokenFile,
		ClientCertificate:     c.TLSClientConfig.CertFile,
		ClientCertificateData: c.TLSClientConfig.CertData,
		ClientKey:             c.TLSClientConfig.KeyFile,
		ClientKeyData:         c.TLSClientConfig.KeyData,
		Username:              c.Username,
		Password:              c.Password,
	}
	raw.Contexts["x"] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u", Namespace: g.namespace}
	raw.CurrentContext = "x"
	return clientcmd.NewDefaultClientConfig(*raw, &clientcmd.ConfigOverrides{})
}

// configuration is a Helm action.Configuration backed by Secrets in namespace
// ("" = all namespaces, for listing). Helm's own debug logging is dropped: it
// can include rendered manifests and values.
func configuration(cfg *rest.Config, namespace string) (*action.Configuration, error) {
	ac := new(action.Configuration)
	if err := ac.Init(restGetter{cfg: cfg, namespace: namespace}, namespace, "secret", func(string, ...interface{}) {}); err != nil {
		return nil, fmt.Errorf("preparing helm: %w", err)
	}
	return ac, nil
}

// Release is a release's metadata. It deliberately carries neither the values
// nor the manifest (both can hold secrets); values come from Values.
type Release struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	Chart        string    `json:"chart"`
	ChartVersion string    `json:"chartVersion"`
	AppVersion   string    `json:"appVersion,omitempty"`
	Updated      time.Time `json:"updated"`
	Description  string    `json:"description,omitempty"`
	// Repo is the catalog repository the release was installed from, when it was
	// installed through Vinaire.
	Repo string `json:"repo,omitempty"`
}

func toRelease(r *release.Release) Release {
	out := Release{Name: r.Name, Namespace: r.Namespace, Revision: r.Version}
	if r.Info != nil {
		out.Status = string(r.Info.Status)
		out.Updated = r.Info.LastDeployed.Time
		out.Description = r.Info.Description
	}
	if r.Chart != nil && r.Chart.Metadata != nil {
		out.Chart = r.Chart.Metadata.Name
		out.ChartVersion = r.Chart.Metadata.Version
		out.AppVersion = r.Chart.Metadata.AppVersion
	}
	out.Repo = r.Labels[RepoLabel]
	return out
}

// classify turns a Helm/Kubernetes failure into one of this package's sentinel
// errors where it recognises it, and adds a hint where the cause is a known
// trap. Anything else is returned as-is.
func classify(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case errors.Is(err, driver.ErrReleaseNotFound):
		return fmt.Errorf("release: %w", ErrNotFound)
	case strings.Contains(msg, "cannot re-use a name that is still in use") || errors.Is(err, driver.ErrReleaseExists):
		return fmt.Errorf("%w: a release with that name already exists in this namespace", ErrConflict)
	case strings.Contains(msg, "is forbidden"):
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case strings.Contains(msg, "ensure CRDs are installed first") || strings.Contains(msg, "no matches for kind"):
		return fmt.Errorf("%s\nHint: this chart uses custom resources whose CRDs aren't in the cluster yet. Install the CRDs first (many charts have a separate CRD chart or a value such as installCRDs=true) and try again", msg)
	}
	return err
}

func parseValues(valuesYAML string) (map[string]any, error) {
	if len(valuesYAML) > maxValuesBytes {
		return nil, fmt.Errorf("%w: values are larger than %d KiB", ErrInvalid, maxValuesBytes>>10)
	}
	vals := map[string]any{}
	if strings.TrimSpace(valuesYAML) == "" {
		return vals, nil
	}
	if err := yaml.Unmarshal([]byte(valuesYAML), &vals); err != nil {
		return nil, fmt.Errorf("%w: values are not valid YAML: %v", ErrInvalid, err)
	}
	if vals == nil {
		vals = map[string]any{}
	}
	return vals, nil
}

// pinToNode adds nodeSelector kubernetes.io/hostname=<node> to values. It only
// does so for a chart that actually has a top-level nodeSelector value; for any
// other layout guessing a path would silently do nothing, so it says so.
func pinToNode(vals map[string]any, ch *chart.Chart, node string) error {
	if !nodeNameRe.MatchString(node) {
		return fmt.Errorf("%w: %q is not a valid node name", ErrInvalid, node)
	}
	if _, ok := ch.Values["nodeSelector"]; !ok {
		return fmt.Errorf("%w: this chart has no top-level nodeSelector value, so Vinaire can't pin it to a node — set the right nodeSelector in the values instead", ErrInvalid)
	}
	sel := map[string]any{}
	switch cur := vals["nodeSelector"].(type) {
	case nil:
	case map[string]any:
		for k, v := range cur {
			sel[k] = v
		}
	default:
		return fmt.Errorf("%w: nodeSelector in the values must be a map", ErrInvalid)
	}
	sel["kubernetes.io/hostname"] = node
	vals["nodeSelector"] = sel
	return nil
}

// InstallRequest is everything an install needs.
type InstallRequest struct {
	Repo, Chart, Version string
	Namespace, Release   string
	ValuesYAML           string
	NodeName             string // optional: pin to this node
	CreateNamespace      bool
	Wait                 bool // wait for the resources to be ready
	// AllowDeprecated permits installing a chart its maintainers have deprecated
	// (no more fixes — security ones included); off, such an install is refused.
	AllowDeprecated bool
}

// refuseDeprecated is the check every install path runs on a loaded chart.
func refuseDeprecated(ch *chart.Chart, allow bool) error {
	if ch.Metadata != nil && ch.Metadata.Deprecated && !allow {
		return fmt.Errorf("%w: the chart %q is deprecated — it is no longer maintained and may be unsafe; tick \"install anyway\" if you really mean to use it", ErrInvalid, ch.Metadata.Name)
	}
	return nil
}

// InstallResult is the outcome of an install or upgrade.
type InstallResult struct {
	Release Release `json:"release"`
	Notes   string  `json:"notes,omitempty"`
}

// ansiRe matches terminal colour codes: some charts print their notes with them, and a browser shows
// them as "[0;34m" noise.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func result(r *release.Release) *InstallResult {
	res := &InstallResult{Release: toRelease(r)}
	if r.Info != nil {
		res.Notes = truncate(ansiRe.ReplaceAllString(r.Info.Notes, ""), maxNotesBytes)
	}
	return res
}

// Install installs a chart from the catalog.
func (rl *Releases) Install(ctx context.Context, cfg *rest.Config, req InstallRequest) (*InstallResult, error) {
	if !releaseNameRe.MatchString(req.Release) {
		return nil, fmt.Errorf("%w: release name must be lowercase letters, digits and dashes (max 53 characters)", ErrInvalid)
	}
	if !dnsLabelRe.MatchString(req.Namespace) {
		return nil, fmt.Errorf("%w: invalid namespace name", ErrInvalid)
	}
	vals, err := parseValues(req.ValuesYAML)
	if err != nil {
		return nil, err
	}
	ch, err := rl.Catalog.LoadChart(ctx, req.Repo, req.Chart, req.Version)
	if err != nil {
		return nil, err
	}
	if err := refuseDeprecated(ch, req.AllowDeprecated); err != nil {
		return nil, err
	}
	if req.NodeName != "" {
		if err := pinToNode(vals, ch, req.NodeName); err != nil {
			return nil, err
		}
	}
	ac, err := configuration(cfg, req.Namespace)
	if err != nil {
		return nil, err
	}
	in := action.NewInstall(ac)
	in.ReleaseName = req.Release
	in.Namespace = req.Namespace
	in.CreateNamespace = req.CreateNamespace
	in.Wait = req.Wait
	in.Timeout = opTimeout
	in.Labels = map[string]string{RepoLabel: req.Repo}

	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	rel, err := in.RunWithContext(ctx, ch, vals)
	if err != nil {
		return nil, classify(err)
	}
	return result(rel), nil
}

// UpgradeRequest upgrades an existing release to a catalog chart version. The
// values replace the release's current user-supplied values (the UI starts
// from them), rather than being merged invisibly.
type UpgradeRequest struct {
	Repo, Chart, Version string
	Namespace, Release   string
	ValuesYAML           string
	NodeName             string
	Wait                 bool
}

// Upgrade moves a release to another version of its chart.
func (rl *Releases) Upgrade(ctx context.Context, cfg *rest.Config, req UpgradeRequest) (*InstallResult, error) {
	if !releaseNameRe.MatchString(req.Release) || !dnsLabelRe.MatchString(req.Namespace) {
		return nil, fmt.Errorf("%w: invalid release or namespace name", ErrInvalid)
	}
	vals, err := parseValues(req.ValuesYAML)
	if err != nil {
		return nil, err
	}
	ac, err := configuration(cfg, req.Namespace)
	if err != nil {
		return nil, err
	}
	current, err := action.NewGet(ac).Run(req.Release)
	if err != nil {
		return nil, classify(err)
	}
	ch, err := rl.Catalog.LoadChart(ctx, req.Repo, req.Chart, req.Version)
	if err != nil {
		return nil, err
	}
	// Upgrading to a different chart is a replacement, not an upgrade — and an
	// easy mistake with a mistyped name.
	if current.Chart != nil && current.Chart.Metadata != nil && current.Chart.Metadata.Name != ch.Metadata.Name {
		return nil, fmt.Errorf("%w: release %s runs chart %s, not %s", ErrInvalid, req.Release, current.Chart.Metadata.Name, ch.Metadata.Name)
	}
	if req.NodeName != "" {
		if err := pinToNode(vals, ch, req.NodeName); err != nil {
			return nil, err
		}
	}
	up := action.NewUpgrade(ac)
	up.Namespace = req.Namespace
	up.Wait = req.Wait
	up.Timeout = opTimeout
	up.Labels = map[string]string{RepoLabel: req.Repo}

	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	rel, err := up.RunWithContext(ctx, req.Release, ch, vals)
	if err != nil {
		return nil, classify(err)
	}
	return result(rel), nil
}

// Rollback returns a release to an earlier revision (a new revision whose
// content is the old one's).
func (rl *Releases) Rollback(_ context.Context, cfg *rest.Config, namespace, name string, revision int) error {
	if !releaseNameRe.MatchString(name) || !dnsLabelRe.MatchString(namespace) || revision < 1 {
		return fmt.Errorf("%w: invalid release, namespace or revision", ErrInvalid)
	}
	ac, err := configuration(cfg, namespace)
	if err != nil {
		return err
	}
	rb := action.NewRollback(ac)
	rb.Version = revision
	rb.Timeout = opTimeout
	return classify(rb.Run(name))
}

// Uninstall removes a release and what it created.
func (rl *Releases) Uninstall(_ context.Context, cfg *rest.Config, namespace, name string) error {
	if !releaseNameRe.MatchString(name) || !dnsLabelRe.MatchString(namespace) {
		return fmt.Errorf("%w: invalid release or namespace name", ErrInvalid)
	}
	ac, err := configuration(cfg, namespace)
	if err != nil {
		return err
	}
	un := action.NewUninstall(ac)
	un.Timeout = opTimeout
	_, err = un.Run(name)
	return classify(err)
}

// List returns the releases in namespace ("" = every namespace the caller may
// read Secrets in), newest-changed first.
func (rl *Releases) List(_ context.Context, cfg *rest.Config, namespace string) ([]Release, error) {
	if namespace != "" && !dnsLabelRe.MatchString(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace name", ErrInvalid)
	}
	ac, err := configuration(cfg, namespace)
	if err != nil {
		return nil, err
	}
	l := action.NewList(ac)
	l.AllNamespaces = namespace == ""
	l.Deployed, l.Failed, l.Pending, l.Uninstalling = true, true, true, true
	l.SetStateMask()
	rels, err := l.Run()
	if err != nil {
		return nil, classify(err)
	}
	out := make([]Release, 0, len(rels))
	for _, r := range rels {
		out = append(out, toRelease(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// History lists every revision of a release, newest first.
func (rl *Releases) History(_ context.Context, cfg *rest.Config, namespace, name string) ([]Release, error) {
	if !releaseNameRe.MatchString(name) || !dnsLabelRe.MatchString(namespace) {
		return nil, fmt.Errorf("%w: invalid release or namespace name", ErrInvalid)
	}
	ac, err := configuration(cfg, namespace)
	if err != nil {
		return nil, err
	}
	rels, err := action.NewHistory(ac).Run(name)
	if err != nil {
		return nil, classify(err)
	}
	out := make([]Release, 0, len(rels))
	for _, r := range rels {
		out = append(out, toRelease(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}

// Values returns the values the user supplied for a release (not the chart's
// defaults), as YAML. They can contain passwords, which is why the HTTP layer
// audits this call the way it audits a Secret reveal.
func (rl *Releases) Values(_ context.Context, cfg *rest.Config, namespace, name string) (string, error) {
	if !releaseNameRe.MatchString(name) || !dnsLabelRe.MatchString(namespace) {
		return "", fmt.Errorf("%w: invalid release or namespace name", ErrInvalid)
	}
	ac, err := configuration(cfg, namespace)
	if err != nil {
		return "", err
	}
	vals, err := action.NewGetValues(ac).Run(name)
	if err != nil {
		return "", classify(err)
	}
	if len(vals) == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(vals)
	if err != nil {
		return "", fmt.Errorf("encoding values: %w", err)
	}
	return string(out), nil
}
