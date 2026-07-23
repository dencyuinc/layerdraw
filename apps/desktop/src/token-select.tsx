// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { ReactNode } from "react";
import { SelectIcon, SelectItem, SelectItemText, SelectPopup, SelectPortal, SelectPositioner, SelectRoot, SelectTrigger, SelectValue } from "@layerdraw/react/primitives";

export interface TokenSelectOption { readonly value: string; readonly label: string }

/** shadcn/Base UI Select composition shared by desktop settings-style rows. */
export function tokenSelect(ariaLabel: string, value: string, options: readonly TokenSelectOption[], onChange: (value: string) => void): ReactNode {
  return (
    <SelectRoot
      value={value}
      onValueChange={(next: unknown) => { if (typeof next === "string") onChange(next); }}
      items={Object.fromEntries(options.map((option) => [option.value, option.label]))}
    >
      <SelectTrigger aria-label={ariaLabel}>
        <SelectValue />
        <SelectIcon>
          <svg viewBox="0 0 16 16" width={12} height={12} fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" aria-hidden={true}><path d="m4 6 4 4 4-4" /></svg>
        </SelectIcon>
      </SelectTrigger>
      <SelectPortal>
        <SelectPositioner sideOffset={4} className="ld-settings-select-positioner">
          <SelectPopup className="ld-settings-select-popup">
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                <SelectItemText>{option.label}</SelectItemText>
              </SelectItem>
            ))}
          </SelectPopup>
        </SelectPositioner>
      </SelectPortal>
    </SelectRoot>
  );
}
