// Renders what the "Install from an image" wizard would create (Roadmap Phase 78), so the integration
// tests install exactly that and not a hand-copied approximation that drifts from the wizard.
//
//   npx --yes tsx@4.20.3 scripts/render-image-fixtures.ts <outDir> <namespace> <preset>:<name> [...]
//
// For each `preset:name` it writes <outDir>/<name>.yaml (Secret, Service, StatefulSet — in the order the
// wizard applies them), with a small volume and fixed test passwords. Test-only values; never run
// against a real cluster.
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { build, newForm, presets, validate } from "../src/lib/images/presets";

const [outDir, namespace, ...specs] = process.argv.slice(2);
if (!outDir || !namespace || specs.length === 0) {
  console.error(
    "usage: render-image-fixtures.ts <outDir> <namespace> <preset>:<name>...",
  );
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });

for (const spec of specs) {
  const [id, name] = spec.split(":");
  const preset = presets.find((p) => p.id === id);
  if (!preset || !name) {
    console.error(`unknown preset or missing name: ${spec}`);
    process.exit(2);
  }
  const form = newForm(preset);
  form.name = name;
  form.namespace = namespace;
  form.size = "1Gi";
  const problems = validate(form);
  if (Object.keys(problems).length > 0) {
    console.error(`${spec}: ${JSON.stringify(problems)}`);
    process.exit(1);
  }
  const passwords = Object.fromEntries(
    preset.secrets.map((k) => [k, "ci-test-password"]),
  );
  const docs = build(form, passwords);
  writeFileSync(
    join(outDir, `${name}.yaml`),
    docs.map((d) => d.yaml).join("---\n"),
  );
  // what the tests need to know about the instance without parsing YAML
  writeFileSync(
    join(outDir, `${name}.json`),
    JSON.stringify({
      preset: id,
      name,
      namespace,
      user: form.values.user ?? "",
      database: form.values.database ?? "",
      password: "ci-test-password",
      tag: form.tag,
    }),
  );
  console.log(`rendered ${spec}`);
}
