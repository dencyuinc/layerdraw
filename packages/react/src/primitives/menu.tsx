// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style DropdownMenu over Base UI, restyled with tokens: surface
// popup, 8px radius, menu shadow token, compact 13px rows, ghost hover.

import type { ComponentProps, ReactNode } from "react";
import { Menu as BaseMenu } from "@base-ui-components/react/menu";
import { cn } from "./cn.js";

export const DropdownMenuRoot = BaseMenu.Root;
export const DropdownMenuTrigger = BaseMenu.Trigger;
export const DropdownMenuPortal = BaseMenu.Portal;
export const DropdownMenuPositioner = BaseMenu.Positioner;

export type DropdownMenuPopupProps = ComponentProps<typeof BaseMenu.Popup>;

export function DropdownMenuPopup({ className, ...props }: DropdownMenuPopupProps): ReactNode {
  return (
    <BaseMenu.Popup
      {...props}
      className={cn(
        "min-w-44 rounded-lg border border-line-subtle bg-surface p-1 font-sans text-sm text-ink shadow-menu",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}

export type DropdownMenuItemProps = ComponentProps<typeof BaseMenu.Item>;

export function DropdownMenuItem({ className, ...props }: DropdownMenuItemProps): ReactNode {
  return (
    <BaseMenu.Item
      {...props}
      className={cn(
        "flex cursor-default items-center gap-2 rounded-sm px-2.5 py-1.5 text-sm text-ink outline-none " +
          "data-[highlighted]:bg-ghost-hover data-[disabled]:text-disabled",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}

export type DropdownMenuSeparatorProps = ComponentProps<typeof BaseMenu.Separator>;

export function DropdownMenuSeparator({ className, ...props }: DropdownMenuSeparatorProps): ReactNode {
  return <BaseMenu.Separator {...props} className={cn("my-1 h-px bg-line-subtle", typeof className === "string" ? className : undefined)} />;
}
