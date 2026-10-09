package helm

import (
	"context"
	"fmt"
	"sort"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

// What an install will create, and what the cluster lacks for it (Roadmap Phase 59). It is read
// from the same client-side render the access check already makes, plus a few reads as the
// caller (StorageClasses, IngressClasses) — nothing is written, and a caller who may not read
// them simply gets no warning from them.

// KindCount is "3 Deployments".
type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// VolumeNeed is storage the install asks for.
type VolumeNeed struct {
	Name         string `json:"name"`
	Size         string `json:"size,omitempty"`
	StorageClass string `json:"storageClass,omitempty"` // empty = the cluster's default
}

// Warning is something the install is likely to trip over. Code is stable for the interface;
// Detail is a short fact (a class name), phrased by the interface.
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Plan is the readable summary of a render.
type Plan struct {
	Created []KindCount  `json:"created"`
	Volumes []VolumeNeed `json:"volumes"`
	// internal: what the checks below need
	ingressClasses []string // classes named by rendered Ingresses
	hasIngress     bool
}

const maxVolumes = 50

// planOf summarises rendered objects.
func planOf(objs []*unstructured.Unstructured) Plan {
	counts := map[string]int{}
	var p Plan
	for _, o := range objs {
		kind := o.GetKind()
		counts[kind]++
		switch kind {
		case "PersistentVolumeClaim":
			size, _, _ := unstructured.NestedString(o.Object, "spec", "resources", "requests", "storage")
			class, _, _ := unstructured.NestedString(o.Object, "spec", "storageClassName")
			p.addVolume(VolumeNeed{Name: o.GetName(), Size: size, StorageClass: class})
		case "StatefulSet":
			tpls, _, _ := unstructured.NestedSlice(o.Object, "spec", "volumeClaimTemplates")
			replicas := int64(1)
			if v, found, _ := unstructured.NestedFieldNoCopy(o.Object, "spec", "replicas"); found {
				switch n := v.(type) { // a decoded manifest carries numbers as int64 or float64
				case int64:
					replicas = n
				case float64:
					replicas = int64(n)
				}
			}
			for _, t := range tpls {
				m, ok := t.(map[string]any)
				if !ok {
					continue
				}
				name, _, _ := unstructured.NestedString(m, "metadata", "name")
				size, _, _ := unstructured.NestedString(m, "spec", "resources", "requests", "storage")
				class, _, _ := unstructured.NestedString(m, "spec", "storageClassName")
				label := fmt.Sprintf("%s (%s × %d)", name, o.GetName(), replicas)
				p.addVolume(VolumeNeed{Name: label, Size: size, StorageClass: class})
			}
		case "Ingress":
			p.hasIngress = true
			class, _, _ := unstructured.NestedString(o.Object, "spec", "ingressClassName")
			if class != "" {
				p.ingressClasses = append(p.ingressClasses, class)
			}
		}
	}
	for k, n := range counts {
		p.Created = append(p.Created, KindCount{Kind: k, Count: n})
	}
	sort.Slice(p.Created, func(i, j int) bool {
		if p.Created[i].Count != p.Created[j].Count {
			return p.Created[i].Count > p.Created[j].Count
		}
		return p.Created[i].Kind < p.Created[j].Kind
	})
	if p.Created == nil {
		p.Created = []KindCount{}
	}
	if p.Volumes == nil {
		p.Volumes = []VolumeNeed{}
	}
	return p
}

func (p *Plan) addVolume(v VolumeNeed) {
	if len(p.Volumes) < maxVolumes {
		p.Volumes = append(p.Volumes, v)
	}
}

const defaultClassAnnotation = "storageclass.kubernetes.io/is-default-class"

// clusterWarnings compares the plan with what the cluster has. A read the caller isn't allowed
// (or that fails) yields no warning for that part — "unknown" is not "missing".
func clusterWarnings(ctx context.Context, cs kubernetes.Interface, p Plan) []Warning {
	out := []Warning{}
	if len(p.Volumes) > 0 {
		if list, err := cs.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{}); err == nil {
			out = append(out, storageWarnings(p.Volumes, list.Items)...)
		}
	}
	if p.hasIngress {
		if list, err := cs.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{}); err == nil {
			have := map[string]bool{}
			for _, c := range list.Items {
				have[c.Name] = true
			}
			if len(list.Items) == 0 {
				out = append(out, Warning{Code: "no-ingress-controller"})
			}
			seen := map[string]bool{}
			for _, c := range p.ingressClasses {
				if !have[c] && !seen[c] {
					seen[c] = true
					out = append(out, Warning{Code: "missing-ingress-class", Detail: c})
				}
			}
		}
	}
	return out
}

func storageWarnings(vols []VolumeNeed, classes []storagev1.StorageClass) []Warning {
	have := map[string]bool{}
	hasDefault := false
	for _, c := range classes {
		have[c.Name] = true
		if c.Annotations[defaultClassAnnotation] == "true" {
			hasDefault = true
		}
	}
	var out []Warning
	needsDefault, seen := false, map[string]bool{}
	for _, v := range vols {
		switch {
		case v.StorageClass == "":
			needsDefault = true
		case v.StorageClass == "-":
			// an explicit "no class": static provisioning, nothing to check
		case !have[v.StorageClass] && !seen[v.StorageClass]:
			seen[v.StorageClass] = true
			out = append(out, Warning{Code: "missing-storage-class", Detail: v.StorageClass})
		}
	}
	if needsDefault && !hasDefault {
		out = append([]Warning{{Code: "no-default-storage-class"}}, out...)
	}
	return out
}
