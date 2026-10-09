package helm

import (
	"strings"
	"testing"
)

func TestEmbeddedRecipesAreValid(t *testing.T) {
	r, err := loadRecipes(embeddedRecipes)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"keycloak/keycloakx", "keycloak/keycloak", "gitea/gitea", "nextcloud/nextcloud"} {
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
