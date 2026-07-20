// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export interface AccessibilityProfile {
  readonly keyboard_only?: boolean;
  readonly reduced_motion?: boolean;
  readonly screen_reader?: boolean;
  readonly probe_id?: string;
  readonly viewer_mode?: "2d" | "2.5d";
  readonly window_width?: number;
  readonly window_height?: number;
}

interface ShellResult<T> {
  readonly outcome: string;
  readonly value: T;
}

interface CommandStatus {
  readonly id: string;
  readonly generation: string;
}

interface NativeShellBinding {
  PackagedProbeMode(): Promise<boolean>;
  CommandStatus(): Promise<ShellResult<readonly CommandStatus[]>>;
  InvokeCommand(input: Readonly<{id: string; source: string; status_generation: string}>): Promise<ShellResult<CommandStatus>>;
  AccessibilityProbeReady(): Promise<void>;
  SubmitAccessibilityReport(id: string, report: Readonly<Record<string, boolean | number | string>>): Promise<void>;
}

function nativeShell(): NativeShellBinding {
  const binding = (window as typeof window & {go?: {desktopwails?: {ShellBinding?: NativeShellBinding}}}).go?.desktopwails?.ShellBinding;
  if (binding === undefined) throw new Error("native shell binding is unavailable");
  return binding;
}

async function invokeSettings(): Promise<boolean> {
  const shell = nativeShell();
  const statuses = await shell.CommandStatus();
  const current = statuses.outcome === "success" ? statuses.value.find((value) => value.id === "desktop.settings") : undefined;
  if (current === undefined) return false;
  const result = await shell.InvokeCommand({id: current.id, source: "control", status_generation: current.generation});
  return result.outcome === "success";
}

