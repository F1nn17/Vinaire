package instances

import (
	"encoding/json"
	"strings"
	"testing"
)

// Roadmap Phase 78: the tag of a version change comes from a request body. Whatever it holds, a plan that
// is accepted may only change the tag of the image the instance already runs.

func FuzzPlanUpgradeTag(f *testing.F) {
	for _, s := range []string{
		"", "17.4", "18", "latest", "17.4-alpine", "evil/postgres:17", "17@sha256:abc", "17\n", "17 ; rm -rf /",
		"../../x", "$(id)", "`id`", "17.4'", "\"", "-17", strings.Repeat("9", 300), "０", "v17", "17.2",
	} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, tag string, accept bool) {
		sts := setWith("postgres:17.2", "vinaire", nil)
		p, err := PlanUpgrade(sts, tag, accept)
		if err != nil {
			return
		}
		if p.To != "postgres:"+tag || !tagRe.MatchString(tag) {
			t.Fatalf("accepted tag %q produced %q", tag, p.To)
		}
		if strings.ContainsAny(tag, "/@ \t\r\n;$`'\"\\|&<>()*?") {
			t.Fatalf("accepted a tag with a dangerous character: %q", tag)
		}
		if !json.Valid(p.Patch) {
			t.Fatalf("patch is not JSON: %s", p.Patch)
		}
		if p.Major && !accept {
			t.Fatalf("a major change was accepted without acknowledgement: %q", tag)
		}
	})
}

// The image recorded before an upgrade is read back from an annotation, which anyone with update rights on
// the StatefulSet can write: a revert must never leave the instance's repository.
func FuzzPlanRevertAnnotation(f *testing.F) {
	for _, s := range []string{"", "postgres:17.2", "evil/postgres:17.2", "postgres@sha256:abc", "postgres:17.2\n", "quay.io/x/y:1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, prev string) {
		sts := setWith("postgres:17.4", "vinaire", map[string]string{AnnotationPreviousImage: prev})
		p, err := PlanRevert(sts)
		if err != nil {
			return
		}
		if !strings.HasPrefix(p.To, "postgres:") || strings.ContainsAny(p.To, "@ \t\r\n;$`'\"\\") {
			t.Fatalf("revert to %q accepted", p.To)
		}
	})
}
