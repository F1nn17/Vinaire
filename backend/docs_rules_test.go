package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// Documentation is checked like code (AGENTS.md: "Documentation in every phase"). These rules catch the drift a reader
// would hit first: a link that goes nowhere, a chart value that was renamed, an environment variable that no longer
// exists. They can't tell whether a sentence is still true — that is what reading the docs of a phase is for.

func docFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"../README.md", "../README.ru.md", "../keycloak/README.md", "../AGENTS.md"}
	more, err := filepath.Glob("../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	return append(files, more...)
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// codeSpans returns the text of every `inline code` span, ignoring fenced blocks.
func codeSpans(doc string) []string {
	var out []string
	inFence := false
	span := regexp.MustCompile("`([^`\n]+)`")
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range span.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

func TestDocsLinksResolve(t *testing.T) {
	link := regexp.MustCompile(`\]\(([^)\s]+)\)`)
	for _, f := range docFiles(t) {
		inFence := false
		for i, line := range strings.Split(readDoc(t, f), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
			}
			if inFence {
				continue
			}
			for _, m := range link.FindAllStringSubmatch(line, -1) {
				target := m[1]
				if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				if i := strings.IndexAny(target, "#?"); i >= 0 {
					target = target[:i]
				}
				if target == "" {
					continue
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(f), filepath.FromSlash(target))); err != nil {
					t.Errorf("%s:%d: the link %q goes nowhere", filepath.Base(f), i+1, m[1])
				}
			}
		}
	}
}

// chartValueKeys flattens the chart's values.yaml into dotted paths.
func chartValueKeys(t *testing.T) (all map[string]bool, top map[string]bool) {
	t.Helper()
	var root map[string]any
	if err := yaml.Unmarshal([]byte(readDoc(t, "../charts/vinaire/values.yaml")), &root); err != nil {
		t.Fatal(err)
	}
	all, top = map[string]bool{}, map[string]bool{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		if prefix != "" {
			all[prefix] = true
		}
		if m, ok := v.(map[string]any); ok {
			for k, c := range m {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, c)
			}
		}
	}
	walk("", root)
	for k := range root {
		top[k] = true
	}
	return all, top
}

// Documentation names chart values in code spans (`keycloak.allowHttpPrivateNetwork`); each must exist in values.yaml.
func TestDocsNameOnlyChartValuesThatExist(t *testing.T) {
	all, top := chartValueKeys(t)
	// a chart value is lowerCamelCase all the way; Go identifiers (keycloak.Secret), file names (keycloak.go) and the
	// virtual resources (audit.vinaire.io) are something else
	ident := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(\.[a-z][a-zA-Z0-9]*)+$`)
	notValue := map[string]bool{"go": true, "io": true, "v2": true, "yaml": true, "yml": true, "json": true, "md": true, "tpl": true, "sh": true, "env": true}
	for _, f := range docFiles(t) {
		for _, span := range codeSpans(readDoc(t, f)) {
			name := strings.TrimSuffix(strings.SplitN(span, "=", 2)[0], ".")
			name = strings.TrimSpace(name)
			if !ident.MatchString(name) {
				continue
			}
			if notValue[name[strings.LastIndex(name, ".")+1:]] {
				continue
			}
			first := strings.SplitN(name, ".", 2)[0]
			if !top[first] {
				continue // not about the chart (a file name, a Kubernetes field…)
			}
			// a user-provided map (extraRules, targets…) has keys of its own: check up to the first one that exists as a value
			if !all[name] && !hasAncestorThatIsFree(name, all) {
				t.Errorf("%s names the chart value %q, which values.yaml doesn't have", filepath.Base(f), name)
			}
		}
	}
}

// hasAncestorThatIsFree: `backup.targets.x` is fine when `backup.targets` is a list or an empty map the user fills.
func hasAncestorThatIsFree(name string, all map[string]bool) bool {
	parts := strings.Split(name, ".")
	for i := len(parts) - 1; i >= 1; i-- {
		if all[strings.Join(parts[:i], ".")] && !hasChildren(strings.Join(parts[:i], "."), all) {
			return true
		}
	}
	return false
}

func hasChildren(prefix string, all map[string]bool) bool {
	for k := range all {
		if strings.HasPrefix(k, prefix+".") {
			return true
		}
	}
	return false
}

// An environment variable named in the docs must still be read somewhere (Go code and its tests, the chart, a workflow).
func TestDocsNameOnlyEnvironmentVariablesThatExist(t *testing.T) {
	var corpus strings.Builder
	walk := func(root string, ok func(string) bool) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".next" || d.Name() == "out" || d.Name() == ".git") {
				return filepath.SkipDir
			}
			if !d.IsDir() && ok(p) {
				if b, err := os.ReadFile(p); err == nil {
					corpus.Write(b)
				}
			}
			return nil
		})
	}
	walk("../backend", func(p string) bool { return strings.HasSuffix(p, ".go") })
	walk("../charts", func(p string) bool { return true })
	walk("../.github", func(p string) bool { return true })
	walk("../frontend/src", func(p string) bool { return strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") })
	walk("../scripts", func(p string) bool { return true })
	have := corpus.String()

	env := regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)
	// variables of other programs, or placeholders, that the docs mention on purpose
	foreign := map[string]bool{
		"KUBE_CONFIG": true, "E2E_TOKEN": true, "E2E_BASE_URL": true, "KC_ADMIN_PASSWORD": true,
		"NEXT_PUBLIC_API_URL": true,
	}
	var missing []string
	seen := map[string]bool{}
	for _, f := range docFiles(t) {
		for _, span := range codeSpans(readDoc(t, f)) {
			name := strings.SplitN(span, "=", 2)[0]
			if !env.MatchString(name) || foreign[name] || seen[name] {
				continue
			}
			seen[name] = true
			if !strings.Contains(have, name) {
				missing = append(missing, name+" ("+filepath.Base(f)+")")
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("the docs name the variable %s, which nothing reads any more", m)
	}
}