export async function auditAccessibility(profile: AccessibilityProfile): Promise<Readonly<Record<string, boolean | number | string>>> {
  const waitForPaint = async (): Promise<void> => {
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
  };
  const mode = profile.viewer_mode ?? "2d";
  const modeControl = [...document.querySelectorAll<HTMLButtonElement>(".ld-desktop-view-mode button")].find((button) => button.textContent?.trim().toLowerCase() === mode);
  modeControl?.click();
  await waitForPaint();
  const controls = [...document.querySelectorAll<HTMLElement>("button,input,select,textarea,a[href],[tabindex]")]
    .filter((node) => node.tabIndex >= 0 && !node.hasAttribute("disabled"));
  const labelsComplete = controls.every((node) =>
    (node.textContent?.trim().length ?? 0) > 0 || node.hasAttribute("aria-label") || node.hasAttribute("aria-labelledby") ||
    ("labels" in node && Array.from((node as HTMLInputElement).labels ?? []).some((label) => (label.textContent?.trim().length ?? 0) > 0)) ||
    (node.closest("label")?.textContent?.trim().length ?? 0) > 0);
  const focusOrderValid = controls.every((node, index) => {
    node.focus();
	const previous = controls[index - 1];
	const followsPrevious = node.tabIndex === 0 && (index === 0 || (previous !== undefined && Boolean(previous.compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING)));
	const outlineVisible = getComputedStyle(node).outlineStyle !== "none";
	const focusRect = node.matches(".ld-desktop-viewer-item") ? node.querySelector<SVGElement>("rect") : null;
	const strokeVisible = focusRect !== null && Number.parseFloat(getComputedStyle(focusRect).strokeWidth) >= 3;
    return document.activeElement === node && followsPrevious && (outlineVisible || strokeVisible);
  });
  const keyboardWorkflowValid = !profile.keyboard_only || await invokeSettings();
  document.documentElement.dataset.reducedMotion = String(Boolean(profile.reduced_motion));
  const reducedMotionHonored = !profile.reduced_motion || [...document.querySelectorAll<HTMLElement>(".ld-desktop-shell, .ld-desktop-shell *, .ld-desktop-shell *::before, .ld-desktop-shell *::after")]
    .every((node) => {
      const style = getComputedStyle(node);
      return Number.parseFloat(style.animationDuration) <= .001 && Number.parseFloat(style.transitionDuration) <= .001;
    });
  const rgb = (value: string): number[] => {
    const values = (value.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number);
    return value.startsWith("color(srgb") ? values.map((channel) => channel * 255) : values;
  };
  const luminance = (value: string): number => rgb(value).map((channel) => channel / 255)
    .map((channel) => channel <= .03928 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4)
    .reduce((sum, channel, index) => sum + channel * ([.2126, .7152, .0722][index] ?? 0), 0);
  const background = (node: Element): string => {
    let current: Element | null = node;
    while (current !== null) {
      const value = getComputedStyle(current).backgroundColor;
      if (!value.endsWith(", 0)") && value !== "transparent") return value;
      current = current.parentElement;
    }
    return "rgb(255, 255, 255)";
  };
  const contrastNodes = [...document.querySelectorAll<HTMLElement>(".ld-desktop-shell h1, .ld-desktop-shell h2, .ld-desktop-shell h3, .ld-desktop-shell p, .ld-desktop-shell button, .ld-desktop-shell dt, .ld-desktop-shell dd, .ld-desktop-shell small, .ld-desktop-shell span")]
    .filter((node) => node.getClientRects().length > 0 && (node.textContent?.trim().length ?? 0) > 0);
  const ratios = contrastNodes.map((node) => {
      const foreground = luminance(getComputedStyle(node).color);
      const behind = luminance(background(node));
      return (Math.max(foreground, behind) + .05) / (Math.min(foreground, behind) + .05);
    });
  const minimumContrast = ratios.length === 0 ? 0 : Math.min(...ratios);
  const landmarks = ["main", "nav[aria-label]", "section[aria-label]", "aside[aria-label]"]
    .every((selector) => document.querySelector(selector) !== null);
  const labelledGraphics = [...document.querySelectorAll<HTMLElement>("svg[role],canvas[role]")]
    .every((node) => (node.getAttribute("aria-label")?.trim().length ?? 0) > 0);
  const screenReaderSemantics = landmarks && labelledGraphics && document.querySelector("h1") !== null;
  const viewerItems = mode === "2.5d"
    ? [...document.querySelectorAll<HTMLElement>(".ld-desktop-viewer-three ul button")]
    : [...document.querySelectorAll<HTMLElement>(".ld-desktop-viewer-item")];
  const firstViewerItem = viewerItems[0];
  if (firstViewerItem !== undefined) {
    firstViewerItem.focus();
    firstViewerItem.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await waitForPaint();
  }
  const viewerKeyboardSelection = firstViewerItem?.getAttribute("aria-pressed") === "true";
  const canvas = document.querySelector<HTMLCanvasElement>(".ld-desktop-viewer-three-canvas[data-render-ready='true']");
  const gl = canvas?.getContext("webgl2") ?? canvas?.getContext("webgl") ?? null;
  const webglVerified = mode !== "2.5d" || (gl !== null && !gl.isContextLost() && gl.drawingBufferWidth > 0 && gl.drawingBufferHeight > 0 && String(gl.getParameter(gl.VERSION)).includes("WebGL"));
  const rendererBackend = mode === "2.5d" && webglVerified ? "three.js" : mode === "2d" && document.querySelector(".ld-desktop-viewer-canvas") !== null ? "svg" : "unavailable";
  const viewerRelationCount = mode === "2.5d"
    ? Number.parseInt(canvas?.dataset.relationCount ?? "0", 10)
    : document.querySelectorAll(".ld-desktop-viewer-path").length;
  const viewerCrossLayerRelationCount = mode === "2.5d" ? Number.parseInt(canvas?.dataset.crossLayerRelationCount ?? "0", 10) : 0;
  return Object.freeze({
    labels_complete: labelsComplete,
    screen_reader_semantics: screenReaderSemantics,
    focus_order_valid: focusOrderValid,
    keyboard_workflow_valid: keyboardWorkflowValid,
    reduced_motion_honored: reducedMotionHonored,
    minimum_contrast: minimumContrast,
    zoom_layout_valid: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    viewport_width: Math.max(0, Math.min(window.innerWidth, 65535)),
    viewport_height: Math.max(0, Math.min(window.innerHeight, 65535)),
    viewer_mode: mode,
    renderer_backend: rendererBackend,
    viewer_item_count: viewerItems.length,
    viewer_relation_count: viewerRelationCount,
    viewer_cross_layer_relation_count: viewerCrossLayerRelationCount,
    viewer_keyboard_selection: viewerKeyboardSelection,
    webgl_verified: webglVerified,
  });
}

export function installAccessibilityProbe(eventsOn: (name: string, listener: (...data: unknown[]) => void) => unknown): void {
  eventsOn("layerdraw:accessibility-probe", (...data: unknown[]) => {
    const [id, profile] = data;
    if (typeof id !== "string" || typeof profile !== "object" || profile === null) return;
    void auditAccessibility(profile as AccessibilityProfile)
      .then((report) => nativeShell().SubmitAccessibilityReport(id, report));
  });
}

export async function signalAccessibilityProbeReady(): Promise<void> {
  await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
  await nativeShell().AccessibilityProbeReady();
}

export async function isPackagedProbeMode(): Promise<boolean> {
  return nativeShell().PackagedProbeMode();
}
