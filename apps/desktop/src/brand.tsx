// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ReactNode } from "react";
import { layerdrawIconDataUri, layerdrawLogoOnDarkDataUri, layerdrawLogoOnLightDataUri } from "./brand-assets.js";

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
  return <img className="ld-brand-icon" src={layerdrawIconDataUri} alt={title} width={size} height={size} />;
}

/**
 * Canonical logo lockup (icon + wordmark as authored in brand/, including the
 * wordmark typeface and ink baked into the SVG). The light and dark lockups
 * both render; the active theme picks one via CSS.
 */
export function LayerDrawWordmark({ title }: LayerDrawBrandProps): ReactNode {
  return (
    <span className="ld-wordmark" role="img" aria-label={title}>
      <img className="ld-wordmark-logo ld-wordmark-logo-light" src={layerdrawLogoOnLightDataUri} alt="" aria-hidden={true} height={22} />
      <img className="ld-wordmark-logo ld-wordmark-logo-dark" src={layerdrawLogoOnDarkDataUri} alt="" aria-hidden={true} height={22} />
    </span>
  );
}
