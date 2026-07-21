// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { createElement, type ReactNode } from "react";

export interface LayerDrawWordmarkProps {
  /** Accessible name for the wordmark (the localized application name). */
  readonly title: string;
}

/**
 * Lightweight LayerDraw wordmark for chrome: a simplified stacked-layer mark in
 * brand violet plus the product wordmark. Colors resolve from tokens so it
 * adapts to every theme. The full gradient logo asset is reserved for marketing
 * surfaces; product chrome uses this flat token-driven mark.
 */
export function LayerDrawWordmark({ title }: LayerDrawWordmarkProps): ReactNode {
  return createElement("span", { className: "ld-wordmark", role: "img", "aria-label": title },
    createElement("svg", { className: "ld-wordmark-mark", viewBox: "0 0 24 24", width: 22, height: 22, "aria-hidden": true, focusable: false },
      createElement("path", { d: "M12 3 21 8l-9 5-9-5 9-5Z", className: "ld-wordmark-layer ld-wordmark-layer-top" }),
      createElement("path", { d: "M3 12l9 5 9-5", className: "ld-wordmark-layer ld-wordmark-layer-mid", fill: "none" }),
      createElement("path", { d: "M3 16l9 5 9-5", className: "ld-wordmark-layer ld-wordmark-layer-bottom", fill: "none" })),
    createElement("span", { className: "ld-wordmark-text" }, "LayerDraw"));
}
