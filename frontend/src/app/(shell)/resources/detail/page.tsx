"use client";

import { ResourceDetailView } from "@/components/resources/ResourceDetailView";

// An object opened from "All resources" (Settings). Opened from Routing it is /network/detail.
export default function ResourceDetailRoute() {
  return <ResourceDetailView context="settings" />;
}
