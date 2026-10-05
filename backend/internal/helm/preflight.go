package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	authzv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Preflight answers, before anything is created, "may this caller install this
// chart?". Helm acts as the caller, and before it creates each object it reads
// it (`get`), so an install by someone without those rights fails halfway —
// after some objects already exist — with one raw Kubernetes sentence about
// whichever kind it reached first. Rendering the chart locally and asking the
// cluster (a SelfSubjectAccessReview, as the caller) about every kind it would
// create lets the UI list *everything* that is missing, and lets the install
// refuse to start. Nothing is written: the render is client-side only.
//
// It is advisory in the "pass" direction: the cluster's real RBAC still decides
// during the install. A kind it can't map (a CRD the chart itself installs) is
// reported as unchecked rather than blocking.

// ErrRender marks a preflight that could not render the chart. The install itself
// then runs without a preflight (it renders with the cluster's exact capabilities
// and fails with the real cause if the chart really is broken), instead of the
// check blocking an install it merely failed to emulate.
var ErrRender = errors.New("the chart could not be rendered for the check")

// maxPreflightChecks bounds the access reviews one preflight may issue.
const maxPreflightChecks = 80

// Needed is one resource the install needs rights on and doesn't have.
type Needed struct {
	Group     string   `json:"group"`
	Resource  string   `json:"resource"`
	Namespace string   `json:"namespace,omitempty"` // empty for a cluster-scoped kind
	Verbs     []string `json:"verbs"`
}

// PreflightResult is the outcome of a preflight.
type PreflightResult struct {
	// Objects is how many objects the chart renders to.
	Objects int `json:"objects"`
	// Missing lists what the caller may not do; empty means the install may go ahead.
	Missing []Needed `json:"missing"`
	// Unchecked lists kinds that couldn't be mapped to a resource (typically CRDs
	// the chart installs itself) — they weren't verified either way.
	Unchecked []string `json:"unchecked,omitempty"`
}

// OK reports whether nothing was found missing.
func (p *PreflightResult) OK() bool { return len(p.Missing) == 0 }

// String renders the gaps as one human sentence, for the error an install
// refused by preflight returns.
func (p *PreflightResult) String() string {
	parts := make([]string, 0, len(p.Missing))
	for _, m := range p.Missing {
		res := m.Resource
		if m.Group != "" {
			res += "." + m.Group
		}
		where := "cluster-wide"
		if m.Namespace != "" {
			where = "in namespace " + m.Namespace
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", strings.Join(m.Verbs, "/"), res, where))
	}
	return strings.Join(parts, "; ")
}

type accessKey struct{ group, resource, namespace string }

// renderObjects renders the chart client-side and returns the objects it would
// create (manifest plus non-test hooks).
func renderObjects(ctx context.Context, cfg *rest.Config, req InstallRequest, rl *Releases) ([]*unstructured.Unstructured, error) {
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
	in.DryRun = true
	in.ClientOnly = true
	in.DisableHooks = false
	in.Timeout = opTimeout
	// A client-side render assumes an old Kubernetes with few APIs, so a chart that
	// needs `kubeVersion: >=1.25` or checks `.Capabilities.APIVersions.Has` would
	// fail or render differently. Tell it what the real cluster serves.
	if dc, derr := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(cfg)); derr == nil {
		if v, verr := dc.ServerVersion(); verr == nil {
			in.KubeVersion = &chartutil.KubeVersion{Version: v.GitVersion, Major: v.Major, Minor: v.Minor}
		}
		if groups, gerr := dc.ServerGroups(); gerr == nil {
			for _, g := range groups.Groups {
				for _, gv := range g.Versions {
					in.APIVersions = append(in.APIVersions, gv.GroupVersion)
				}
			}
		}
	}
	rel, err := in.RunWithContext(ctx, ch, vals)
	if err != nil {
		return nil, fmt.Errorf("%w: %w: the chart doesn't render with these values: %v", ErrInvalid, ErrRender, err)
	}
	docs := rel.Manifest
	for _, h := range rel.Hooks {
		isTest := false
		for _, ev := range h.Events {
			if strings.HasPrefix(string(ev), "test") {
				isTest = true
			}
		}
		if !isTest {
			docs += "\n---\n" + h.Manifest
		}
	}
	return decodeObjects(docs)
}

