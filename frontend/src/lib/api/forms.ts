import useSWR from "swr";
import { apiFetch } from "./http";

// Roadmap Phase 31: matches the backend's api.FormSchema — a small,
// hand-written field description per resource kind. Purely descriptive; the
// form's state is serialized to YAML client-side.

export type FormFieldSchema = {
  key: string;
  label: string;
  type: "string" | "number" | "keyvalue" | "node";
  required?: boolean;
  default?: string | number;
  placeholder?: string;
  help?: string;
  secret?: boolean;
  group?: string;
};

export type FormSchema = {
  resourceType: string;
  kind: string;
  label: string;
  fields: FormFieldSchema[];
};

export function useFormSchema(resourceType: string | null) {
  return useSWR(resourceType ? ["form-schema", resourceType] : null, () =>
    apiFetch<FormSchema>(`/api/v1/forms/${encodeURIComponent(resourceType!)}`),
  );
}
