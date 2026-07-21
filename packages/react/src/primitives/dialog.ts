// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style Dialog over Base UI, restyled with tokens: scrim from the
// token set, surface popup with 8px radius, dialog shadow token, flat borders.

import { createElement, type ComponentProps, type ReactNode } from "react";
import { Dialog as BaseDialog } from "@base-ui-components/react/dialog";
import { cn } from "./cn.js";

export const DialogRoot = BaseDialog.Root;
export const DialogTrigger = BaseDialog.Trigger;
export const DialogPortal = BaseDialog.Portal;
export const DialogTitle = BaseDialog.Title;
export const DialogDescription = BaseDialog.Description;
export const DialogClose = BaseDialog.Close;

export type DialogBackdropProps = ComponentProps<typeof BaseDialog.Backdrop>;

export function DialogBackdrop({ className, ...props }: DialogBackdropProps): ReactNode {
  return createElement(BaseDialog.Backdrop, {
    ...props,
    className: cn("fixed inset-0 bg-scrim", typeof className === "string" ? className : undefined),
  });
}

export type DialogPopupProps = ComponentProps<typeof BaseDialog.Popup>;

export function DialogPopup({ className, ...props }: DialogPopupProps): ReactNode {
  return createElement(BaseDialog.Popup, {
    ...props,
    className: cn(
      "fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[min(28rem,calc(100vw-2rem))] " +
        "rounded-lg border border-line-subtle bg-surface p-4 font-sans text-sm text-ink shadow-dialog",
      typeof className === "string" ? className : undefined,
    ),
  });
}
