package helm

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Install recipes (Roadmap Phase 59): for a chart the catalog knows well, what the install page
// can offer beyond a bare values editor — "use my own PostgreSQL instead of the bundled one",
// "publish it through an Ingress" — each as a few fields that become Helm values.
//
// A recipe is DATA, shipped with Vinaire (recipes.json), never taken from a request: like the
// catalog's repositories, what a click can change is decided by the operator's side. A recipe only
// ever produces values for the chart the person is installing — it does not install, create or
// name anything itself; the install is the same audited one, run as the caller. Secrets are never
// typed into these fields: a password is a reference (the name of an existing Secret and a key).

//go:embed recipes.json
var embeddedRecipes []byte

// Recipe is the install help for one chart.
type Recipe struct {
	Summary  string          `json:"summary,omitempty"`
	Guide    []string        `json:"guide,omitempty"`
	Sections []RecipeSection `json:"sections"`
}

// RecipeSection is one decision: a database, how to reach the app.
type RecipeSection struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Help  string `json:"help,omitempty"`
	// Discover asks the interface to look in the cluster for what the section needs
	// ("postgres": Services that look like a PostgreSQL) and offer them.
	Discover string         `json:"discover,omitempty"`
	Options  []RecipeOption `json:"options"`
}

// RecipeOption is one answer to a section.
type RecipeOption struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Note    string        `json:"note,omitempty"`
	Default bool          `json:"default,omitempty"`
	Fields  []RecipeField `json:"fields,omitempty"`
	Set     []RecipeSet   `json:"set"`
}

// RecipeField is a value the person types for an option.
type RecipeField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	// Kind: "" (text), "number", "secret" (the name of an existing Secret — offered from the
	// namespace's own), "ingressclass" (offered from the cluster's classes).
	Kind string `json:"kind,omitempty"`
	// Pattern (a regular expression) is what the interface and the tests hold the value to; every
	// value ends up inside YAML, so a field that can reach a text template is always constrained.
	Pattern string `json:"pattern,omitempty"`
}

// RecipeSet puts one value at a path ("a.b.0.c": numbers index lists). Strings (also inside
// lists and maps) may hold {field} placeholders; an entry that mentions a field the person left
// empty is skipped. As "number" turns the substituted string into a number.
type RecipeSet struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
	As    string `json:"as,omitempty"`
}

var (
	placeholderRe = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9]*)\}`)
	recipePathRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
	recipeIDRe    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// recipes maps "repo/chart" to its recipe.
func loadRecipes(data []byte) (map[string]*Recipe, error) {
	var out map[string]*Recipe
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("recipes: %w", err)
	}
	for key, r := range out {
		if err := r.validate(); err != nil {
			return nil, fmt.Errorf("recipes: %s: %w", key, err)
		}
	}
	return out, nil
}

var builtinRecipes = func() map[string]*Recipe {
	r, err := loadRecipes(embeddedRecipes)
	if err != nil {
		panic("embedded recipes.json is invalid: " + err.Error()) // a build-time bug, caught by tests
	}
	return r
}()

// RecipeFor returns the recipe of a chart, or nil.
func RecipeFor(repo, chart string) *Recipe { return builtinRecipes[repo+"/"+chart] }

func (r *Recipe) validate() error {
	if len(r.Sections) == 0 {
		return fmt.Errorf("no sections")
	}
	seenSection := map[string]bool{}
	for _, s := range r.Sections {
		if !recipeIDRe.MatchString(s.ID) || seenSection[s.ID] {
			return fmt.Errorf("section id %q is invalid or repeated", s.ID)
		}
		seenSection[s.ID] = true
		if s.Title == "" || len(s.Options) < 2 {
			return fmt.Errorf("section %s needs a title and at least two options", s.ID)
		}
		defaults := 0
		seenOption := map[string]bool{}
		for _, o := range s.Options {
			if !recipeIDRe.MatchString(o.ID) || seenOption[o.ID] {
				return fmt.Errorf("section %s: option id %q is invalid or repeated", s.ID, o.ID)
			}
			seenOption[o.ID] = true
			if o.Default {
				defaults++
			}
			if err := o.validate(); err != nil {
				return fmt.Errorf("section %s, option %s: %w", s.ID, o.ID, err)
			}
		}
		if defaults != 1 {
			return fmt.Errorf("section %s needs exactly one default option", s.ID)
		}
	}
	return nil
}

func (o RecipeOption) validate() error {
	fields := map[string]RecipeField{}
	for _, f := range o.Fields {
		if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`).MatchString(f.Key) {
			return fmt.Errorf("field key %q", f.Key)
		}
		if _, dup := fields[f.Key]; dup {
			return fmt.Errorf("field %q repeated", f.Key)
		}
		switch f.Kind {
		case "", "number", "secret", "ingressclass":
		default:
			return fmt.Errorf("field %s: unknown kind %q", f.Key, f.Kind)
		}
		if f.Pattern != "" {
			re, err := regexp.Compile(f.Pattern)
			if err != nil {
				return fmt.Errorf("field %s: pattern: %w", f.Key, err)
			}
			if f.Default != "" && !re.MatchString(f.Default) {
				return fmt.Errorf("field %s: its default %q doesn't match its own pattern", f.Key, f.Default)
			}
		}
		fields[f.Key] = f
	}
	for _, s := range o.Set {
		if !recipePathRe.MatchString(s.Path) {
			return fmt.Errorf("path %q", s.Path)
		}
		if s.As != "" && s.As != "number" {
			return fmt.Errorf("path %s: unknown conversion %q", s.Path, s.As)
		}
		for _, name := range placeholdersIn(s.Value) {
			f, ok := fields[name]
			if !ok {
				return fmt.Errorf("path %s uses {%s}, which is not a field of the option", s.Path, name)
			}
			// a value that lands in YAML text (not as a structured value) must be constrained
			if _, text := s.Value.(string); text && strings.Contains(s.Value.(string), "\n") && f.Pattern == "" && f.Kind != "number" {
				return fmt.Errorf("path %s puts field %s into YAML text but the field has no pattern", s.Path, name)
			}
		}
	}
	return nil
}

func placeholdersIn(v any) []string {
	switch x := v.(type) {
	case string:
		var out []string
		for _, m := range placeholderRe.FindAllStringSubmatch(x, -1) {
			out = append(out, m[1])
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, placeholdersIn(e)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, e := range x {
			out = append(out, placeholdersIn(e)...)
		}
		return out
	}
	return nil
}
