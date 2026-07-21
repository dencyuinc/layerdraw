// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { DesktopFeatureAvailability, DesktopMCPPort, DesktopProjectDialogPort, DesktopShellPorts } from "./contracts.js";
import { DesktopShellController } from "./controller.js";
import type { DesktopEditorCapabilityIDs } from "./editor-surface.js";
import type { ReviewModel } from "@layerdraw/review";
import type { LibraryController } from "@layerdraw/library";
import { I18nProvider, baseShellCatalogs, resolveLocale } from "@layerdraw/react/i18n";
import { DesktopShell } from "./shell.js";

export interface DesktopMountOptions extends DesktopShellPorts {
  readonly mcp: DesktopMCPPort;
  readonly projectDialogs?: DesktopProjectDialogPort;
  readonly viewSelectionCapability: CapabilityID;
  readonly editorCapabilities: DesktopEditorCapabilityIDs;
  readonly reviewModel?: ReviewModel;
  readonly library?: LibraryController;
  readonly libraryAvailability?: DesktopFeatureAvailability;
  readonly reviewAvailability?: DesktopFeatureAvailability;
  /** Returns to the project hub from an open workspace without a process restart. */
  readonly onReturnToProjects?: () => void;
}

export interface MountedDesktopShell {
  readonly controller: DesktopShellController;
  close(): Promise<void>;
}

/**
 * Production frontend composition seam. The Wails AppOption/bootstrap creates
 * exact generated clients and lifecycle adapters, then injects them here.
 */
export function mountDesktopShell(element: Element, options: DesktopMountOptions): MountedDesktopShell {
  const controller = new DesktopShellController({ lifecycle: options.lifecycle, viewer: options.viewer });
  const root: Root = createRoot(element);
  const locale = resolveLocale(typeof navigator === "undefined" ? undefined : navigator.language);
  root.render(createElement(I18nProvider, { locale, catalogs: baseShellCatalogs }, createElement(DesktopShell, {
    controller,
    viewSelectionCapability: options.viewSelectionCapability,
    editorCapabilities: options.editorCapabilities,
    mcp: options.mcp,
    ...(options.projectDialogs === undefined ? {} : { projectDialogs: options.projectDialogs }),
    ...(options.libraryAvailability === undefined ? {} : { libraryAvailability: options.libraryAvailability }),
    ...(options.reviewAvailability === undefined ? {} : { reviewAvailability: options.reviewAvailability }),
    ...(options.reviewModel === undefined ? {} : { reviewModel: options.reviewModel }),
    ...(options.library === undefined ? {} : { library: options.library }),
    ...(options.onReturnToProjects === undefined ? {} : { onReturnToProjects: options.onReturnToProjects }),
  })));
  return {
    controller,
    async close() {
      root.unmount();
      await controller.close();
    },
  };
}
