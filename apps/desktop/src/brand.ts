// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { createElement, type ReactNode } from "react";
import { layerdrawIconDataUri } from "./brand-assets.js";

export interface LayerDrawBrandProps {
  /** Accessible name (the localized application name). */
  readonly title: string;
}

/**
 * Canonical LayerDraw brand icon (embedded from brand/png via the generated
 * brand-assets module; run `pnpm --filter @layerdraw/desktop generate` after a
 * brand asset change).
 */
export function LayerDrawIcon({ title, size = 22 }: LayerDrawBrandProps & { readonly size?: number }): ReactNode {
  return createElement("img", { className: "ld-brand-icon", src: layerdrawIconDataUri, alt: title, width: size, height: size });
}

/** Brand row for chrome: canonical icon + wordmark text in brand ink. */
export function LayerDrawWordmark({ title }: LayerDrawBrandProps): ReactNode {
  return createElement("span", { className: "ld-wordmark", role: "img", "aria-label": title },
    createElement("img", { className: "ld-brand-icon", src: layerdrawIconDataUri, alt: "", "aria-hidden": true, width: 22, height: 22 }),
    createElement("span", { className: "ld-wordmark-text" }, "LayerDraw"));
}
