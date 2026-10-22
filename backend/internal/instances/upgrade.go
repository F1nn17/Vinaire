package instances

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
)

// Version upgrade of an instance installed from an image (Roadmap Phase 77d). This file only plans:
// it validates the request and builds the patch; internal/k8s applies it, as the caller.
//
// Only the *tag* changes. The repository is taken from the image the instance runs now — a request
// never names an image — so an upgrade cannot point an instance at another publisher. A change of the
// leading ("major") version needs an explicit acknowledgement, because a database cannot read the data
// directory of another major version; a lower major version is refused outright (revert instead).

// AnnotationPreviousImage remembers the image before the last upgrade, so it can be put back.
const AnnotationPreviousImage = "vinaire.io/previous-image"

var (
	tagRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	imageRe = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]{0,127}:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	verRe   = regexp.MustCompile(`^v?([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?`)
)

// UpgradePlan is what an upgrade does.
type UpgradePlan struct {
	From, To string // full image references
	Major    bool   // the leading version changes (or cannot be compared): acknowledgement required
	Patch    []byte // strategic merge patch for the StatefulSet
}

func split(image string) (repo, tag string, err error) {
	if strings.Contains(image, "@") {
		return "", "", fmt.Errorf("the instance is pinned by digest; change it in the YAML")
	}
	i := strings.LastIndex(image, ":")
	if i < 0 || i < strings.LastIndex(image, "/") || !imageRe.MatchString(image) {
		return "", "", fmt.Errorf("the instance image has an unexpected form")
	}
	return image[:i], image[i+1:], nil
}

func version(tag string) (v [3]int, ok bool) {
	m := verRe.FindStringSubmatch(tag)
	if m == nil {
		return v, false
	}
	for i := 0; i < 3; i++ {
		if m[i+1] != "" {
			n, err := strconv.Atoi(m[i+1])
			if err != nil {
				return v, false
			}
			v[i] = n
		}
	}
	return v, true
}

func appContainerImage(sts appsv1.StatefulSet) (string, error) {
	if sts.Labels[labelManagedBy] != "vinaire" {
		return "", fmt.Errorf("only instances installed from an image are upgraded here; upgrade a chart from the Marketplace")
	}
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name == "app" {
			return c.Image, nil
		}
	}
	return "", fmt.Errorf("the instance has no container named app")
}

func patchFor(previous, image string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{AnnotationPreviousImage: previous}},
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []map[string]string{{"name": "app", "image": image}},
		}}},
	})
}

// PlanUpgrade builds the upgrade of sts to `tag`.
func PlanUpgrade(sts appsv1.StatefulSet, tag string, acceptMajor bool) (UpgradePlan, error) {
	cur, err := appContainerImage(sts)
	if err != nil {
		return UpgradePlan{}, err
	}
	repo, curTag, err := split(cur)
	if err != nil {
		return UpgradePlan{}, err
	}
	if !tagRe.MatchString(tag) {
		return UpgradePlan{}, fmt.Errorf("invalid image tag")
	}
	if tag == curTag {
		return UpgradePlan{}, fmt.Errorf("the instance already runs %s", tag)
	}
	plan := UpgradePlan{From: cur, To: repo + ":" + tag}
	a, aok := version(curTag)
	b, bok := version(tag)
	switch {
	case !aok || !bok:
		plan.Major = true // cannot tell what changes (latest, alpine…)
	case b[0] < a[0]:
		return UpgradePlan{}, fmt.Errorf("a lower major version cannot read the data of a higher one; revert to the previous image instead")
	case b[0] != a[0]:
		plan.Major = true
	}
	if plan.Major && !acceptMajor {
		return UpgradePlan{}, fmt.Errorf("this changes the major version (or it cannot be compared): the data may not be readable by the new version. Take a backup and confirm that you accept this")
	}
	if plan.Patch, err = patchFor(cur, plan.To); err != nil {
		return UpgradePlan{}, err
	}
	return plan, nil
}

// PlanRevert builds the return to the image recorded before the last upgrade.
func PlanRevert(sts appsv1.StatefulSet) (UpgradePlan, error) {
	cur, err := appContainerImage(sts)
	if err != nil {
		return UpgradePlan{}, err
	}
	prev := sts.Annotations[AnnotationPreviousImage]
	if prev == "" {
		return UpgradePlan{}, fmt.Errorf("there is no previous image recorded for this instance")
	}
	pr, _, err := split(prev)
	if err != nil {
		return UpgradePlan{}, err
	}
	cr, _, err := split(cur)
	if err != nil || pr != cr {
		return UpgradePlan{}, fmt.Errorf("the recorded image is of another repository; refusing")
	}
	patch, err := patchFor(cur, prev) // the "previous" of a revert is the image it leaves, so it can be undone
	if err != nil {
		return UpgradePlan{}, err
	}
	return UpgradePlan{From: cur, To: prev, Patch: patch}, nil
}
