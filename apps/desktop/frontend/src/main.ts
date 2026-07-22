// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { mountDesktopShell } from "../../src/mount.js";
import { installAccessibilityProbe, isPackagedProbeMode, signalAccessibilityProbeReady } from "../../src/native-shell.js";
import { mountPackagedProbeShell } from "../../src/packaged-probe.js";
import { createDesktopWailsComposition, waitForDesktopApplicationReady, type DesktopWailsApplicationBinding, type DesktopWailsMCPBinding } from "../../src/wails-bootstrap.js";
import { createDesktopGeneratedBindings, type DesktopWailsInvoke } from "../../src/wails-bindings.js";
import { createDesktopWailsProjectOwner } from "../../src/wails-project-owner.js";
import type { DesktopProjectHostBinding, DesktopReviewHostBinding } from "../../src/wails-owner.js";
import { AcquireExternalLease, ApplyExternalReconcile, ConnectExternal, CreateMCPConnection, CloseCurrentProject, DeleteMCPConnection, CreateProjectDialog, DisconnectExternal, ImportExternalDialog, InspectExternal, Invoke, ListMCPConnections, MaterializeProjectView, MCPClientConfig, MCPStatus, NativeExportProfiles, OpenProjectDialog, OpenRecentProject, PlanExternalReconcile, PreviewEditor, ProjectPublication, PublishNativeExportDialog, RecentProjects, RefreshExternal, RegistryDispatch, RestartMCP, RevokeMCPConnection, ReviewApproveAndApply, ReviewComment, ReviewSnapshot, ReviewWithdraw, SelectExternalRemote, SerializeNativeExport, SetMCPEnabled, State } from "../wailsjs/go/desktopwails/FrontendBridge.js";
import { Settings, UpdateSettings } from "../wailsjs/go/desktopwails/ShellBinding.js";
import { EventsOff, EventsOn, LogError } from "../wailsjs/runtime/runtime.js";
import type { DesktopSettingsDTO, DesktopSettingsPort } from "../../src/contracts.js";

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
    State, CloseCurrentProject, CreateProjectDialog, OpenProjectDialog, RecentProjects, OpenRecentProject,
    ConnectExternal, InspectExternal, RefreshExternal, DisconnectExternal,
    SelectExternalRemote, AcquireExternalLease, PlanExternalReconcile, ApplyExternalReconcile,
    NativeExportProfiles, SerializeNativeExport, PublishNativeExportDialog, ImportExternalDialog,
  } as unknown as DesktopWailsApplicationBinding;
  await waitForDesktopApplicationReady(application);
  const generatedBindings = createDesktopGeneratedBindings(Invoke as unknown as DesktopWailsInvoke);
  const project = await createDesktopWailsProjectOwner({ ProjectPublication, PreviewEditor, MaterializeProjectView } as unknown as DesktopProjectHostBinding, generatedBindings);
  const composition = await createDesktopWailsComposition(
    application,
    { EventsOn, EventsOff, LogError },
    { MCPStatus, SetMCPEnabled, RestartMCP, ListMCPConnections, CreateMCPConnection, RevokeMCPConnection, MCPClientConfig, DeleteMCPConnection } as unknown as DesktopWailsMCPBinding,
    Invoke as unknown as DesktopWailsInvoke,
    {
      project,
      registry: { RegistryDispatch },
      review: { ReviewSnapshot, ReviewComment, ReviewApproveAndApply, ReviewWithdraw } as unknown as DesktopReviewHostBinding,
    },
  );
  const settingsPort: DesktopSettingsPort = {
    load: async () => await Settings() as Awaited<ReturnType<DesktopSettingsPort["load"]>>,
    update: async (value: DesktopSettingsDTO) => await UpdateSettings(value) as Awaited<ReturnType<DesktopSettingsPort["update"]>>,
  };
  const mounted = mountDesktopShell(root, {
    settings: settingsPort,
    lifecycle: composition.lifecycle,
		viewer: composition.viewer,
		mcp: composition.mcp,
		projectDialogs: composition.projectDialogs,
		onReturnToProjects: () => { void composition.projectDialogs.close().catch(() => {}); },
		libraryAvailability: composition.library.status === "available" ? { status: "available" } : composition.library.availability,
		...(composition.library.status === "available" ? { library: composition.library.value } : {}),
		reviewAvailability: composition.review.status === "available" ? { status: "available" } : composition.review.availability,
		...(composition.review.status === "available" ? { reviewModel: composition.review.value } : {}),
    viewSelectionCapability: "engine.materialize_view",
    editorCapabilities: { preview: "engine.preview_operations", apply: "runtime.commit_operations", history: "runtime.commit_operations" },
  });
  void settingsPort.load().then((result) => {
    const locale = result.outcome === "success" ? result.value?.locale : undefined;
    if (locale !== undefined && locale !== "" && locale !== "system") mounted.setLocale(locale);
  }, () => {});
  EventsOn("desktop:menu", (command: unknown) => {
    if (typeof command !== "string") return;
    if (command.startsWith("locale:")) {
      const locale = command.slice("locale:".length);
      mounted.setLocale(locale === "system" ? null : locale);
      return;
    }
    window.dispatchEvent(new CustomEvent("layerdraw:menu", { detail: command }));
  });
  await signalAccessibilityProbeReady();
}

void start().catch((error: unknown) => {
  LogError(`Desktop frontend startup failed: ${error instanceof Error ? `${error.name}: ${error.message}` : "unknown error"}`);
  const root = document.querySelector("#root");
  if (root !== null) root.replaceChildren(Object.assign(document.createElement("p"), { role: "alert", textContent: "LayerDraw Desktop could not start." }));
});
