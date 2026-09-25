package api

import corev1 "k8s.io/api/core/v1"

// StatusColor mirrors the frontend's StatusColor union type
// (frontend/src/lib/types.ts: "ok" | "warn" | "err").
type StatusColor string

const (
	StatusOK   StatusColor = "ok"
	StatusWarn StatusColor = "warn"
	StatusErr  StatusColor = "err"
)

// readyStatus derives a generic Healthy/Degraded status for anything shaped
// like "N of M replicas ready" (Deployments, StatefulSets, DaemonSets).
func readyStatus(ready, desired int32) (status string, color StatusColor) {
	switch {
	case desired == 0:
		// Nothing desired (e.g. scaled to zero) — not a failure state.
		return "Healthy", StatusOK
	case ready == desired:
		return "Healthy", StatusOK
	case ready == 0:
		return "Degraded", StatusErr
	default:
		return "Degraded", StatusWarn
	}
}

// podReadyCount returns how many of a pod's containers are ready, and how
// many containers it has in total.
func podReadyCount(pod corev1.Pod) (ready, total int32) {
	total = int32(len(pod.Status.ContainerStatuses))
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return ready, total
}

// podRestarts sums restart counts across all of a pod's containers.
func podRestarts(pod corev1.Pod) int32 {
	var total int32
	for _, cs := range pod.Status.ContainerStatuses {
		total += cs.RestartCount
	}
	return total
}

// podStatus mirrors (a simplified version of) `kubectl get pods`' STATUS
// column: prefer a specific waiting/terminated reason over the coarse pod
// phase, since "CrashLoopBackOff" is a lot more useful than "Running".
func podStatus(pod corev1.Pod) (status string, color StatusColor) {
	if pod.DeletionTimestamp != nil {
		return "Terminating", StatusWarn
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason, waitingColor(cs.State.Waiting.Reason)
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason == "Error" {
			return "Error", StatusErr
		}
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		return "Running", StatusOK
	case corev1.PodSucceeded:
		return "Succeeded", StatusOK
	case corev1.PodPending:
		return "Pending", StatusWarn
	case corev1.PodFailed:
		return "Failed", StatusErr
	default:
		return string(pod.Status.Phase), StatusWarn
	}
}

// waitingColor classifies a container's "waiting" reason. Reasons genuinely
// on the way up (pulling an image, running init containers) are a warning;
// anything else waiting (CrashLoopBackOff, ImagePullBackOff, ...) is an
// error.
func waitingColor(reason string) StatusColor {
	switch reason {
	case "ContainerCreating", "PodInitializing":
		return StatusWarn
	default:
		return StatusErr
	}
}
