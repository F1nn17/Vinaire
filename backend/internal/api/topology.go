package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// TopologyNode is one resource in the graph. Kind is one of "ingress",
// "service", "deployment", "pod", "configmap", "secret".
type TopologyNode struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	Namespace   string      `json:"namespace"`
	StatusText  string      `json:"statusText"`
	StatusColor string      `json:"statusColor"`
	Extra       [][2]string `json:"extra"`
}

// TopologyEdge is a directed relationship between two TopologyNode.ID
// values — what points at what, not necessarily "who owns whom".
type TopologyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// TopologyGraph matches the frontend's TopologyData type
// (frontend/src/lib/types.ts).
type TopologyGraph struct {
	Namespace string         `json:"namespace"`
	Nodes     []TopologyNode `json:"nodes"`
	Edges     []TopologyEdge `json:"edges"`
}

// Topology handles GET /api/v1/topology?namespace=. Relationships are
// computed here, server-side, from owner references and label selectors —
// the frontend just lays out and draws whatever graph it's given. See
// ROADMAP.md Phase 7 for the (deliberately simplified) matching rules.
func (h *Handler) Topology(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	if _, _, ok := h.authorize(w, r, namespace, "read"); !ok {
		return
	}

	ctx := r.Context()

	ingresses, err := k8s.ListIngresses(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	services, err := k8s.ListServices(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	deployments, err := k8s.ListDeployments(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	pods, err := k8s.ListPods(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	configMaps, err := k8s.ListConfigMaps(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	secrets, err := k8s.ListSecrets(ctx, cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	respondJSON(w, buildTopologyGraph(namespace, ingresses, services, deployments, pods, configMaps, secrets))
}

func nodeID(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

func buildTopologyGraph(
	namespace string,
	ingresses []networkingv1.Ingress,
	services []corev1.Service,
	deployments []appsv1.Deployment,
	pods []corev1.Pod,
	configMaps []corev1.ConfigMap,
	secrets []corev1.Secret,
) TopologyGraph {
	var nodes []TopologyNode
	nodeIndex := map[string]int{}
	addNode := func(n TopologyNode) {
		nodeIndex[n.ID] = len(nodes)
		nodes = append(nodes, n)
	}

	for _, ing := range ingresses {
		addNode(ingressNode(ing))
	}
	for _, s := range services {
		addNode(serviceNode(s))
	}
	for _, d := range deployments {
		addNode(deploymentTopologyNode(d))
	}
	for _, p := range pods {
		addNode(podTopologyNode(p))
	}
	for _, cm := range configMaps {
		addNode(configMapTopologyNode(cm))
	}
	for _, s := range secrets {
		addNode(secretTopologyNode(s))
	}

	serviceByName := make(map[string]corev1.Service, len(services))
	for _, s := range services {
		serviceByName[s.Name] = s
	}

	var edges []TopologyEdge
	usedConfigMaps := map[string]bool{}
	usedSecrets := map[string]bool{}

	// Ingress -> Service
	for _, ing := range ingresses {
		ingID := nodeID("ingress", ing.Namespace, ing.Name)
		for _, svcName := range ingressBackendServiceNames(ing) {
			if svc, ok := serviceByName[svcName]; ok {
				edges = append(edges, TopologyEdge{From: ingID, To: nodeID("service", svc.Namespace, svc.Name)})
			}
		}
	}

	// Service -> Pods (selector match)
	for _, s := range services {
		if len(s.Spec.Selector) == 0 {
			continue
		}
		svcID := nodeID("service", s.Namespace, s.Name)
		for _, p := range pods {
			if labelsMatch(s.Spec.Selector, p.Labels) {
				edges = append(edges, TopologyEdge{From: svcID, To: nodeID("pod", p.Namespace, p.Name)})
			}
		}
	}

	// Deployment -> Pods (selector match, skipping the ReplicaSet hop) and
	// Deployment -> ConfigMap/Secret (scanned from the pod template).
	for _, d := range deployments {
		depID := nodeID("deployment", d.Namespace, d.Name)

		selector := map[string]string{}
		if d.Spec.Selector != nil {
			selector = d.Spec.Selector.MatchLabels
		}
		for _, p := range pods {
			if labelsMatch(selector, p.Labels) {
				edges = append(edges, TopologyEdge{From: depID, To: nodeID("pod", p.Namespace, p.Name)})
			}
		}

		for _, cmName := range configMapRefsInPodSpec(d.Spec.Template.Spec) {
			cmID := nodeID("configmap", d.Namespace, cmName)
			if _, ok := nodeIndex[cmID]; ok {
				edges = append(edges, TopologyEdge{From: depID, To: cmID})
				usedConfigMaps[cmID] = true
			}
		}
		for _, secretName := range secretRefsInPodSpec(d.Spec.Template.Spec) {
			secretID := nodeID("secret", d.Namespace, secretName)
			if _, ok := nodeIndex[secretID]; ok {
				edges = append(edges, TopologyEdge{From: depID, To: secretID})
				usedSecrets[secretID] = true
			}
		}
	}

	// ConfigMap/Secret status depends on whether anything actually
	// references them — only known once every deployment has been scanned.
	for id, idx := range nodeIndex {
		switch nodes[idx].Kind {
		case "configmap":
			nodes[idx].StatusText, nodes[idx].StatusColor = inUseStatus(usedConfigMaps[id])
		case "secret":
			nodes[idx].StatusText, nodes[idx].StatusColor = inUseStatus(usedSecrets[id])
		}
	}

	return TopologyGraph{Namespace: namespace, Nodes: nodes, Edges: edges}
}

func inUseStatus(used bool) (string, string) {
	if used {
		return "In use", string(StatusOK)
	}
	return "Unused", string(StatusWarn)
}

// labelsMatch reports whether every key in selector is present in labels
// with the same value. An empty selector deliberately matches nothing —
// otherwise it would match every pod in the namespace, which is never the
// intent of an empty Service/Deployment selector in practice.
func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func ingressBackendServiceNames(ing networkingv1.Ingress) []string {
	var names []string
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		names = append(names, ing.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				names = append(names, path.Backend.Service.Name)
			}
		}
	}
	return names
}

// configMapRefsInPodSpec scans envFrom/env[].valueFrom/volumes (containers,
// init containers, and volumes) for ConfigMap references.
func configMapRefsInPodSpec(spec corev1.PodSpec) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}

	for _, c := range allContainers(spec) {
		for _, ef := range c.EnvFrom {
			if ef.ConfigMapRef != nil {
				add(ef.ConfigMapRef.Name)
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil && e.ValueFrom.ConfigMapKeyRef != nil {
				add(e.ValueFrom.ConfigMapKeyRef.Name)
			}
		}
	}
	for _, v := range spec.Volumes {
		if v.ConfigMap != nil {
			add(v.ConfigMap.Name)
		}
	}
	return names
}

// secretRefsInPodSpec is configMapRefsInPodSpec's Secret equivalent.
func secretRefsInPodSpec(spec corev1.PodSpec) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}

	for _, c := range allContainers(spec) {
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil {
				add(ef.SecretRef.Name)
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				add(e.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	for _, v := range spec.Volumes {
		if v.Secret != nil {
			add(v.Secret.SecretName)
		}
	}
	return names
}

func allContainers(spec corev1.PodSpec) []corev1.Container {
	all := make([]corev1.Container, 0, len(spec.Containers)+len(spec.InitContainers))
	all = append(all, spec.InitContainers...)
	all = append(all, spec.Containers...)
	return all
}

func ingressNode(ing networkingv1.Ingress) TopologyNode {
	statusText, color := "Pending", StatusWarn
	if len(ing.Status.LoadBalancer.Ingress) > 0 {
		statusText, color = "Active", StatusOK
	}

	class := "—"
	if ing.Spec.IngressClassName != nil {
		class = *ing.Spec.IngressClassName
	}

	tls := "None"
	if len(ing.Spec.TLS) > 0 {
		tls = fmt.Sprintf("%d host(s)", len(ing.Spec.TLS))
	}

	hostCount, pathCount := 0, 0
	hostSeen := map[string]struct{}{}
	for _, rule := range ing.Spec.Rules {
		if rule.Host != "" {
			if _, ok := hostSeen[rule.Host]; !ok {
				hostSeen[rule.Host] = struct{}{}
				hostCount++
			}
		}
		if rule.HTTP != nil {
			pathCount += len(rule.HTTP.Paths)
		}
	}

	return TopologyNode{
		ID:          nodeID("ingress", ing.Namespace, ing.Name),
		Kind:        "ingress",
		Name:        ing.Name,
		Namespace:   ing.Namespace,
		StatusText:  statusText,
		StatusColor: string(color),
		Extra: [][2]string{
			{"Class", class},
			{"TLS", tls},
			{"Rules", fmt.Sprintf("%d host(s), %d path(s)", hostCount, pathCount)},
		},
	}
}

func serviceNode(s corev1.Service) TopologyNode {
	ports := make([]string, 0, len(s.Spec.Ports))
	for _, p := range s.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%d", p.Port))
	}
	portsStr := strings.Join(ports, ", ")
	if portsStr == "" {
		portsStr = "—"
	}

	return TopologyNode{
		ID:          nodeID("service", s.Namespace, s.Name),
		Kind:        "service",
		Name:        s.Name,
		Namespace:   s.Namespace,
		StatusText:  "Active",
		StatusColor: string(StatusOK),
		Extra: [][2]string{
			{"Type", string(s.Spec.Type)},
			{"Cluster IP", s.Spec.ClusterIP},
			{"Ports", portsStr},
		},
	}
}

func deploymentTopologyNode(d appsv1.Deployment) TopologyNode {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	status, color := readyStatus(d.Status.ReadyReplicas, desired)

	image := "—"
	if len(d.Spec.Template.Spec.Containers) > 0 {
		image = d.Spec.Template.Spec.Containers[0].Image
	}

	return TopologyNode{
		ID:          nodeID("deployment", d.Namespace, d.Name),
		Kind:        "deployment",
		Name:        d.Name,
		Namespace:   d.Namespace,
		StatusText:  status,
		StatusColor: string(color),
		Extra: [][2]string{
			{"Replicas", fmt.Sprintf("%d/%d ready", d.Status.ReadyReplicas, desired)},
			{"Strategy", string(d.Spec.Strategy.Type)},
			{"Image", image},
		},
	}
}

func podTopologyNode(p corev1.Pod) TopologyNode {
	status, color := podStatus(p)

	image := "—"
	if len(p.Spec.Containers) > 0 {
		image = p.Spec.Containers[0].Image
	}

	return TopologyNode{
		ID:          nodeID("pod", p.Namespace, p.Name),
		Kind:        "pod",
		Name:        p.Name,
		Namespace:   p.Namespace,
		StatusText:  status,
		StatusColor: string(color),
		Extra: [][2]string{
			{"Node", p.Spec.NodeName},
			{"Restarts", fmt.Sprintf("%d", podRestarts(p))},
			{"Image", image},
			{"Age", formatAge(p.CreationTimestamp.Time)},
		},
	}
}

func configMapTopologyNode(cm corev1.ConfigMap) TopologyNode {
	keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	for k := range cm.BinaryData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keysStr := strings.Join(keys, ", ")
	if keysStr == "" {
		keysStr = "—"
	}

	return TopologyNode{
		ID:        nodeID("configmap", cm.Namespace, cm.Name),
		Kind:      "configmap",
		Name:      cm.Name,
		Namespace: cm.Namespace,
		// StatusText/StatusColor filled in by buildTopologyGraph once
		// every deployment's references have been scanned.
		Extra: [][2]string{{"Keys", keysStr}},
	}
}

func secretTopologyNode(s corev1.Secret) TopologyNode {
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	keysStr := strings.Join(keys, ", ")
	if keysStr == "" {
		keysStr = "—"
	}

	return TopologyNode{
		ID:        nodeID("secret", s.Namespace, s.Name),
		Kind:      "secret",
		Name:      s.Name,
		Namespace: s.Namespace,
		Extra: [][2]string{
			{"Type", string(s.Type)},
			{"Keys", keysStr},
		},
	}
}
