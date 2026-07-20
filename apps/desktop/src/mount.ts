// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { CapabilityID } from "@layerdraw/protocol/common";
import { createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import type { DesktopFeatureAvailability, DesktopMCPPort, DesktopShellPorts } from "./contracts.js";
import { DesktopShellController } from "./controller.js";
import type { DesktopEditorCapabilityIDs } from "./editor-surface.js";
import type { ReviewModel } from "@layerdraw/review";
import { DesktopShell, type DesktopShellLabels } from "./shell.js";

export interface DesktopMountOptions extends DesktopShellPorts {
  readonly mcp: DesktopMCPPort;
  readonly viewSelectionCapability: CapabilityID;
  readonly editorCapabilities: DesktopEditorCapabilityIDs;
  readonly reviewModel?: ReviewModel;
  readonly libraryAvailability?: DesktopFeatureAvailability;
  readonly reviewAvailability?: DesktopFeatureAvailability;
  readonly labels?: DesktopShellLabels;
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
  root.render(createElement(DesktopShell, {
    controller,
    viewSelectionCapability: options.viewSelectionCapability,
    editorCapabilities: options.editorCapabilities,
    mcp: options.mcp,
    ...(options.libraryAvailability === undefined ? {} : { libraryAvailability: options.libraryAvailability }),
    ...(options.reviewAvailability === undefined ? {} : { reviewAvailability: options.reviewAvailability }),
    ...(options.reviewModel === undefined ? {} : { reviewModel: options.reviewModel }),
    ...(options.labels === undefined ? {} : { labels: options.labels }),
  }));
  return {
    controller,
    async close() {
      root.unmount();
      await controller.close();
    },
  };
}
