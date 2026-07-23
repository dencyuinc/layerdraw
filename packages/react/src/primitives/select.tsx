// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style Select over Base UI, restyled with tokens: compact trigger
// matching Input, surface popup with menu shadow, 13px rows.

import type { ComponentProps, ReactNode } from "react";
import { Select as BaseSelect } from "@base-ui-components/react/select";
import { cn } from "./cn.js";

export const SelectRoot = BaseSelect.Root;
export const SelectValue = BaseSelect.Value;
export const SelectIcon = BaseSelect.Icon;
export const SelectPortal = BaseSelect.Portal;
export const SelectPositioner = BaseSelect.Positioner;
export const SelectItemText = BaseSelect.ItemText;
export const SelectItemIndicator = BaseSelect.ItemIndicator;

export type SelectTriggerProps = ComponentProps<typeof BaseSelect.Trigger>;

export function SelectTrigger({ className, ...props }: SelectTriggerProps): ReactNode {
  return (
    <BaseSelect.Trigger
      {...props}
      className={cn(
        "inline-flex h-8 min-w-0 cursor-pointer items-center justify-between gap-2 rounded-sm border border-line " +
          "bg-surface px-2 font-sans text-sm text-ink transition-colors hover:border-line-strong " +
          "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus " +
          "disabled:cursor-not-allowed disabled:opacity-50",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}

export type SelectPopupProps = ComponentProps<typeof BaseSelect.Popup>;

export function SelectPopup({ className, ...props }: SelectPopupProps): ReactNode {
  return (
    <BaseSelect.Popup
      {...props}
      className={cn(
        "rounded-lg border border-line-subtle bg-surface p-1 font-sans text-sm text-ink shadow-menu",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}

export type SelectItemProps = ComponentProps<typeof BaseSelect.Item>;

export function SelectItem({ className, ...props }: SelectItemProps): ReactNode {
  return (
    <BaseSelect.Item
      {...props}
      className={cn(
        "flex cursor-default items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm text-ink outline-none " +
          "data-[highlighted]:bg-ghost-hover data-[disabled]:text-disabled",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}
