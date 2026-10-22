// Package diagnose says, in plain words, why a pod is not running or not ready (Roadmap Phase 75).
//
// Kubernetes already knows: the reason is in the pod's conditions, its containers' states and the
// events recorded against it — but as `ContainerCreating`, `CrashLoopBackOff` or "0/1 nodes are
// available: 1 Insufficient memory", which a person who has not met it before cannot act on. This
// reads those and returns findings with a fixed title (so it can be translated), the cluster's own
// words as the detail, and what to do about it. It only reads what it is handed and never makes up a
// cause: when it does not recognise the situation it says nothing rather than guess.
package diagnose

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Severity of a finding.
const (
	Error   = "error"
	Warning = "warning"
	Info    = "info"
)

// Finding is one thing wrong (or worth knowing) about a pod.
type Finding struct {
	Severity string `json:"severity"`
	// Code is a stable name of the situation, for tests and for the interface to key on.
	Code string `json:"code"`
	// Title is fixed text, so the interface can translate it.
	Title string `json:"title"`
	// Container names the container it is about, when it is about one.
	Container string `json:"container,omitempty"`
	// Detail is the cluster's own words (a message, an exit code), shortened.
	Detail string   `json:"detail,omitempty"`
	Hints  []string `json:"hints,omitempty"`
}

const maxDetail = 300

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDetail {
		s = s[:maxDetail] + "…"
	}
	return s
}

// Pod returns the findings for a pod, worst first. A pod that is running and ready, or has finished
// well, has none.
func Pod(p *corev1.Pod, events []corev1.Event) []Finding {
	if p == nil || p.Status.Phase == corev1.PodSucceeded {
		return nil
	}
	var out []Finding

	if p.Status.Phase == corev1.PodFailed {
		out = append(out, failedPod(p)...)
	}
	out = append(out, unschedulable(p)...)
	for _, cs := range p.Status.InitContainerStatuses {
		out = append(out, container(p, cs, events, true)...)
	}
	for _, cs := range p.Status.ContainerStatuses {
		out = append(out, container(p, cs, events, false)...)
	}
	// events that say something the states don't, once each
	out = append(out, fromEvents(p, events, out)...)

	// one finding per (code, container); worst first, in the order they were found otherwise
	seen := map[string]bool{}
	uniq := out[:0]
	for _, f := range out {
		k := f.Code + "/" + f.Container
		if seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, f)
	}
	rank := map[string]int{Error: 0, Warning: 1, Info: 2}
	sort.SliceStable(uniq, func(i, j int) bool { return rank[uniq[i].Severity] < rank[uniq[j].Severity] })
	return uniq
}

func failedPod(p *corev1.Pod) []Finding {
	if p.Status.Reason == "Evicted" {
		return []Finding{{
			Severity: Error, Code: "evicted", Title: "The node evicted this pod",
			Detail: short(p.Status.Message),
			Hints: []string{
				"The node was short of memory or disk. Free space or memory on it, or add a node.",
				"A pod that belongs to a Deployment or StatefulSet is recreated by its controller; this one stays as a record and can be deleted.",
			},
		}}
	}
	if p.Status.Reason == "" && p.Status.Message == "" {
		return nil
	}
	return []Finding{{
		Severity: Error, Code: "failed", Title: "The pod failed",
		Detail: short(strings.TrimSpace(p.Status.Reason + " " + p.Status.Message)),
	}}
}

var (
	insufficientRe = regexp.MustCompile(`Insufficient (cpu|memory|ephemeral-storage|pods)`)
	untoleratedRe  = regexp.MustCompile(`untolerated taint`)
	affinityRe     = regexp.MustCompile(`(?i)didn't match Pod's node (affinity|selector)|node affinity/selector`)
	volumeBindRe   = regexp.MustCompile(`(?i)unbound immediate PersistentVolumeClaims|persistentvolumeclaim "[^"]*" not found|PersistentVolumeClaim .* not found|volume node affinity conflict`)
	tooManyPodsRe  = regexp.MustCompile(`Too many pods`)
)

