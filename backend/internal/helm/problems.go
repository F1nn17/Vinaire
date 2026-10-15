package helm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Roadmap Phase 71: a chart that refuses to render because a value is missing — `required`,
// `fail` — used to surface only as Helm's raw sentence at install time ("config.datasource.password
// is required when not using …"). The preflight now reads that sentence, finds which of the chart's
// own values it talks about (only ones that exist in its defaults), and hands the UI a structured
// description so the install page can ask for exactly those values before anything is installed.
//
// The text it reads comes from the chart's author, so it is treated as untrusted: it is only ever
// searched for dotted names, and a name counts only if it resolves in the chart's own default values.

// ValueNeed is one value of the chart that a render problem points at.
type ValueNeed struct {
	// Path is the dotted path in the values ("config.datasource.passwordSecret").
	Path string `json:"path"`
	// Kind is what the chart's default says it is: string, number, bool, object or list.
	Kind string `json:"kind"`
	// Sensitive: a plain value that is probably a password or token.
	Sensitive bool `json:"sensitive,omitempty"`
	// SecretRef: the value names a Secret (or holds a Secret reference) rather than the secret itself.
	SecretRef bool `json:"secretRef,omitempty"`
	// Children are the scalar keys of an object value ("name", "key" of a secret reference).
	Children []string `json:"children,omitempty"`
	// Guessed: the chart's default is an empty object, so Children is the usual shape of a
	// Kubernetes secret reference, not something the chart declares.
	Guessed bool `json:"guessed,omitempty"`
}

// RenderProblem is a chart that does not render with the values it was given.
type RenderProblem struct {
	// Message is the chart's own words.
	Message string `json:"message"`
	// Template is where in the chart it happened ("prometheus-postgres-exporter/templates/secrets.yaml:2:9").
	Template string `json:"template,omitempty"`
	// Required: the chart itself asked for a value (`required` / `fail`). Unlike other render
	// errors this depends only on the values, so the install cannot succeed until it is answered.
	// A chart that stops because something "was not found" in the cluster is not counted: the
	// client-side render has no cluster, so its `lookup` always finds nothing, and the real install
	// might well pass.
	Required bool `json:"required"`
	// Needs are the chart's values the message names. When there are several they are usually
	// alternatives ("a password, or a Secret, or a file").
	Needs []ValueNeed `json:"needs,omitempty"`

	cause error
}

// Error keeps the sentence the UI and older callers have always seen.
func (p *RenderProblem) Error() string {
	return fmt.Sprintf("%v: %v: the chart doesn't render with these values: %v", ErrInvalid, ErrRender, p.cause)
}

// Unwrap lets errors.Is find ErrInvalid and ErrRender, as before.
func (p *RenderProblem) Unwrap() []error { return []error{ErrInvalid, ErrRender, p.cause} }

var (
	execErrRe   = regexp.MustCompile(`(?s)execution error at \(([^)]*)\): (.+)$`)
	valuePathRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_-]*(?:\.[A-Za-z_][A-Za-z0-9_-]*)+`)
	secretRefRe = regexp.MustCompile(`(?i)(secret|secretname)$`)
	sensitiveRe = regexp.MustCompile(`(?i)(password|passwd|token|apikey|api_key|credential|secret)`)
	fileRe      = regexp.MustCompile(`(?i)(file|path)$`)
	// a chart's `fail` after a `lookup` of something in the cluster
	notFoundRe = regexp.MustCompile(`(?i)(not found|does not exist|doesn't exist|cannot be found|no such)`)
	// a line of a YAML example the chart prints ("cluster:" / "  name: my-cluster")
	snippetRe = regexp.MustCompile(`^(\s*)([A-Za-z_][A-Za-z0-9_-]*):(?:\s+(\S.*))?$`)
)

const maxNeeds = 8

// classifyRenderError turns Helm's error into a RenderProblem; defaults are the chart's own values.
func classifyRenderError(err error, defaults map[string]any) *RenderProblem {
	p := &RenderProblem{cause: err}
	text := err.Error()
	m := execErrRe.FindStringSubmatch(text)
	if m == nil {
		p.Message = text
		return p
	}
	p.Template = strings.TrimSpace(m[1])
	p.Message = strings.TrimSpace(m[2])
	p.Required = !notFoundRe.MatchString(p.Message)

	candidates := valuePathRe.FindAllString(p.Message, -1)
	for i, c := range candidates {
		candidates[i] = strings.TrimPrefix(c, "Values.")
	}
	candidates = append(candidates, snippetPaths(p.Message)...)

	seen := map[string]bool{}
	for _, path := range candidates {
		if seen[path] {
			continue
		}
		v, ok := resolveNeed(defaults, strings.Split(path, "."))
		if !ok {
			continue
		}
		seen[path] = true
		p.Needs = append(p.Needs, describeNeed(path, v))
		if len(p.Needs) == maxNeeds {
			break
		}
	}
	return p
}

// resolveNeed accepts a path the chart's defaults know — or a new key inside a map they know, which is
// how a chart asks for something like loki.storage.bucketNames.chunks that it leaves out of its
// defaults. It never accepts a name whose parent isn't there: a file name or a made-up word in a
// sentence has no such parent.
func resolveNeed(defaults map[string]any, path []string) (any, bool) {
	if v, ok := lookupPath(defaults, path); ok {
		return v, true
	}
	if len(path) >= 2 {
		if parent, ok := lookupPath(defaults, path[:len(path)-1]); ok {
			if _, isMap := parent.(map[string]any); isMap {
				return nil, true
			}
		}
	}
	return nil, false
}

// snippetPaths reads the dotted paths of a YAML example in the message:
//
//	Please set:
//	cluster:
//	  name: my-cluster-name
//
// gives "cluster.name". Lines that aren't "key:" or "key: value" are skipped; whether a path means
// anything is decided by resolveNeed against the chart's own values.
func snippetPaths(msg string) []string {
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	var out []string
	for _, line := range strings.Split(msg, "\n") {
		m := snippetRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, frame{indent, m[2]})
		if m[3] != "" {
			keys := make([]string, len(stack))
			for i, f := range stack {
				keys[i] = f.key
			}
			out = append(out, strings.Join(keys, "."))
		}
		if len(out) >= 4*maxNeeds {
			break
		}
	}
	return out
}

// lookupPath finds a dotted path in the chart's default values; a key whose value is null still counts.
func lookupPath(vals map[string]any, path []string) (any, bool) {
	var cur any = vals
	for _, seg := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func describeNeed(path string, def any) ValueNeed {
	leaf := path[strings.LastIndex(path, ".")+1:]
	n := ValueNeed{Path: path}
	switch v := def.(type) {
	case bool:
		n.Kind = "bool"
	case int, int64, float64:
		n.Kind = "number"
	case map[string]any:
		n.Kind = "object"
		for k, c := range v {
			switch c.(type) {
			case map[string]any, []any:
			default:
				n.Children = append(n.Children, k)
			}
		}
		sort.Strings(n.Children)
		if len(n.Children) > 8 {
			n.Children = n.Children[:8]
		}
		if len(v) == 0 && secretRefRe.MatchString(leaf) {
			n.Children, n.Guessed = []string{"name", "key"}, true
		}
	case []any:
		n.Kind = "list"
	default: // a string, or null (an empty placeholder)
		n.Kind = "string"
	}
	n.SecretRef = secretRefRe.MatchString(leaf) && !fileRe.MatchString(leaf)
	n.Sensitive = n.Kind == "string" && !n.SecretRef && !fileRe.MatchString(leaf) && sensitiveRe.MatchString(leaf)
	return n
}
