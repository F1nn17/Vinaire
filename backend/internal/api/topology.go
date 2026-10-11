package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// TopologyNode is one resource in the graph. Kind is one of "ingress",
// "service", "deployment", "statefulset", "daemonset", "job", "pod", "configmap", "secret".
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
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	// "all" is every namespace: the lists below take an empty namespace to mean that
	scope := namespace
	if namespace == "all" {
		scope = ""
	}

	ctx := r.Context()

	ingresses, err := k8s.ListIngresses(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	services, err := k8s.ListServices(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	deployments, err := k8s.ListDeployments(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	// A kind the caller may not list leaves its nodes out; it doesn't blank the whole graph.
	statefulsets, _ := k8s.ListStatefulSets(ctx, clientset, scope)
	daemonsets, _ := k8s.ListDaemonSets(ctx, clientset, scope)
	jobs, _ := k8s.ListJobs(ctx, clientset, scope)
	pods, err := k8s.ListPods(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	configMaps, err := k8s.ListConfigMaps(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	secrets, err := k8s.ListSecrets(ctx, clientset, scope)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	respondJSON(w, buildTopologyGraph(namespace, ingresses, services, deployments, statefulsets, daemonsets, jobs, pods, configMaps, secrets))
}

func nodeID(kind, namespace, name string) string {
	return kind + "/" + namespace + "/" + name
}

// topologyWorkload is what the graph needs of any pod-owning kind: who it is, which pods are its
// own, and what its pod template refers to.
type topologyWorkload struct {
	node     TopologyNode
	selector map[string]string
	spec     corev1.PodSpec
}

func buildTopologyGraph(
	namespace string,
	ingresses []networkingv1.Ingress,
	services []corev1.Service,
	deployments []appsv1.Deployment,
	statefulsets []appsv1.StatefulSet,
	daemonsets []appsv1.DaemonSet,
	jobs []batchv1.Job,
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
	matchLabels := func(sel *metav1.LabelSelector) map[string]string {
		if sel == nil {
			return map[string]string{}
		}
		return sel.MatchLabels
	}

	var workloads []topologyWorkload
	for _, d := range deployments {
		workloads = append(workloads, topologyWorkload{deploymentTopologyNode(d), matchLabels(d.Spec.Selector), d.Spec.Template.Spec})
	}
	for _, st := range statefulsets {
		workloads = append(workloads, topologyWorkload{statefulSetTopologyNode(st), matchLabels(st.Spec.Selector), st.Spec.Template.Spec})
	}
	for _, ds := range daemonsets {
		workloads = append(workloads, topologyWorkload{daemonSetTopologyNode(ds), matchLabels(ds.Spec.Selector), ds.Spec.Template.Spec})
	}
	for _, j := range jobs {
		workloads = append(workloads, topologyWorkload{jobTopologyNode(j), matchLabels(j.Spec.Selector), j.Spec.Template.Spec})
	}

	for _, ing := range ingresses {
		addNode(ingressNode(ing))
	}
	for _, s := range services {
		addNode(serviceNode(s))
	}
	for _, w := range workloads {
		addNode(w.node)
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

	// With every namespace in one graph, a name alone no longer says which object is meant:
	// everything is matched inside its own namespace.
	serviceByKey := make(map[string]corev1.Service, len(services))
	for _, s := range services {
		serviceByKey[s.Namespace+"/"+s.Name] = s
	}

	var edges []TopologyEdge
	usedConfigMaps := map[string]bool{}
	usedSecrets := map[string]bool{}

	// Ingress -> Service
	for _, ing := range ingresses {
		ingID := nodeID("ingress", ing.Namespace, ing.Name)
		for _, svcName := range ingressBackendServiceNames(ing) {
			if svc, ok := serviceByKey[ing.Namespace+"/"+svcName]; ok {
				edges = append(edges, TopologyEdge{From: ingID, To: nodeID("service", svc.Namespace, svc.Name)})
			}
		}
	}

	// Service -> Pods (selector match, same namespace)
	for _, s := range services {
		if len(s.Spec.Selector) == 0 {
			continue
		}
		svcID := nodeID("service", s.Namespace, s.Name)
		for _, p := range pods {
			if p.Namespace == s.Namespace && labelsMatch(s.Spec.Selector, p.Labels) {
				edges = append(edges, TopologyEdge{From: svcID, To: nodeID("pod", p.Namespace, p.Name)})
			}
		}
	}

	// Workload -> Pods (selector match, skipping the ReplicaSet hop) and Workload -> ConfigMap/Secret
	// (scanned from the pod template).
	for _, w := range workloads {
		wID := w.node.ID
		if len(w.selector) > 0 {
			for _, p := range pods {
				if p.Namespace == w.node.Namespace && labelsMatch(w.selector, p.Labels) {
					edges = append(edges, TopologyEdge{From: wID, To: nodeID("pod", p.Namespace, p.Name)})
				}
			}
		}
		for _, cmName := range configMapRefsInPodSpec(w.spec) {
			cmID := nodeID("configmap", w.node.Namespace, cmName)
			if _, ok := nodeIndex[cmID]; ok {
				edges = append(edges, TopologyEdge{From: wID, To: cmID})
				usedConfigMaps[cmID] = true
			}
		}
		for _, secretName := range secretRefsInPodSpec(w.spec) {
			secretID := nodeID("secret", w.node.Namespace, secretName)
			if _, ok := nodeIndex[secretID]; ok {
				edges = append(edges, TopologyEdge{From: wID, To: secretID})
				usedSecrets[secretID] = true
			}
		}
	}

	// ConfigMap/Secret status depends on whether anything actually
	// references them — only known once every workload has been scanned.
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

func statefulSetTopologyNode(st appsv1.StatefulSet) TopologyNode {
	desired := int32(1)
	if st.Spec.Replicas != nil {
		desired = *st.Spec.Replicas
	}
	status, color := readyStatus(st.Status.ReadyReplicas, desired)
	image := "—"
	if len(st.Spec.Template.Spec.Containers) > 0 {
		image = st.Spec.Template.Spec.Containers[0].Image
	}
	return TopologyNode{
		ID: nodeID("statefulset", st.Namespace, st.Name), Kind: "statefulset", Name: st.Name, Namespace: st.Namespace,
		StatusText: status, StatusColor: string(color),
		Extra: [][2]string{{"Replicas", fmt.Sprintf("%d/%d ready", st.Status.ReadyReplicas, desired)}, {"Image", image}},
	}
}

func daemonSetTopologyNode(ds appsv1.DaemonSet) TopologyNode {
	status, color := readyStatus(ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)
	image := "—"
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		image = ds.Spec.Template.Spec.Containers[0].Image
	}
	return TopologyNode{
		ID: nodeID("daemonset", ds.Namespace, ds.Name), Kind: "daemonset", Name: ds.Name, Namespace: ds.Namespace,
		StatusText: status, StatusColor: string(color),
		Extra: [][2]string{{"Nodes", fmt.Sprintf("%d/%d ready", ds.Status.NumberReady, ds.Status.DesiredNumberScheduled)}, {"Image", image}},
	}
}

func jobTopologyNode(j batchv1.Job) TopologyNode {
	status, color := "Running", StatusOK
	switch {
	case j.Status.Failed > 0 && j.Status.Succeeded == 0 && j.Status.Active == 0:
		status, color = "Failed", StatusErr
	case j.Status.Succeeded > 0 && j.Status.Active == 0:
		status = "Complete"
	}
	return TopologyNode{
		ID: nodeID("job", j.Namespace, j.Name), Kind: "job", Name: j.Name, Namespace: j.Namespace,
		StatusText: status, StatusColor: string(color),
		Extra: [][2]string{{"Succeeded", fmt.Sprintf("%d", j.Status.Succeeded)}, {"Failed", fmt.Sprintf("%d", j.Status.Failed)}},
	}
}
