package diagnose

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func codes(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return strings.Join(out, ",")
}

func waiting(reason, message string) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: "app", Image: "reg/app:1", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}}}
}

func podWith(cs ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: cs},
	}
}

func TestAHealthyOrFinishedPodHasNoFindings(t *testing.T) {
	ready := corev1.ContainerStatus{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
	if got := Pod(podWith(ready), nil); len(got) != 0 {
		t.Errorf("a ready pod: %+v", got)
	}
	done := podWith()
	done.Status.Phase = corev1.PodSucceeded
	if got := Pod(done, nil); len(got) != 0 {
		t.Errorf("a finished pod: %+v", got)
	}
	if got := Pod(nil, nil); got != nil {
		t.Errorf("no pod: %+v", got)
	}
}

func TestUnschedulablePodsSayWhy(t *testing.T) {
	for msg, want := range map[string]string{
		"0/1 nodes are available: 1 Insufficient memory. preemption: 0/1 nodes are available: 1 No preemption victims found": "insufficient-resources",
		"0/3 nodes are available: 3 Insufficient cpu.":                                                   "insufficient-resources",
		"0/1 nodes are available: 1 node(s) had untolerated taint {node.kubernetes.io/disk-pressure: }.": "taint",
		"0/2 nodes are available: 2 node(s) didn't match Pod's node affinity/selector.":                  "node-selector",
		"0/1 nodes are available: pod has unbound immediate PersistentVolumeClaims.":                     "volume-not-bound",
		`0/1 nodes are available: persistentvolumeclaim "data" not found.`:                               "volume-not-bound",
		"0/1 nodes are available: 1 Too many pods.":                                                      "too-many-pods",
		"something nobody has written a rule for":                                                        "unschedulable",
	} {
		p := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: msg}}}}
		got := Pod(p, nil)
		if len(got) != 1 || got[0].Code != want || got[0].Severity != Error {
			t.Errorf("%q: %s, want %s", msg, codes(got), want)
		}
		if want != "unschedulable" && len(got[0].Hints) == 0 {
			t.Errorf("%s: no hint what to do", want)
		}
		if got[0].Detail == "" {
			t.Errorf("%s: the cluster's own words are the detail", want)
		}
	}
}

func TestImagePullFailuresAreTold(t *testing.T) {
	for msg, want := range map[string]string{
		`rpc error: code = Unknown desc = failed to pull and unpack image "x": failed to resolve reference: pull access denied, repository does not exist or may require authorization`: "image-private",
		`Back-off pulling image "x": manifest unknown`:                                                                        "image-not-found",
		`failed to pull image: toomanyrequests: You have reached your pull rate limit`:                                        "registry-rate-limit",
		`failed to do request: Head "https://registry-1.docker.io/v2/x": dial tcp: lookup registry-1.docker.io: no such host`: "registry-unreachable",
		`some new kind of failure`: "image-pull",
	} {
		got := Pod(podWith(waiting("ImagePullBackOff", msg)), nil)
		if len(got) != 1 || got[0].Code != want {
			t.Errorf("%q: %s, want %s", msg, codes(got), want)
		}
	}
	if got := Pod(podWith(waiting("InvalidImageName", "couldn't parse image name")), nil); codes(got) != "image-name" {
		t.Errorf("invalid name: %s", codes(got))
	}
}

func TestCrashesNameTheCause(t *testing.T) {
	crash := func(exit int32, reason string) corev1.ContainerStatus {
		cs := waiting("CrashLoopBackOff", "back-off 5m0s restarting failed container")
		cs.RestartCount = 7
		cs.LastTerminationState = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exit, Reason: reason}}
		return cs
	}
	got := Pod(podWith(crash(137, "OOMKilled")), nil)
	if codes(got) != "oom" || !strings.Contains(got[0].Detail, "limit 128Mi") {
		t.Errorf("OOM names the limit: %+v", got)
	}
	for exit, want := range map[int32]string{1: "crash", 2: "crash", 126: "command-not-executable", 127: "command-not-found", 137: "crash", 139: "crash", 143: "crash"} {
		if got := Pod(podWith(crash(exit, "Error")), nil); codes(got) != want {
			t.Errorf("exit %d: %s, want %s", exit, codes(got), want)
		}
	}
	if got := Pod(podWith(crash(137, "Error")), nil); !strings.Contains(strings.Join(got[0].Hints, " "), "137") {
		t.Errorf("exit 137 is explained: %+v", got)
	}
	// no record of why it died yet
	if got := Pod(podWith(waiting("CrashLoopBackOff", "")), nil); codes(got) != "crashloop" {
		t.Errorf("crashloop without a last state: %s", codes(got))
	}
}

