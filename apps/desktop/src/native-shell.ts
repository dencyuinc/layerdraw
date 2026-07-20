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

let focusPresentationInstalled = false;

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
  const focusFailures: string[] = [];
  const focusOrderValid = controls.map((node, index) => {
    node.focus();
	const previous = controls[index - 1];
	const followsPrevious = node.tabIndex === 0 && (index === 0 || (previous !== undefined && Boolean(previous.compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING)));
	const active = document.activeElement === node;
	if (active && node.dataset.focusVisible !== "true") {
		previous?.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: node }));
		node.dispatchEvent(new FocusEvent("focusin", { bubbles: true, relatedTarget: previous ?? null }));
	}
	const outlineVisible = getComputedStyle(node).outlineStyle !== "none";
	const focusRect = node.matches(".ld-desktop-viewer-item") ? node.querySelector<SVGElement>("rect") : null;
	const strokeVisible = focusRect !== null && Number.parseFloat(getComputedStyle(focusRect).strokeWidth) >= 3;
    const visible = outlineVisible || strokeVisible;
    if (!active || !followsPrevious || !visible) focusFailures.push(`${node.tagName}.${node.className}:${node.textContent?.trim()}:active=${active},ordered=${followsPrevious},visible=${visible}`);
    return active && followsPrevious && visible;
  }).every(Boolean);
  const keyboardWorkflowValid = !profile.keyboard_only || await invokeSettings();
  document.documentElement.dataset.reducedMotion = String(Boolean(profile.reduced_motion));
  const reducedMotionHonored = !profile.reduced_motion || [...document.querySelectorAll<HTMLElement>(".ld-desktop-shell, .ld-desktop-shell *, .ld-desktop-shell *::before, .ld-desktop-shell *::after")]
    .every((node) => {
      const style = getComputedStyle(node);
      return Number.parseFloat(style.animationDuration) <= .001 && Number.parseFloat(style.transitionDuration) <= .001;
    });
  type RGBA = readonly [number, number, number, number];
  const rgba = (value: string): RGBA => {
    if (value === "transparent") return [0, 0, 0, 0];
    const values = (value.match(/[\d.]+/g) ?? []).map(Number);
    const channels = value.startsWith("color(srgb") ? values.slice(0, 3).map((channel) => channel * 255) : values.slice(0, 3);
    return [channels[0] ?? 0, channels[1] ?? 0, channels[2] ?? 0, values[3] ?? 1];
  };
  const composite = (over: RGBA, under: RGBA): RGBA => {
    const alpha = over[3] + under[3] * (1 - over[3]);
    if (alpha === 0) return [0, 0, 0, 0];
    const channel = (index: 0 | 1 | 2): number => (over[index] * over[3] + under[index] * under[3] * (1 - over[3])) / alpha;
    return [channel(0), channel(1), channel(2), alpha];
  };
  const luminance = (value: RGBA): number => value.slice(0, 3).map((channel) => channel / 255)
    .map((channel) => channel <= .03928 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4)
    .reduce((sum, channel, index) => sum + channel * ([.2126, .7152, .0722][index] ?? 0), 0);
  const background = (node: Element): RGBA => {
    const layers: RGBA[] = [];
    let current: Element | null = node;
    while (current !== null) {
      layers.push(rgba(getComputedStyle(current).backgroundColor));
      current = current.parentElement;
    }
    return layers.reverse().reduce<RGBA>((result, layer) => composite(layer, result), [255, 255, 255, 1]);
  };
  const contrastNodes = [...document.querySelectorAll<HTMLElement>(".ld-desktop-shell h1, .ld-desktop-shell h2, .ld-desktop-shell h3, .ld-desktop-shell p, .ld-desktop-shell button, .ld-desktop-shell dt, .ld-desktop-shell dd, .ld-desktop-shell small, .ld-desktop-shell span")]
    .filter((node) => node.getClientRects().length > 0 && (node.textContent?.trim().length ?? 0) > 0 && node.closest(".ld-desktop-visually-hidden,.ld-visually-hidden") === null);
  const ratios = contrastNodes.map((node) => {
      const nodeBackground = background(node);
      const foreground = luminance(composite(rgba(getComputedStyle(node).color), nodeBackground));
      const behind = luminance(nodeBackground);
      return (Math.max(foreground, behind) + .05) / (Math.min(foreground, behind) + .05);
    });
  const minimumContrast = ratios.length === 0 ? 0 : Math.min(...ratios);
  const minimumNode = contrastNodes[ratios.indexOf(minimumContrast)];
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
    focus_order_failures: focusFailures.join("|"),
    keyboard_workflow_valid: keyboardWorkflowValid,
    reduced_motion_honored: reducedMotionHonored,
    minimum_contrast: minimumContrast,
    minimum_contrast_target: minimumNode === undefined ? "" : `${minimumNode.tagName}.${minimumNode.className}:${minimumNode.textContent?.trim()}:foreground=${getComputedStyle(minimumNode).color}:background=${background(minimumNode).join(",")}`,
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
  if (!focusPresentationInstalled) {
    focusPresentationInstalled = true;
    document.addEventListener("focusin", (event) => {
      const target = event.target;
      if (target instanceof HTMLElement || target instanceof SVGElement) target.dataset.focusVisible = "true";
    });
    document.addEventListener("focusout", (event) => {
      const target = event.target;
      if (target instanceof HTMLElement || target instanceof SVGElement) delete target.dataset.focusVisible;
    });
  }
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
