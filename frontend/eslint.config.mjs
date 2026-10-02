import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";
import eslintConfigPrettier from "eslint-config-prettier";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  eslintConfigPrettier,
  // Roadmap Phase 36. Anything that comes from the cluster (pod names, labels,
  // annotations, event messages, log lines, provisioning output) is untrusted
  // input and is only ever rendered as text.
  {
    files: ["src/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message:
            "Never render cluster data as HTML — render it as text (Roadmap Phase 36).",
        },
        {
          selector:
            "AssignmentExpression[left.property.name=/^(innerHTML|outerHTML)$/]",
          message: "Never assign HTML strings — render as text.",
        },
        {
          selector:
            "CallExpression[callee.property.name=/^(insertAdjacentHTML|write|writeln)$/][callee.object.name=/^(document|element|el)$/]",
          message: "Never write HTML strings into the document.",
        },
        {
          selector: "NewExpression[callee.name='Function']",
          message: "No dynamic code evaluation.",
        },
        {
          selector: "CallExpression[callee.name='eval']",
          message: "No dynamic code evaluation.",
        },
      ],
      // Tokens, kubeconfigs and SSH credentials must never reach web storage.
      // The one thing kept there is which cluster was last selected (a name,
      // not a secret) — in lib/clusterStore.ts, the only file allowed.
      "no-restricted-globals": [
        "error",
        {
          name: "localStorage",
          message:
            "Don't use web storage: credentials must never be stored in the browser (only lib/clusterStore.ts may, for the selected cluster's name).",
        },
        {
          name: "sessionStorage",
          message:
            "Don't use web storage: credentials must never be stored in the browser.",
        },
      ],
    },
  },
  {
    files: ["src/lib/clusterStore.ts"],
    rules: { "no-restricted-globals": "off" },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
]);

export default eslintConfig;
