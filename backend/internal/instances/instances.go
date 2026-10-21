// Package instances groups the objects of a cluster into "instances" (Roadmap Phase 77a): one
// installed copy of an application — everything a Marketplace install (Helm) or "Install from an
// image" created under one release / instance name — and says what deleting it would remove and
// what it would leave behind.
//
// It reads nothing itself: it is handed what the caller may list, and it writes nothing. An instance
// is recognised by the standard labels `app.kubernetes.io/instance` and `app.kubernetes.io/managed-by`
// (`vinaire` for an image install, `Helm` for a chart); the labels are cluster data and untrusted, so
// they are only used to group and are validated before they are returned. A Secret contributes its
// name and type, never a value.
package instances

import (
	"regexp"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Origins an instance can have.
const (
	OriginImage = "image" // "Install from an image" (managed-by: vinaire)
	OriginHelm  = "helm"  // a Marketplace / Helm install (managed-by: Helm)
)

const (
	labelInstance  = "app.kubernetes.io/instance"
	labelName      = "app.kubernetes.io/name"
	labelVersion   = "app.kubernetes.io/version"
	labelManagedBy = "app.kubernetes.io/managed-by"
	helmSecretType = "helm.sh/release.v1"

	// MaxInstances bounds the list.
	MaxInstances = 500
)

var (
	nameRe    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
)

var systemNamespaces = map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true}

// Snapshot is what instances are built from. A nil slice means nothing of that kind was listed.
type Snapshot struct {
	StatefulSets []appsv1.StatefulSet
	Deployments  []appsv1.Deployment
	Services     []corev1.Service
	PVCs         []corev1.PersistentVolumeClaim
	Secrets      []corev1.Secret
}

// Part is one object of an instance.
type Part struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Detail is short text: "2/3 ready", "10Gi", the Secret's type.
	Detail string `json:"detail,omitempty"`
	// Fate says what deleting the instance does to this object: "removed" or "stays".
	Fate string `json:"fate"`
	// Why explains a "stays".
	Why string `json:"why,omitempty"`
}

// Instance is one installed copy of an application.
type Instance struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Origin    string `json:"origin"`
	App       string `json:"app,omitempty"`
	Version   string `json:"version,omitempty"`
	// System is true in the cluster's own namespaces; deleting is refused there.
	System bool `json:"system"`
	// Image is the image of an image-installed instance's `app` container, and PreviousImage the one
	// before its last upgrade (Phase 77d); both empty for a chart.
	Image         string `json:"image,omitempty"`
	PreviousImage string `json:"previousImage,omitempty"`
	Parts         []Part `json:"parts"`
	// Stays counts the parts that remain after a delete (volume claims, Secrets nobody owns).
	Stays int `json:"stays"`
}

type key struct{ ns, name, origin string }

func originOf(l map[string]string) string {
	switch l[labelManagedBy] {
	case "vinaire":
		return OriginImage
	case "Helm":
		return OriginHelm
	}
	return ""
}

func keyOf(ns string, l map[string]string) (key, bool) {
	o := originOf(l)
	n := l[labelInstance]
	if o == "" || !nameRe.MatchString(n) {
		return key{}, false
	}
	return key{ns, n, o}, true
}

// ownedByWorkload reports whether an object belongs to a workload through an ownerReference, which
// is how "Install from an image" ties its Secret and Service to the StatefulSet.
func ownedByWorkload(refs []metav1.OwnerReference) bool {
	for _, r := range refs {
		if r.Kind == "StatefulSet" || r.Kind == "Deployment" {
			return true
		}
	}
	return false
}

