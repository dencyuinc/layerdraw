// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mountDesktopShell } from "../../src/mount.js";
import { installAccessibilityProbe, signalAccessibilityProbeReady } from "../../src/native-shell.js";
import { createDesktopWailsComposition } from "../../src/wails-bootstrap.js";
import { Invoke, State } from "../wailsjs/go/desktopwails/FrontendBridge.js";
import { EventsOff, EventsOn } from "../wailsjs/runtime/runtime.js";

async function start(): Promise<void> {
  installAccessibilityProbe(EventsOn);
  const root = document.querySelector("#root");
  if (root === null) throw new Error("Desktop root is unavailable");
  const composition = await createDesktopWailsComposition(
    { State },
    { EventsOn, EventsOff },
    (method, exchange) => Invoke(method, exchange),
  );
  mountDesktopShell(root, {
    lifecycle: composition.lifecycle,
    viewer: composition.viewer,
    viewSelectionCapability: "engine.materialize_view",
    editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
  });
  await signalAccessibilityProbeReady();
}

void start().catch(() => {
  const root = document.querySelector("#root");
  if (root !== null) root.replaceChildren(Object.assign(document.createElement("p"), { role: "alert", textContent: "LayerDraw Desktop could not start." }));
});