func unschedulable(p *corev1.Pod) []Finding {
	for _, c := range p.Status.Conditions {
		if c.Type != corev1.PodScheduled || c.Status != corev1.ConditionFalse {
			continue
		}
		msg := c.Message
		switch {
		case insufficientRe.MatchString(msg):
			return []Finding{{
				Severity: Error, Code: "insufficient-resources", Title: "No node has enough free resources for this pod",
				Detail: short(msg),
				Hints: []string{
					"The pod asks for more of a resource (CPU, memory, disk) than any node has left — the message above says which. Lower its requests (the Requests & Limits page shows them) or free capacity.",
					"Or add a node, or scale down something else.",
				},
			}}
		case volumeBindRe.MatchString(msg):
			return []Finding{{
				Severity: Error, Code: "volume-not-bound", Title: "The pod's volume is not ready",
				Detail: short(msg),
				Hints: []string{
					"Its PersistentVolumeClaim is missing or not bound to a volume. Open PVCs & volumes: a claim stuck in Pending usually has no StorageClass, or the class cannot create the volume.",
				},
			}}
		case untoleratedRe.MatchString(msg):
			return []Finding{{
				Severity: Error, Code: "taint", Title: "The nodes are tainted and the pod does not tolerate it",
				Detail: short(msg),
				Hints: []string{
					"Remove the taint from a node (Nodes page), or give the pod a matching toleration.",
				},
			}}
		case affinityRe.MatchString(msg):
			return []Finding{{
				Severity: Error, Code: "node-selector", Title: "No node matches the pod's node selector or affinity",
				Detail: short(msg),
				Hints: []string{
					"Check the pod's nodeSelector and affinity against the nodes' labels (Nodes page), or label a node to match.",
				},
			}}
		case tooManyPodsRe.MatchString(msg):
			return []Finding{{
				Severity: Error, Code: "too-many-pods", Title: "The nodes are full: they cannot run more pods",
				Detail: short(msg),
				Hints:  []string{"Each node has a limit on the number of pods. Remove pods you don't need, or add a node."},
			}}
		}
		return []Finding{{
			Severity: Error, Code: "unschedulable", Title: "The pod cannot be placed on any node",
			Detail: short(msg),
		}}
	}
	return nil
}

var (
	pullPrivateRe     = regexp.MustCompile(`(?i)pull access denied|unauthorized|authentication required|denied: requested access|insufficient_scope`)
	pullNotFoundRe    = regexp.MustCompile(`(?i)not found|manifest unknown|no such manifest|name unknown|repository does not exist`)
	pullRateRe        = regexp.MustCompile(`(?i)toomanyrequests|rate limit|too many requests`)
	pullUnreachableRe = regexp.MustCompile(`(?i)no such host|i/o timeout|connection refused|tls handshake|network is unreachable|context deadline exceeded|dial tcp|temporary failure in name resolution`)
	missingConfigRe   = regexp.MustCompile(`(?i)(secret|configmap) "([^"]+)" not found|couldn't find key ([^ ]+) in (Secret|ConfigMap)`)
)

func container(p *corev1.Pod, cs corev1.ContainerStatus, events []corev1.Event, init bool) []Finding {
	var out []Finding
	name := cs.Name
	where := name
	if init {
		where = "init: " + name
	}
	switch {
	case cs.State.Waiting != nil:
		w := cs.State.Waiting
		switch w.Reason {
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
			out = append(out, imagePull(where, w.Reason, w.Message, cs.Image))
		case "CrashLoopBackOff":
			out = append(out, crash(p, cs, where, init)...)
		case "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
			out = append(out, configError(where, w.Reason, w.Message))
		case "ContainerCreating", "PodInitializing":
			out = append(out, creating(p, where, events)...)
		}
	case cs.State.Terminated != nil && !init:
		t := cs.State.Terminated
		if t.Reason == "OOMKilled" {
			out = append(out, oom(p, cs, where))
		} else if t.ExitCode != 0 {
			out = append(out, exited(cs, where, t.ExitCode, t.Reason))
		}
	case cs.State.Running != nil && !cs.Ready && !init:
		out = append(out, notReady(cs, where, events))
	}
	// a container that keeps being killed even while it runs now
	if cs.State.Running != nil && cs.RestartCount > 0 && cs.LastTerminationState.Terminated != nil {
		lt := cs.LastTerminationState.Terminated
		if lt.Reason == "OOMKilled" {
			out = append(out, oom(p, cs, where))
		} else if lt.ExitCode != 0 && cs.RestartCount >= 2 {
			out = append(out, Finding{
				Severity: Warning, Code: "restarting", Container: name, Title: "The container has been restarting",
				Detail: fmt.Sprintf("%d restarts; the last one ended with exit code %d (%s)", cs.RestartCount, lt.ExitCode, lt.Reason),
				Hints:  []string{"Read its logs: the Logs tab can show the previous run."},
			})
		}
	}
	return out
}

