// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mountDesktopShell } from "../../src/mount.js";
import { installAccessibilityProbe, isPackagedProbeMode, signalAccessibilityProbeReady } from "../../src/native-shell.js";
import { mountPackagedProbeShell } from "../../src/packaged-probe.js";
import { createDesktopWailsComposition } from "../../src/wails-bootstrap.js";
import { CreateMCPConnection, Invoke, ListMCPConnections, MCPStatus, RestartMCP, RevokeMCPConnection, SetMCPEnabled, State } from "../wailsjs/go/desktopwails/FrontendBridge.js";
import { EventsOff, EventsOn } from "../wailsjs/runtime/runtime.js";

async function start(): Promise<void> {
  installAccessibilityProbe(EventsOn);
  const root = document.querySelector("#root");
  if (root === null) throw new Error("Desktop root is unavailable");
  if (await isPackagedProbeMode()) {
    mountPackagedProbeShell(root);
    await signalAccessibilityProbeReady();
    return;
  }
  const composition = await createDesktopWailsComposition(
    { State },
		{ EventsOn, EventsOff },
		{ MCPStatus, SetMCPEnabled, RestartMCP, ListMCPConnections, CreateMCPConnection, RevokeMCPConnection },
    (method, exchange) => Invoke(method, exchange),
  );
  mountDesktopShell(root, {
    lifecycle: composition.lifecycle,
		viewer: composition.viewer,
		mcp: composition.mcp,
    viewSelectionCapability: "engine.materialize_view",
    editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
  });
  await signalAccessibilityProbeReady();
}

void start().catch(() => {
  const root = document.querySelector("#root");
  if (root !== null) root.replaceChildren(Object.assign(document.createElement("p"), { role: "alert", textContent: "LayerDraw Desktop could not start." }));
});
