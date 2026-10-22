"use client";

import { ResourceDetailView } from "@/components/resources/ResourceDetailView";

// A Service, Ingress or Gateway object opened from Routing: the same overview and YAML editor as in
// "All resources", but inside Routing — the sidebar keeps Routing selected and the way back is the list.
export default function NetworkDetailRoute() {
  return <ResourceDetailView context="routing" />;
}
