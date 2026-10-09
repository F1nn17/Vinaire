package helm

import "strings"

// Role tells what kind of thing a chart installs, so the Marketplace can separate "one database I can
// use now" from "an operator" and "a cluster an operator runs": the operator's own list (Repo.Roles)
// first, then the names charts conventionally carry. "" means one ordinary instance.
func Role(r Repo, chart string) string {
	if v, ok := r.Roles[chart]; ok {
		return v
	}
	n := strings.ToLower(chart)
	switch {
	case strings.Contains(n, "operator") || strings.HasSuffix(n, "-controller"):
		return "operator"
	case n == "cluster" || strings.HasSuffix(n, "-ha") || strings.HasSuffix(n, "-cluster") ||
		strings.HasSuffix(n, "-db") || strings.Contains(n, "sharded"):
		return "cluster"
	}
	return ""
}

func roleMatches(want, got string) bool {
	switch want {
	case "":
		return true
	case "single":
		return got == ""
	default:
		return got == want
	}
}