// Build groups the snapshot, sorted by namespace then name.
func Build(s Snapshot) []Instance {
	m := map[key]*Instance{}
	get := func(k key, l map[string]string) *Instance {
		in := m[k]
		if in == nil {
			in = &Instance{Namespace: k.ns, Name: k.name, Origin: k.origin, System: systemNamespaces[k.ns], Parts: []Part{}}
			m[k] = in
		}
		if in.App == "" && nameRe.MatchString(l[labelName]) {
			in.App = l[labelName]
		}
		if in.Version == "" && versionRe.MatchString(l[labelVersion]) {
			in.Version = l[labelVersion]
		}
		return in
	}
	// Helm removes everything its release created; an image install removes what its workload owns.
	removed := func(k key, refs []metav1.OwnerReference) bool {
		return k.origin == OriginHelm || ownedByWorkload(refs)
	}
	const unowned = "nothing owns it, so it stays"
	for _, w := range s.StatefulSets {
		if k, ok := keyOf(w.Namespace, w.Labels); ok {
			in := get(k, w.Labels)
			if k.origin == OriginImage {
				for _, c := range w.Spec.Template.Spec.Containers {
					if c.Name == "app" && imageRe.MatchString(c.Image) {
						in.Image = c.Image
					}
				}
				if p := w.Annotations[AnnotationPreviousImage]; imageRe.MatchString(p) {
					in.PreviousImage = p
				}
			}
			in.Parts = append(in.Parts, Part{Kind: "StatefulSet", Name: w.Name, Detail: ready(w.Status.ReadyReplicas, w.Spec.Replicas), Fate: "removed"})
		}
	}
	for _, w := range s.Deployments {
		if k, ok := keyOf(w.Namespace, w.Labels); ok {
			in := get(k, w.Labels)
			in.Parts = append(in.Parts, Part{Kind: "Deployment", Name: w.Name, Detail: ready(w.Status.ReadyReplicas, w.Spec.Replicas), Fate: "removed"})
		}
	}
	for _, v := range s.Services {
		if k, ok := keyOf(v.Namespace, v.Labels); ok {
			in := get(k, v.Labels)
			in.Parts = append(in.Parts, fate(Part{Kind: "Service", Name: v.Name, Detail: string(v.Spec.Type)}, removed(k, v.OwnerReferences), unowned))
		}
	}
	for _, c := range s.PVCs {
		// A claim made from a StatefulSet's volumeClaimTemplate carries the set's selector labels
		// (name, instance) but not managed-by, so it joins the instance of that name already found.
		k, ok := keyOf(c.Namespace, c.Labels)
		if !ok && nameRe.MatchString(c.Labels[labelInstance]) {
			for _, o := range []string{OriginImage, OriginHelm} {
				if _, found := m[key{c.Namespace, c.Labels[labelInstance], o}]; found {
					k, ok = key{c.Namespace, c.Labels[labelInstance], o}, true
					break
				}
			}
		}
		if ok {
			in := get(k, c.Labels)
			q := c.Spec.Resources.Requests[corev1.ResourceStorage]
			in.Parts = append(in.Parts, Part{Kind: "PersistentVolumeClaim", Name: c.Name, Detail: q.String(), Fate: "stays",
				Why: "a volume claim outlives its workload; delete it on its own to free the data"})
		}
	}
	for _, sec := range s.Secrets {
		if string(sec.Type) == helmSecretType {
			continue
		}
		if k, ok := keyOf(sec.Namespace, sec.Labels); ok {
			in := get(k, sec.Labels)
			in.Parts = append(in.Parts, fate(Part{Kind: "Secret", Name: sec.Name, Detail: string(sec.Type)}, removed(k, sec.OwnerReferences), unowned))
		}
	}
	out := make([]Instance, 0, len(m))
	for _, in := range m {
		// An instance needs a workload; a stray labelled Service is not one.
		if !hasWorkload(in) {
			continue
		}
		sort.Slice(in.Parts, func(i, j int) bool {
			if in.Parts[i].Kind != in.Parts[j].Kind {
				return in.Parts[i].Kind < in.Parts[j].Kind
			}
			return in.Parts[i].Name < in.Parts[j].Name
		})
		for _, p := range in.Parts {
			if p.Fate == "stays" {
				in.Stays++
			}
		}
		out = append(out, *in)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > MaxInstances {
		out = out[:MaxInstances]
	}
	return out
}

func hasWorkload(in *Instance) bool {
	for _, p := range in.Parts {
		if p.Kind == "StatefulSet" || p.Kind == "Deployment" {
			return true
		}
	}
	return false
}

func fate(p Part, removed bool, why string) Part {
	if removed {
		p.Fate = "removed"
	} else {
		p.Fate, p.Why = "stays", why
	}
	return p
}

func ready(n int32, want *int32) string {
	w := int32(1)
	if want != nil {
		w = *want
	}
	return strconv.Itoa(int(n)) + "/" + strconv.Itoa(int(w)) + " ready"
}
