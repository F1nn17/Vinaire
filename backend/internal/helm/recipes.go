package helm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	// After is shown once the install is done: where to go next. {release} and {namespace} are
	// filled in by the interface; nothing else may appear in these texts.
	After []string `json:"after,omitempty"`
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
	ID      string `json:"id"`
	Title   string `json:"title"`
	Note    string `json:"note,omitempty"`
	Default bool   `json:"default,omitempty"`
	// Auto (on a default option, without fields) is written into the values as soon as the install
	// page opens — for a setting without which the chart does not work at all (an image that no
	// longer exists under its default name). It is visible in "Your values" and can be undone by
	// choosing another option.
	Auto   bool          `json:"auto,omitempty"`
	Fields []RecipeField `json:"fields,omitempty"`
	Set    []RecipeSet   `json:"set"`
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
	// Generate (secret fields only) lets the interface offer "create this Secret with a generated
	// password" — for an administrator account, where nothing outside the cluster has to agree with
	// the password. It says which keys the new Secret gets.
	Generate *RecipeGenerate `json:"generate,omitempty"`
	// Pattern (a regular expression) is what the interface and the tests hold the value to; every
	// value ends up inside YAML, so a field that can reach a text template is always constrained.
	Pattern string `json:"pattern,omitempty"`
}

// RecipeGenerate names the keys of a Secret the interface may create with a generated password:
// the password's key (a literal, or the field that holds it), and optionally a user name with its
// key (the same two ways).
type RecipeGenerate struct {
	PasswordKey      string `json:"passwordKey,omitempty"`
	PasswordKeyField string `json:"passwordKeyField,omitempty"`
	User             string `json:"user,omitempty"`
	UserKey          string `json:"userKey,omitempty"`
	UserKeyField     string `json:"userKeyField,omitempty"`
}

// RecipeSet puts one value at a path ("a.b.0.c": numbers index lists). Strings (also inside
// lists and maps) may hold {field} placeholders; an entry that mentions a field the person left
// empty is skipped — except {?field}, an optional one: a map key whose value is just that
// placeholder is left out when the field is empty, and elsewhere it becomes an empty string. As "number" turns the substituted string into a number.
type RecipeSet struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
	As    string `json:"as,omitempty"`
}

var (
	placeholderRe = regexp.MustCompile(`\{\??([a-zA-Z][a-zA-Z0-9]*)\}`)
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

// maxRecipesFile bounds an operator's recipes file.
const maxRecipesFile = 256 << 10

var recipeKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9.-]*$`)

// LoadExtraRecipes adds the operator's own recipes (CATALOG_RECIPES: a JSON file shaped like
// recipes.json) to the built-in ones; a recipe for a chart the built-in set also covers replaces
// it. The file goes through exactly the checks the built-in data does — an unknown field, a
// missing default option, a field that can reach YAML text without a pattern — and one bad
// recipe refuses the whole file, so what is accepted is as safe as what ships. Call it once at
// startup, before the server takes requests.
func LoadExtraRecipes(path string) (int, error) {
	f, err := os.Open(path) // #nosec G304 -- path is CATALOG_RECIPES, set by the operator
	if err != nil {
		return 0, fmt.Errorf("reading recipes: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecipesFile+1))
	if err != nil {
		return 0, fmt.Errorf("reading recipes: %w", err)
	}
	if len(data) > maxRecipesFile {
		return 0, fmt.Errorf("recipes file is larger than %d KiB", maxRecipesFile>>10)
	}
	extra, err := loadRecipesStrict(data)
	if err != nil {
		return 0, err
	}
	for key, r := range extra {
		builtinRecipes[key] = r
	}
	return len(extra), nil
}

// loadRecipesStrict is loadRecipes for input that did not ship with Vinaire: unknown fields are
// refused and the keys must look like "repo/chart".
func loadRecipesStrict(data []byte) (map[string]*Recipe, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out map[string]*Recipe
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("recipes: %w", err)
	}
	for key, r := range out {
		if !recipeKeyRe.MatchString(key) || r == nil {
			return nil, fmt.Errorf("recipes: %q is not a repo/chart key", key)
		}
		if err := r.validate(); err != nil {
			return nil, fmt.Errorf("recipes: %s: %w", key, err)
		}
	}
	return out, nil
}

// RecipeFor returns the recipe of a chart, or nil.
func RecipeFor(repo, chart string) *Recipe { return builtinRecipes[repo+"/"+chart] }

func (r *Recipe) validate() error {
	if len(r.Sections) == 0 {
		return fmt.Errorf("no sections")
	}
	for _, text := range r.After {
		for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
			if m[1] != "release" && m[1] != "namespace" {
				return fmt.Errorf("an \"after\" text uses {%s}; only {release} and {namespace} are filled in", m[1])
			}
		}
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
			if o.Auto && (!o.Default || len(o.Fields) > 0) {
				return fmt.Errorf("section %s, option %s: auto is only for a default option with no fields", s.ID, o.ID)
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
	for _, f := range o.Fields {
		if f.Generate == nil {
			continue
		}
		g := f.Generate
		if f.Kind != "secret" {
			return fmt.Errorf("field %s: only a secret field can have a generated password", f.Key)
		}
		if (g.PasswordKey == "") == (g.PasswordKeyField == "") {
			return fmt.Errorf("field %s: generate needs exactly one of passwordKey and passwordKeyField", f.Key)
		}
		if g.User != "" && (g.UserKey == "") == (g.UserKeyField == "") {
			return fmt.Errorf("field %s: a generated user name needs exactly one of userKey and userKeyField", f.Key)
		}
		for _, lit := range []string{g.PasswordKey, g.UserKey} {
			if lit != "" && !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(lit) {
				return fmt.Errorf("field %s: generate key %q", f.Key, lit)
			}
		}
		if g.User != "" && !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(g.User) {
			return fmt.Errorf("field %s: generate user %q", f.Key, g.User)
		}
		for _, ref := range []string{g.PasswordKeyField, g.UserKeyField} {
			if ref != "" {
				if _, ok := fields[ref]; !ok {
					return fmt.Errorf("field %s: generate refers to field %q, which the option doesn't have", f.Key, ref)
				}
			}
		}
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
