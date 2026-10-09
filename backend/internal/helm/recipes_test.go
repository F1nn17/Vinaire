package helm

import (
	"os"
	"strings"
	"testing"
)

func TestEmbeddedRecipesAreValid(t *testing.T) {
	r, err := loadRecipes(embeddedRecipes)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"keycloak/keycloakx", "keycloak/keycloak", "gitea/gitea", "nextcloud/nextcloud", "metabase/metabase", "grafana/grafana", "bitnami-apps/keycloak", "bitnami-apps/wordpress"} {
		if r[key] == nil {
			t.Errorf("no recipe for %s", key)
		}
	}
	if RecipeFor("keycloak", "keycloakx") == nil || RecipeFor("nope", "nope") != nil {
		t.Error("RecipeFor looks up by repo/chart")
	}
}

func TestEveryDatabaseRecipeKeepsPasswordsOutOfTheForm(t *testing.T) {
	for key, r := range builtinRecipes {
		for _, s := range r.Sections {
			for _, o := range s.Options {
				for _, f := range o.Fields {
					low := strings.ToLower(f.Key + " " + f.Label)
					if strings.Contains(low, "password") && f.Kind != "secret" && !strings.Contains(low, "key") && !strings.Contains(low, "secret") {
						t.Errorf("%s: field %q looks like a typed password — it must be a reference to a Secret", key, f.Key)
					}
				}
			}
		}
	}
}

func TestRecipeValidationRejectsMistakes(t *testing.T) {
	bad := map[string]string{
		"unknown placeholder": `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[{"path":"a.b","value":"{nope}"}]},{"id":"p","title":"P","set":[]}]}]}}`,
		"no default":          `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","set":[]},{"id":"p","title":"P","set":[]}]}]}}`,
		"two defaults":        `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[]},{"id":"p","title":"P","default":true,"set":[]}]}]}}`,
		"one option":          `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[]}]}]}}`,
		"bad path":            `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[{"path":"a..b","value":1}]},{"id":"p","title":"P","set":[]}]}]}}`,
		"bad kind":            `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"fields":[{"key":"k","label":"K","kind":"shell"}],"set":[]},{"id":"p","title":"P","set":[]}]}]}}`,
		"unconstrained text":  `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"fields":[{"key":"k","label":"K"}],"set":[{"path":"a.b","value":"- name: {k}\n"}]},{"id":"p","title":"P","set":[]}]}]}}`,
		"default off pattern": `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"fields":[{"key":"k","label":"K","default":"UPPER","pattern":"^[a-z]+$"}],"set":[]},{"id":"p","title":"P","set":[]}]}]}}`,
	}
	for name, doc := range bad {
		if _, err := loadRecipes([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAfterInstallTextsOnlyUseWhatTheInterfaceFillsIn(t *testing.T) {
	doc := `{"a/b":{"after":["see {release} and {password}"],"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[]},{"id":"p","title":"P","set":[]}]}]}}`
	if _, err := loadRecipes([]byte(doc)); err == nil {
		t.Error("an after text with an unknown placeholder was accepted")
	}
}

func TestOptionalPlaceholdersAreValidWhenTheFieldExists(t *testing.T) {
	ok := `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"fields":[{"key":"ns","label":"NS"}],"set":[{"path":"a.b","value":[{"namespace":"{?ns}"}]}]},{"id":"p","title":"P","set":[]}]}]}}`
	if _, err := loadRecipes([]byte(ok)); err != nil {
		t.Errorf("rejected: %v", err)
	}
	bad := `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[{"path":"a.b","value":"{?ghost}"}]},{"id":"p","title":"P","set":[]}]}]}}`
	if _, err := loadRecipes([]byte(bad)); err == nil {
		t.Error("an optional placeholder for a field that doesn't exist was accepted")
	}
}

func TestOperatorRecipes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, doc string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := `{"myrepo/mychart":{"summary":"mine","sections":[{"id":"db","title":"DB","options":[{"id":"a","title":"A","default":true,"set":[]},{"id":"b","title":"B","set":[{"path":"x.y","value":true}]}]}]}}`
	n, err := LoadExtraRecipes(write("good.json", good))
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if r := RecipeFor("myrepo", "mychart"); r == nil || r.Summary != "mine" {
		t.Error("the operator's recipe isn't served")
	}
	delete(builtinRecipes, "myrepo/mychart")

	for name, doc := range map[string]string{
		"unknown field": `{"a/b":{"surprise":1,"sections":[]}}`,
		"bad key":       `{"nope":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[]},{"id":"p","title":"P","set":[]}]}]}}`,
		"invalid":       `{"a/b":{"sections":[]}}`,
		"not json":      `[`,
	} {
		if _, err := LoadExtraRecipes(write(strings.ReplaceAll(name, " ", "_")+".json", doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := LoadExtraRecipes(dir + "/missing.json"); err == nil {
		t.Error("a missing file was accepted")
	}
	// one bad recipe refuses the whole file: the good one in it isn't loaded either
	mixed := `{"ok/chart":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"set":[]},{"id":"p","title":"P","set":[]}]}]},"bad/chart":{"sections":[]}}`
	if _, err := LoadExtraRecipes(write("mixed.json", mixed)); err == nil || RecipeFor("ok", "chart") != nil {
		t.Error("a file with one bad recipe was partly loaded")
	}
}

func TestGeneratedSecretsAreOnlyOfferedWhereTheyAreSafe(t *testing.T) {
	wrap := func(field string) string {
		return `{"a/b":{"sections":[{"id":"x","title":"X","options":[{"id":"o","title":"O","default":true,"fields":[` + field + `],"set":[]},{"id":"p","title":"P","set":[]}]}]}}`
	}
	good := wrap(`{"key":"s","label":"S","kind":"secret","generate":{"passwordKeyField":"k"}},{"key":"k","label":"K"}`)
	if _, err := loadRecipes([]byte(good)); err != nil {
		t.Errorf("rejected: %v", err)
	}
	for name, doc := range map[string]string{
		"not a secret field": wrap(`{"key":"s","label":"S","generate":{"passwordKey":"p"}}`),
		"no password key":    wrap(`{"key":"s","label":"S","kind":"secret","generate":{}}`),
		"both password keys": wrap(`{"key":"s","label":"S","kind":"secret","generate":{"passwordKey":"p","passwordKeyField":"k"}},{"key":"k","label":"K"}`),
		"user without a key": wrap(`{"key":"s","label":"S","kind":"secret","generate":{"passwordKey":"p","user":"admin"}}`),
		"unknown key field":  wrap(`{"key":"s","label":"S","kind":"secret","generate":{"passwordKeyField":"ghost"}}`),
		"odd key":            wrap(`{"key":"s","label":"S","kind":"secret","generate":{"passwordKey":"a b"}}`),
	} {
		if _, err := loadRecipes([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
