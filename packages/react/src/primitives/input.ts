// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style Input over the Base UI primitive, restyled with tokens:
// 4px radius (compact control), 1px token border, surface background.

import { createElement, type ComponentProps, type ReactNode } from "react";
import { Input as BaseInput } from "@base-ui-components/react/input";
import { cn } from "./cn.js";

const inputClasses =
  "h-8 w-full min-w-0 rounded-sm border border-line bg-surface px-2 font-sans text-sm text-ink " +
  "placeholder:text-muted transition-colors " +
  "hover:border-line-strong focus:border-selected focus:outline-2 focus:outline-offset-1 focus:outline-focus " +
  "disabled:cursor-not-allowed disabled:opacity-50";

export type InputProps = ComponentProps<typeof BaseInput>;

export function Input({ className, ...props }: InputProps): ReactNode {
  return createElement(BaseInput, {
    ...props,
    className: cn(inputClasses, typeof className === "string" ? className : undefined),
  });
}
