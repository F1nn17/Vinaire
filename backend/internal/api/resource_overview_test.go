package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func podWith(containers ...corev1.Container) *corev1.Pod {
	return &corev1.Pod{Spec: corev1.PodSpec{Containers: containers}}
}

func rl(cpu, mem string) corev1.ResourceList {
	l := corev1.ResourceList{}
	if cpu != "" {
		l[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if mem != "" {
		l[corev1.ResourceMemory] = resource.MustParse(mem)
	}
	return l
}

func TestRequestsAndLimitsAreSummedSeparately(t *testing.T) {
	var row ResourceRow
	row.accumulate(podWith(
		corev1.Container{Resources: corev1.ResourceRequirements{Requests: rl("250m", "128Mi"), Limits: rl("1", "256Mi")}},
		corev1.Container{Resources: corev1.ResourceRequirements{Requests: rl("100m", "")}}, // no memory request, no limits
	))
	row.accumulate(podWith(corev1.Container{})) // nothing set at all

	if row.Pods != 2 || row.Containers != 3 {
		t.Errorf("pods/containers = %d/%d, want 2/3", row.Pods, row.Containers)
	}
	if row.CPU.Requests != 350 || row.CPU.Limits != 1000 {
		t.Errorf("cpu requests/limits = %dm/%dm, want 350m/1000m", row.CPU.Requests, row.CPU.Limits)
	}
	if row.Memory.Requests != 128<<20 || row.Memory.Limits != 256<<20 {
		t.Errorf("memory requests/limits = %d/%d", row.Memory.Requests, row.Memory.Limits)
	}
	// the sums are a lower bound when containers set nothing: say how many didn't
	if row.CPU.ContainersWithoutRequests != 1 || row.CPU.ContainersWithoutLimits != 2 || row.Memory.ContainersWithoutRequests != 2 {
		t.Errorf("unset counts: cpu req=%d lim=%d, mem req=%d", row.CPU.ContainersWithoutRequests, row.CPU.ContainersWithoutLimits, row.Memory.ContainersWithoutRequests)
	}

	var cluster ResourceRow
	cluster.merge(&row)
	cluster.merge(&row)
	if cluster.CPU.Requests != 700 || cluster.Pods != 4 {
		t.Errorf("merge: cpu=%d pods=%d", cluster.CPU.Requests, cluster.Pods)
	}
}

func TestAddPtr(t *testing.T) {
	var p *int64
	p = addPtr(p, 5)
	p = addPtr(p, 7)
	if p == nil || *p != 12 {
		t.Errorf("addPtr = %v", p)
	}
}