func decodeObjects(docs string) ([]*unstructured.Unstructured, error) {
	dec := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(docs)), 4096)
	var out []*unstructured.Unstructured
	for {
		obj := map[string]any{}
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading the rendered manifest: %w", err)
		}
		if len(obj) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		if u.GetKind() == "" {
			continue
		}
		// A List kind wraps further objects.
		if u.IsList() {
			_ = u.EachListItem(func(o runtime.Object) error {
				if item, ok := o.(*unstructured.Unstructured); ok && item.GetKind() != "" {
					out = append(out, item)
				}
				return nil
			})
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// Preflight renders the chart and checks, as the caller, that every kind it
// would create is permitted. See the comment at the top of the file.
func (rl *Releases) Preflight(ctx context.Context, cfg *rest.Config, req InstallRequest) (*PreflightResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	objs, err := renderObjects(ctx, cfg, req, rl)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(rest.CopyConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("preparing the access check: %w", err)
	}
	mapper, mapErr := restGetter{cfg: cfg, namespace: req.Namespace}.ToRESTMapper()

	res := &PreflightResult{Objects: len(objs), Missing: []Needed{}}
	want := map[accessKey]map[string]bool{}
	add := func(k accessKey, verbs ...string) {
		if want[k] == nil {
			want[k] = map[string]bool{}
		}
		for _, v := range verbs {
			want[k][v] = true
		}
	}
	// Helm keeps the release itself as a Secret in the release's namespace.
	add(accessKey{"", "secrets", req.Namespace}, "get", "list", "create")
	if req.CreateNamespace {
		add(accessKey{"", "namespaces", ""}, "create")
	}

	unchecked := map[string]bool{}
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		if mapErr != nil {
			unchecked[gvk.Kind] = true
			continue
		}
		m, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			unchecked[gvk.Kind] = true
			continue
		}
		ns := ""
		if m.Scope.Name() == meta.RESTScopeNameNamespace {
			ns = o.GetNamespace()
			if ns == "" {
				ns = req.Namespace
			}
		}
		// Helm reads an object before creating it (to refuse to adopt one it
		// doesn't own), so both verbs are needed.
		add(accessKey{m.Resource.Group, m.Resource.Resource, ns}, "get", "create")
	}
	for k := range unchecked {
		res.Unchecked = append(res.Unchecked, k)
	}
	sort.Strings(res.Unchecked)

	if len(want) > maxPreflightChecks {
		return nil, fmt.Errorf("%w: the chart renders to too many kinds of object to check (%d)", ErrInvalid, len(want))
	}
	keys := make([]accessKey, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		return a.group+"/"+a.resource+"/"+a.namespace < b.group+"/"+b.resource+"/"+b.namespace
	})
	for _, k := range keys {
		var denied []string
		for _, verb := range sortedVerbs(want[k]) {
			ok, err := canI(ctx, cs, verb, k)
			if err != nil {
				// can't ask — don't block on a failure of the check itself
				return res, nil
			}
			if !ok {
				denied = append(denied, verb)
			}
		}
		if len(denied) > 0 {
			res.Missing = append(res.Missing, Needed{Group: k.group, Resource: k.resource, Namespace: k.namespace, Verbs: denied})
		}
	}
	return res, nil
}

func sortedVerbs(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func canI(ctx context.Context, cs kubernetes.Interface, verb string, k accessKey) (bool, error) {
	r, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{
			Verb: verb, Group: k.group, Resource: k.resource, Namespace: k.namespace,
		}},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	return r.Status.Allowed, nil
}
