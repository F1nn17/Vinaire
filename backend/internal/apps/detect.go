// Package apps recognises well-known applications among the pods of a cluster (Roadmap Phase 74), so
// that the Dashboards page can offer a PostgreSQL, Redis, MinIO or Keycloak dashboard when — and
// only when — there is one to look at.
//
// It reads nothing itself: it is handed the pods the caller may list. An application is recognised
// by the repository part of a container image (`postgres`, `bitnami/postgresql`,
// `quay.io/minio/aistor/minio`), never by a label a person could have set to anything, and an
// exporter (`postgres-exporter`, `redis_exporter`) is not the application it exports.
package apps

import (
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// rule recognises one application by its images' repositories.
type rule struct {
	id    string
	title string
	image *regexp.Regexp
}

var rules = []rule{
	{"postgresql", "PostgreSQL", regexp.MustCompile(`(^|/)(postgres|postgresql|postgresql-repmgr|postgresql-ha|spilo-[0-9]+)$`)},
	{"redis", "Redis / Valkey", regexp.MustCompile(`(^|/)(redis|redis-stack|redis-stack-server|valkey)$`)},
	{"minio", "MinIO", regexp.MustCompile(`(^|/)minio$`)},
	{"keycloak", "Keycloak", regexp.MustCompile(`(^|/)keycloak$`)},
}

// maxInstances bounds how many instances one application lists.
const maxInstances = 50

// Instance is one running copy of an application: a workload (StatefulSet, Deployment…) and its pods.
type Instance struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Image     string `json:"image"`
	Pods      int    `json:"pods"`
	Ready     int    `json:"ready"`
}

// App is an application found in the cluster.
type App struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Instances []Instance `json:"instances"`
}

// repoAndTag splits an image reference into its repository path and tag ("" when it has none or
// only a digest). "quay.io:443/a/b:1" keeps its registry port: a colon only starts a tag after the
// last slash.
func repoAndTag(image string) (repo, tag string) {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndexByte(image, '/')
	if colon := strings.LastIndexByte(image, ':'); colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, ""
}

// workload names the thing that owns a pod: its StatefulSet, its Deployment (a ReplicaSet's name
// without the hash), or the pod itself.
func workload(p *corev1.Pod) (kind, name string) {
	for _, o := range p.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			if o.Kind == "ReplicaSet" {
				if i := strings.LastIndexByte(o.Name, '-'); i > 0 {
					return "Deployment", o.Name[:i]
				}
			}
			return o.Kind, o.Name
		}
	}
	return "Pod", p.Name
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Detect finds the known applications among pods. An application with no pod is not listed; a pod
// that has finished (Succeeded or Failed — a completed Job) does not count.
func Detect(pods []corev1.Pod) []App {
	type key struct{ app, ns, kind, name string }
	found := map[key]*Instance{}
	titles := map[string]string{}
	for i := range pods {
		p := &pods[i]
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, c := range p.Spec.Containers {
			repo, tag := repoAndTag(c.Image)
			for _, r := range rules {
				if !r.image.MatchString(repo) {
					continue
				}
				kind, name := workload(p)
				k := key{r.id, p.Namespace, kind, name}
				in := found[k]
				if in == nil {
					img := repo
					if tag != "" {
						img += ":" + tag
					}
					in = &Instance{Namespace: p.Namespace, Name: name, Kind: kind, Image: img}
					found[k] = in
					titles[r.id] = r.title
				}
				in.Pods++
				if podReady(p) {
					in.Ready++
				}
				break
			}
		}
	}
	byApp := map[string][]Instance{}
	for k, in := range found {
		byApp[k.app] = append(byApp[k.app], *in)
	}
	var out []App
	for _, r := range rules { // the rules' order is the display order
		ins := byApp[r.id]
		if len(ins) == 0 {
			continue
		}
		sort.Slice(ins, func(i, j int) bool {
			if ins[i].Namespace != ins[j].Namespace {
				return ins[i].Namespace < ins[j].Namespace
			}
			return ins[i].Name < ins[j].Name
		})
		if len(ins) > maxInstances {
			ins = ins[:maxInstances]
		}
		out = append(out, App{ID: r.id, Title: titles[r.id], Instances: ins})
	}
	return out
}
