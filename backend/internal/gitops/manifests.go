package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

const maxObjects = 500

// ParseManifests reads every YAML/JSON document of the snapshot's files into
// objects, in a stable order: file name order, then document order. A document
// that isn't a Kubernetes object, or one that appears twice, is an error — a
// repository that says two things about one object has no single desired state.
// `kind: List` is expanded. Empty documents (a trailing "---") are ignored.
func ParseManifests(files map[string][]byte) ([]*unstructured.Unstructured, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	var out []*unstructured.Unstructured
	seen := map[string]string{}
	add := func(file string, u *unstructured.Unstructured) error {
		if u.GetAPIVersion() == "" || u.GetKind() == "" {
			return fmt.Errorf("%s: a document has no apiVersion/kind — is it a Kubernetes manifest?", file)
		}
		if u.GetName() == "" {
			if u.GetGenerateName() != "" {
				return fmt.Errorf("%s: %s uses generateName, which can't be synced (it has no stable name)", file, u.GetKind())
			}
			return fmt.Errorf("%s: a %s has no metadata.name", file, u.GetKind())
		}
		key := objectKey(u)
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("%s defines %s that %s already defines", file, key, prev)
		}
		seen[key] = file
		if len(out) >= maxObjects {
			return fmt.Errorf("more than %d objects", maxObjects)
		}
		out = append(out, u)
		return nil
	}

	for _, file := range names {
		dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(files[file]), 4096)
		for {
			var raw map[string]any
			if err := dec.Decode(&raw); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if len(raw) == 0 {
				continue
			}
			u := &unstructured.Unstructured{Object: raw}
			if strings.HasSuffix(u.GetKind(), "List") && raw["items"] != nil {
				items, ok := raw["items"].([]any)
				if !ok {
					return nil, fmt.Errorf("%s: a List has malformed items", file)
				}
				for _, it := range items {
					m, ok := it.(map[string]any)
					if !ok {
						return nil, fmt.Errorf("%s: a List item isn't an object", file)
					}
					if err := add(file, &unstructured.Unstructured{Object: m}); err != nil {
						return nil, err
					}
				}
				continue
			}
			if err := add(file, u); err != nil {
				return nil, err
			}
		}
	}
	sortForApply(out)
	return out, nil
}

func objectKey(u *unstructured.Unstructured) string {
	return fmt.Sprintf("%s %s %s/%s", u.GetAPIVersion(), u.GetKind(), u.GetNamespace(), u.GetName())
}

// applyOrder puts what other objects live in or depend on first: namespaces,
// then CRDs (so custom resources can follow), then everything else, in file
// order. It is the whole of "sync waves" this feature has.
func applyOrder(u *unstructured.Unstructured) int {
	switch u.GetKind() {
	case "Namespace":
		return 0
	case "CustomResourceDefinition":
		return 1
	}
	return 2
}

func sortForApply(objs []*unstructured.Unstructured) {
	sort.SliceStable(objs, func(i, j int) bool { return applyOrder(objs[i]) < applyOrder(objs[j]) })
}

func refOf(u *unstructured.Unstructured) ObjectRef {
	return ObjectRef{APIVersion: u.GetAPIVersion(), Kind: u.GetKind(), Namespace: u.GetNamespace(), Name: u.GetName()}
}
