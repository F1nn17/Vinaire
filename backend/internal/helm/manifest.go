package helm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

// What the install would apply, as YAML, before anything is created (Roadmap Phase 59): the same
// client-side render the access check already makes. Nothing is written. A Secret's values are
// replaced (a chart may render passwords it was given, or generated ones) and the whole is capped,
// so the preview is a way to read the install, not a way to leak or to flood.

// maxManifestBytes bounds the preview.
const maxManifestBytes = 512 << 10

const redacted = "<redacted>"

// RenderManifest returns the rendered objects as one YAML stream (Secret data redacted) and
// whether it was cut at the cap.
func (rl *Releases) RenderManifest(ctx context.Context, cfg *rest.Config, req InstallRequest) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	objs, err := renderObjects(ctx, cfg, req, rl)
	if err != nil {
		return "", false, err
	}
	return manifestOf(objs)
}

func manifestOf(objs []*unstructured.Unstructured) (string, bool, error) {
	var b strings.Builder
	for i, o := range objs {
		out := o.DeepCopy()
		if out.GetKind() == "Secret" {
			redactSecret(out)
		}
		y, err := yaml.Marshal(out.Object)
		if err != nil {
			return "", false, fmt.Errorf("writing %s/%s: %w", out.GetKind(), out.GetName(), err)
		}
		if b.Len()+len(y) > maxManifestBytes {
			return b.String(), true, nil
		}
		if i > 0 {
			b.WriteString("---\n")
		}
		b.Write(y)
	}
	return b.String(), false, nil
}

func redactSecret(o *unstructured.Unstructured) {
	for _, field := range []string{"data", "stringData"} {
		m, found, _ := unstructured.NestedMap(o.Object, field)
		if !found {
			continue
		}
		for k := range m {
			m[k] = redacted
		}
		_ = unstructured.SetNestedMap(o.Object, m, field)
	}
}
