// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mountDesktopShell } from "../../src/mount.js";
import { installAccessibilityProbe, isPackagedProbeMode, signalAccessibilityProbeReady } from "../../src/native-shell.js";
import { mountPackagedProbeShell } from "../../src/packaged-probe.js";
import { createDesktopWailsComposition, type DesktopWailsApplicationBinding, type DesktopWailsMCPBinding } from "../../src/wails-bootstrap.js";
import { createDesktopGeneratedBindings, type DesktopWailsInvoke } from "../../src/wails-bindings.js";
import { createDesktopWailsProjectOwner } from "../../src/wails-project-owner.js";
import type { DesktopProjectHostBinding, DesktopReviewHostBinding } from "../../src/wails-owner.js";
import { AcquireExternalLease, ApplyExternalReconcile, ConnectExternal, CreateMCPConnection, CreateProjectDialog, DisconnectExternal, ImportExternalDialog, InspectExternal, Invoke, ListMCPConnections, MaterializeProjectView, MCPStatus, NativeExportProfiles, OpenProjectDialog, PlanExternalReconcile, PreviewEditor, ProjectPublication, PublishNativeExportDialog, RecentProjects, RefreshExternal, RegistryDispatch, RestartMCP, RevokeMCPConnection, ReviewApproveAndApply, ReviewComment, ReviewSnapshot, ReviewWithdraw, SelectExternalRemote, SerializeNativeExport, SetMCPEnabled, State } from "../wailsjs/go/desktopwails/FrontendBridge.js";
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
  const application = {
    State, CreateProjectDialog, OpenProjectDialog, RecentProjects,
    ConnectExternal, InspectExternal, RefreshExternal, DisconnectExternal,
    SelectExternalRemote, AcquireExternalLease, PlanExternalReconcile, ApplyExternalReconcile,
    NativeExportProfiles, SerializeNativeExport, PublishNativeExportDialog, ImportExternalDialog,
  } as unknown as DesktopWailsApplicationBinding;
  const generatedBindings = createDesktopGeneratedBindings(Invoke as unknown as DesktopWailsInvoke);
  const project = await createDesktopWailsProjectOwner({ ProjectPublication, PreviewEditor, MaterializeProjectView } as unknown as DesktopProjectHostBinding, generatedBindings);
  const composition = await createDesktopWailsComposition(
    application,
    { EventsOn, EventsOff },
    { MCPStatus, SetMCPEnabled, RestartMCP, ListMCPConnections, CreateMCPConnection, RevokeMCPConnection } as unknown as DesktopWailsMCPBinding,
    Invoke as unknown as DesktopWailsInvoke,
    {
      project,
      registry: { RegistryDispatch },
      review: { ReviewSnapshot, ReviewComment, ReviewApproveAndApply, ReviewWithdraw } as unknown as DesktopReviewHostBinding,
    },
  );
  mountDesktopShell(root, {
    lifecycle: composition.lifecycle,
		viewer: composition.viewer,
		mcp: composition.mcp,
		libraryAvailability: composition.library.status === "available" ? { status: "available" } : composition.library.availability,
		reviewAvailability: composition.review.status === "available" ? { status: "available" } : composition.review.availability,
		...(composition.review.status === "available" ? { reviewModel: composition.review.value } : {}),
    viewSelectionCapability: "engine.materialize_view",
    editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
  });
  await signalAccessibilityProbeReady();
}

void start().catch(() => {
  const root = document.querySelector("#root");
  if (root !== null) root.replaceChildren(Object.assign(document.createElement("p"), { role: "alert", textContent: "LayerDraw Desktop could not start." }));
});