func TestMissingConfigurationIsNamed(t *testing.T) {
	got := Pod(podWith(waiting("CreateContainerConfigError", `secret "db-auth" not found`)), nil)
	if codes(got) != "config-missing" {
		t.Errorf("a missing Secret: %s", codes(got))
	}
	got = Pod(podWith(waiting("CreateContainerConfigError", `couldn't find key password in Secret db/db-auth`)), nil)
	if codes(got) != "config-missing" {
		t.Errorf("a missing key: %s", codes(got))
	}
	got = Pod(podWith(waiting("CreateContainerError", "something else")), nil)
	if codes(got) != "config-error" {
		t.Errorf("another config error: %s", codes(got))
	}
}

func ev(reason, msg string, ago time.Duration) corev1.Event {
	return corev1.Event{Reason: reason, Message: msg, LastTimestamp: metav1.NewTime(time.Now().Add(-ago))}
}

func TestContainerCreatingIsExplainedByItsEvents(t *testing.T) {
	creating := podWith(waiting("ContainerCreating", ""))
	if got := Pod(creating, []corev1.Event{ev("Pulling", `Pulling image "big:1"`, time.Minute)}); codes(got) != "pulling-image" || got[0].Severity != Info {
		t.Errorf("an image still downloading is information, not an error: %+v", got)
	}
	if got := Pod(creating, []corev1.Event{ev("Pulling", "p", 2*time.Minute), ev("Pulled", "done", time.Minute)}); len(got) != 0 {
		t.Errorf("once pulled, nothing to say: %+v", got)
	}
	if got := Pod(creating, []corev1.Event{ev("FailedMount", `MountVolume.SetUp failed for volume "cfg": configmap "x" not found`, time.Minute)}); codes(got) != "volume-mount" {
		t.Errorf("a volume that cannot be mounted: %s", codes(got))
	}
	if got := Pod(creating, []corev1.Event{ev("FailedCreatePodSandBox", "failed to setup network", time.Minute)}); codes(got) != "sandbox" {
		t.Errorf("the network of the node: %s", codes(got))
	}
	if got := Pod(creating, nil); len(got) != 0 {
		t.Errorf("no events, no guess: %+v", got)
	}
}

func TestRunningButNotReady(t *testing.T) {
	notReady := corev1.ContainerStatus{Name: "app", Ready: false, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
	if got := Pod(podWith(notReady), nil); codes(got) != "not-ready" || got[0].Severity != Warning {
		t.Errorf("starting up: %+v", got)
	}
	got := Pod(podWith(notReady), []corev1.Event{ev("Unhealthy", "Readiness probe failed: HTTP probe failed with statuscode: 503", time.Minute)})
	if codes(got) != "readiness-probe" || !strings.Contains(got[0].Detail, "503") {
		t.Errorf("a failing readiness probe: %+v", got)
	}
}

func TestEvictedAndOrderingAndDedup(t *testing.T) {
	ev := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", Message: "The node was low on resource: memory."}}
	if got := Pod(ev, nil); codes(got) != "evicted" {
		t.Errorf("evicted: %+v", got)
	}
	// two containers failing the same way are two findings; the same container twice is one
	a := waiting("ImagePullBackOff", "manifest unknown")
	b := waiting("ImagePullBackOff", "manifest unknown")
	b.Name = "sidecar"
	p := podWith(a, b)
	p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "manifest unknown"}}}}
	if got := Pod(p, nil); len(got) != 3 {
		t.Errorf("init, app and sidecar: %+v", got)
	}
	// errors before warnings before information
	notReady := corev1.ContainerStatus{Name: "web", Ready: false, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
	mixed := podWith(notReady, waiting("ImagePullBackOff", "manifest unknown"))
	if got := Pod(mixed, nil); len(got) != 2 || got[0].Severity != Error || got[1].Severity != Warning {
		t.Errorf("worst first: %+v", got)
	}
}

// What the diagnosis must never do with the cluster's text: panic, or return more than a bounded
// detail.
func FuzzPodDiagnosis(f *testing.F) {
	f.Add("Insufficient memory", "manifest unknown", "secret \"x\" not found")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, sched, pull, cfg string) {
		p := podWith(waiting("ImagePullBackOff", pull), waiting("CreateContainerConfigError", cfg))
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Message: sched}}
		for _, fd := range Pod(p, []corev1.Event{ev("FailedMount", sched, 0), ev("Unhealthy", cfg, 0)}) {
			if len(fd.Detail) > maxDetail+8 {
				t.Fatalf("detail not bounded: %d", len(fd.Detail))
			}
			if fd.Title == "" || fd.Code == "" {
				t.Fatalf("a finding without a title or code: %+v", fd)
			}
		}
	})
}
