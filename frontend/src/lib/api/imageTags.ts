import { apiFetch } from "./http";

export type ImageTag = { name: string; updated?: string };

// The tags of an image on Docker Hub or quay.io, newest versions first (empty for another
// registry). A read of a public registry through the backend; the request names an image only.
export function getImageTags(
  image: string,
): Promise<{ registry: string; tags: ImageTag[] }> {
  return apiFetch(`/api/v1/images/tags?image=${encodeURIComponent(image)}`);
}