func imagePull(where, reason, msg, image string) Finding {
	f := Finding{Severity: Error, Container: where, Detail: short(strings.TrimSpace(image + " — " + msg))}
	switch {
	case reason == "InvalidImageName":
		f.Code, f.Title = "image-name", "The image name is not valid"
		f.Hints = []string{"Check the image reference for typos: registry/name:tag, lower case, no spaces."}
	case pullRateRe.MatchString(msg):
		f.Code, f.Title = "registry-rate-limit", "The registry is limiting downloads"
		f.Hints = []string{"Docker Hub limits anonymous pulls. Wait and it retries, sign in with an image pull Secret, or use a mirror or another registry."}
	case pullPrivateRe.MatchString(msg):
		f.Code, f.Title = "image-private", "The registry refused access to the image"
		f.Hints = []string{
			"The image is private, or its name is wrong (a wrong name looks the same). Check the name; for a private one add an imagePullSecret with the registry's credentials.",
		}
	case pullNotFoundRe.MatchString(msg):
		f.Code, f.Title = "image-not-found", "The image or its tag does not exist"
		f.Hints = []string{"Check the image name and the tag; pick a tag that exists in the registry."}
	case pullUnreachableRe.MatchString(msg):
		f.Code, f.Title = "registry-unreachable", "The node cannot reach the registry"
		f.Hints = []string{
			"The node could not connect to the registry (DNS, firewall, proxy or no internet). Test from the node: can it resolve and reach the registry's address?",
		}
	default:
		f.Code, f.Title = "image-pull", "The image cannot be downloaded"
		f.Hints = []string{"See the message above and the pod's Events tab; typical causes are a wrong name or tag, a private registry, or no route to the registry."}
	}
	return f
}

func crash(p *corev1.Pod, cs corev1.ContainerStatus, where string, init bool) []Finding {
	lt := cs.LastTerminationState.Terminated
	if lt == nil {
		return []Finding{{
			Severity: Error, Code: "crashloop", Container: cs.Name, Title: "The container keeps crashing",
			Hints: []string{"Read its logs: the Logs tab can show the previous run."},
		}}
	}
	if lt.Reason == "OOMKilled" {
		return []Finding{oom(p, cs, where)}
	}
	return []Finding{exited(cs, where, lt.ExitCode, lt.Reason)}
}

func oom(p *corev1.Pod, cs corev1.ContainerStatus, where string) Finding {
	limit := ""
	for _, c := range p.Spec.Containers {
		if c.Name == cs.Name {
			if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
				limit = " (limit " + q.String() + ")"
			}
		}
	}
	return Finding{
		Severity: Error, Code: "oom", Container: cs.Name, Title: "The container ran out of memory and was killed",
		Detail: "OOMKilled" + limit + short(fmt.Sprintf(", %d restarts", cs.RestartCount)),
		Hints: []string{
			"Raise the container's memory limit (the YAML tab, resources.limits.memory), or make the application use less.",
		},
	}
}

func exited(cs corev1.ContainerStatus, where string, code int32, reason string) Finding {
	f := Finding{
		Severity: Error, Container: cs.Name, Code: "crash", Title: "The container starts and then exits with an error",
		Detail: fmt.Sprintf("exit code %d (%s), %d restarts", code, reason, cs.RestartCount),
		Hints:  []string{"Read its logs first: the Logs tab can show the previous run — the application usually says why."},
	}
	switch code {
	case 126:
		f.Code, f.Title = "command-not-executable", "The container's command cannot be run"
		f.Hints = []string{"The command or entrypoint exists but is not executable (permissions, or the wrong architecture). Check the image and the command."}
	case 127:
		f.Code, f.Title = "command-not-found", "The container's command was not found"
		f.Hints = []string{"The command or entrypoint does not exist in the image. Check the command/args in the YAML tab and the image's tag."}
	case 137:
		f.Hints = append([]string{"Exit code 137 means the container was killed: by the memory limit (look for OOMKilled), or by a failing liveness probe."}, f.Hints...)
	case 139:
		f.Hints = append([]string{"Exit code 139 is a crash of the process itself (a segmentation fault): often the wrong image architecture or a bug in the program."}, f.Hints...)
	case 143:
		f.Hints = append([]string{"Exit code 143 means the container was stopped by a signal (SIGTERM): it was being shut down, or a probe or the node asked it to stop."}, f.Hints...)
	}
	return f
}

