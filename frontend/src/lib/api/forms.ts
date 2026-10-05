import useSWR from "swr";
import { apiFetch } from "./http";

// Roadmap Phase 31: matches the backend's api.FormSchema — a small,
// hand-written field description per resource kind. Purely descriptive; the
// form's state is serialized to YAML client-side.

export type FormFieldSchema = {
  key: string;
  label: string;
  type: "string" | "number" | "keyvalue" | "node" | "select";
  required?: boolean;
  default?: string | number;
  placeholder?: string;
  help?: string;
  secret?: boolean;
  group?: string;
  // choices of a "select" field
  options?: string[];
  // what the two columns of a "keyvalue" field are, instead of KEY / value
  keyPlaceholder?: string;
  valuePlaceholder?: string;
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
