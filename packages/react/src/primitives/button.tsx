// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style Button copied in over the Base UI primitive and fully
// restyled with LayerDraw tokens: flat elevation, token radii (6px), compact
// workbench density. No default shadcn visual language survives.

import type { ComponentProps, ReactNode } from "react";
import { Button as BaseButton } from "@base-ui-components/react/button";
import { cn } from "./cn.js";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "danger-quiet";
export type ButtonSize = "compact" | "default";

const base =
  "inline-flex items-center justify-center gap-2 rounded-md border font-sans text-sm font-medium " +
  "transition-colors cursor-pointer select-none " +
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus " +
  "disabled:cursor-not-allowed disabled:opacity-50";

const variants: Readonly<Record<ButtonVariant, string>> = {
  primary: "border-transparent bg-primary text-primary-fg hover:bg-primary-hover active:bg-primary-active",
  secondary: "border-secondary-border bg-secondary-bg text-ink hover:bg-secondary-hover",
  ghost: "border-transparent bg-transparent text-ink hover:bg-ghost-hover active:bg-ghost-active",
  danger: "border-transparent bg-danger text-primary-fg hover:opacity-90",
  "danger-quiet": "border-danger-border bg-transparent text-danger-fg hover:bg-danger-bg",
};

const sizes: Readonly<Record<ButtonSize, string>> = {
  compact: "h-8 px-3",
  default: "h-9 px-3.5",
};

export type ButtonProps = ComponentProps<typeof BaseButton> & {
  readonly variant?: ButtonVariant;
  readonly size?: ButtonSize;
};

export function Button({ variant = "secondary", size = "compact", className, ...props }: ButtonProps): ReactNode {
  return <BaseButton {...props} className={cn(base, variants[variant], sizes[size], typeof className === "string" ? className : undefined)} />;
}