func configError(where, reason, msg string) Finding {
	m := missingConfigRe.FindStringSubmatch(msg)
	f := Finding{Severity: Error, Container: where, Code: "config-error", Title: "The container cannot be created: its configuration is wrong", Detail: short(reason + ": " + msg)}
	if m != nil {
		f.Code, f.Title = "config-missing", "The container needs a Secret or ConfigMap that is missing"
		f.Hints = []string{"Create the missing Secret or ConfigMap (or the missing key in it) in this namespace, or fix the name the pod refers to. The pod starts by itself once it exists."}
		return f
	}
	f.Hints = []string{"See the message above: typical causes are a missing Secret/ConfigMap, a wrong key name, or an invalid command."}
	return f
}

// creating explains a pod that sits in ContainerCreating / PodInitializing using its events.
func creating(p *corev1.Pod, where string, events []corev1.Event) []Finding {
	var out []Finding
	var lastPull *corev1.Event
	pulled := false
	for i := range events {
		e := &events[i]
		switch e.Reason {
		case "FailedMount", "FailedAttachVolume":
			out = append(out, Finding{
				Severity: Error, Code: "volume-mount", Title: "A volume cannot be attached or mounted",
				Detail: short(e.Message),
				Hints: []string{
					"The pod waits for a volume: a missing Secret or ConfigMap it mounts, a claim not bound yet, or a storage backend that cannot attach it. See the claims on PVCs & volumes and the Secret/ConfigMap names in the pod's YAML.",
				},
			})
		case "FailedCreatePodSandBox":
			out = append(out, Finding{
				Severity: Error, Code: "sandbox", Title: "The node cannot set up the pod's network or sandbox",
				Detail: short(e.Message),
				Hints:  []string{"Usually the network plugin (CNI) on that node is not ready or out of addresses. Check the node's pods in kube-system; restarting the node's network pod often clears it."},
			})
		case "Pulling":
			if lastPull == nil || eventTime(e).After(eventTime(lastPull)) {
				lastPull = e
			}
		case "Pulled":
			pulled = true
		}
	}
	if lastPull != nil && !pulled && len(out) == 0 {
		out = append(out, Finding{
			Severity: Info, Code: "pulling-image", Title: "The image is being downloaded",
			Detail: short(lastPull.Message),
			Hints:  []string{"A large image or a slow registry takes a while; nothing is wrong yet. If it stays here for many minutes, check the node's connection to the registry."},
		})
	}
	return out
}

func eventTime(e *corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

func notReady(cs corev1.ContainerStatus, where string, events []corev1.Event) Finding {
	f := Finding{
		Severity: Warning, Code: "not-ready", Container: cs.Name, Title: "The container runs but is not ready",
		Hints: []string{"The application may still be starting. If it stays like this, its readiness probe is failing: check the port and path it probes against what the application serves, and the logs."},
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Reason == "Unhealthy" && strings.Contains(strings.ToLower(e.Message), "readiness") {
			f.Code, f.Title = "readiness-probe", "The readiness probe is failing"
			f.Detail = short(e.Message)
			break
		}
	}
	return f
}

// fromEvents adds what only the events say — a failing liveness probe that restarts the container —
// when no finding already covers it.
func fromEvents(p *corev1.Pod, events []corev1.Event, have []Finding) []Finding {
	for _, h := range have {
		if h.Code == "readiness-probe" || h.Code == "oom" {
			return nil
		}
	}
	for _, e := range events {
		if e.Reason == "Unhealthy" && strings.Contains(strings.ToLower(e.Message), "liveness") && e.Count >= 2 {
			return []Finding{{
				Severity: Warning, Code: "liveness-probe", Title: "The liveness probe keeps failing, so the container is restarted",
				Detail: short(e.Message),
				Hints:  []string{"Check the probe's port, path and timing against what the application really does (a slow start needs a startup probe or a longer initial delay)."},
			}}
		}
	}
	return nil
}
