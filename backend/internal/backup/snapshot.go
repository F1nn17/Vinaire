package backup

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Snapshot resources (the CSI external-snapshotter CRDs). A cluster without them, or without a
// VolumeSnapshotClass, cannot take snapshots: the API reports "unavailable", not an error.
var (
	SnapshotGVR      = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
	SnapshotClassGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
	SnapshotCRD      = "volumesnapshots.snapshot.storage.k8s.io"
)

// Snapshot builds a VolumeSnapshot of one claim. class may be empty (the cluster's default class applies).
func Snapshot(namespace, instance, claim, class string, now time.Time) (*unstructured.Unstructured, error) {
	if !nameRe.MatchString(claim) || len(claim) > 40 {
		return nil, fmt.Errorf("invalid claim name %q", claim)
	}
	if class != "" && !nameRe.MatchString(class) {
		return nil, fmt.Errorf("invalid snapshot class")
	}
	spec := map[string]any{"source": map[string]any{"persistentVolumeClaimName": claim}}
	if class != "" {
		spec["volumeSnapshotClassName"] = class
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "snapshot.storage.k8s.io/v1",
		"kind":       "VolumeSnapshot",
		"metadata": map[string]any{
			"name":      claim + "-snap-" + now.UTC().Format("20060102-150405"),
			"namespace": namespace,
			"labels": map[string]any{
				LabelManagedBy: "vinaire", LabelBackupOf: instance, LabelBackupKind: "snapshot",
			},
		},
		"spec": spec,
	}}, nil
}
