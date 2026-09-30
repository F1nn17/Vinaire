import * as monaco from "monaco-editor";
import { loader } from "@monaco-editor/react";

// Bundle Monaco with the app instead of letting @monaco-editor/react pull it
// from a public CDN at runtime — Vinaire is served from inside a cluster,
// often with no route to jsdelivr (same reasoning as the self-hosted fonts).
self.MonacoEnvironment = {
  getWorker() {
    return new Worker(
      new URL("monaco-editor/editor/editor.worker.js", import.meta.url),
    );
  },
};

loader.config({ monaco });
