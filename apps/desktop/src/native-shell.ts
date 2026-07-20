// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export interface AccessibilityProfile {
  readonly keyboard_only?: boolean;
  readonly reduced_motion?: boolean;
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
  CommandStatus(): Promise<ShellResult<readonly CommandStatus[]>>;
  InvokeCommand(input: Readonly<{id: string; source: string; status_generation: string}>): Promise<ShellResult<CommandStatus>>;
  AccessibilityProbeReady(): Promise<void>;
  SubmitAccessibilityReport(id: string, report: Readonly<Record<string, boolean | number>>): Promise<void>;
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

export async function auditAccessibility(profile: AccessibilityProfile): Promise<Readonly<Record<string, boolean | number>>> {
  const controls = [...document.querySelectorAll<HTMLElement>("button,input,select,textarea,a[href],[tabindex]")]
    .filter((node) => node.tabIndex >= 0 && !node.hasAttribute("disabled"));
  const labelsComplete = controls.every((node) =>
    (node.textContent?.trim().length ?? 0) > 0 || node.hasAttribute("aria-label") || node.hasAttribute("aria-labelledby"));
  const focusOrderValid = controls.every((node, index) => {
    node.focus();
	const previous = controls[index - 1];
	const followsPrevious = index === 0 || (previous !== undefined && Boolean(previous.compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING));
	const outlineVisible = getComputedStyle(node).outlineStyle !== "none";
	const focusRect = node.matches(".ld-desktop-viewer-item") ? node.querySelector<SVGElement>("rect") : null;
	const strokeVisible = focusRect !== null && Number.parseFloat(getComputedStyle(focusRect).strokeWidth) >= 3;
    return document.activeElement === node && followsPrevious && (outlineVisible || strokeVisible);
  });
  const keyboardWorkflowValid = !profile.keyboard_only || await invokeSettings();
  document.documentElement.dataset.reducedMotion = String(Boolean(profile.reduced_motion));
  const motionStyle = getComputedStyle(document.querySelector(".ld-desktop-shell") ?? document.documentElement);
  const reducedMotionHonored = !profile.reduced_motion ||
    (Number.parseFloat(motionStyle.animationDuration) <= .001 && Number.parseFloat(motionStyle.transitionDuration) <= .001);
  const rgb = (value: string): number[] => (value.match(/[\d.]+/g) ?? []).slice(0, 3).map(Number);
  const luminance = (value: string): number => rgb(value).map((channel) => channel / 255)
    .map((channel) => channel <= .03928 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4)
    .reduce((sum, channel, index) => sum + channel * ([.2126, .7152, .0722][index] ?? 0), 0);
  const style = getComputedStyle(document.querySelector(".ld-desktop-shell") ?? document.documentElement);
  const lighter = Math.max(luminance(style.color), luminance(style.backgroundColor));
  const darker = Math.min(luminance(style.color), luminance(style.backgroundColor));
  return Object.freeze({
    labels_complete: labelsComplete,
    focus_order_valid: focusOrderValid,
    keyboard_workflow_valid: keyboardWorkflowValid,
    reduced_motion_honored: reducedMotionHonored,
    minimum_contrast: (lighter + .05) / (darker + .05),
    zoom_layout_valid: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
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
