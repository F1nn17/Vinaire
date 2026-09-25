import { secretsMock } from "@/lib/mock/secrets";
import type { SecretDetail } from "@/lib/types";
import { simulatedDelay } from "./config";

// TODO(vinaire): replace with `fetch(`${API_BASE}/api/v1/secrets`)`.
// The backend must only ever return metadata + still-encoded values here,
// same as this mock — never decode secret values server-side for the UI.
export async function getSecrets(): Promise<SecretDetail[]> {
  await simulatedDelay();
  return secretsMock;
}
