// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// shadcn/ui-style Tabs over Base UI, restyled as the LayerDraw segmented
// control: subtle track, surface-raised active segment, token radii, flat.

import type { ComponentProps, ReactNode } from "react";
import { Tabs as BaseTabs } from "@base-ui-components/react/tabs";
import { cn } from "./cn.js";

export const TabsRoot = BaseTabs.Root;
export const TabsPanel = BaseTabs.Panel;

export type TabsListProps = ComponentProps<typeof BaseTabs.List>;

export function TabsList({ className, ...props }: TabsListProps): ReactNode {
  return (
    <BaseTabs.List
      {...props}
      className={cn(
        "inline-flex items-center gap-0.5 rounded-md border border-line-subtle bg-subtle p-0.5",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}

export type TabProps = ComponentProps<typeof BaseTabs.Tab>;

export function Tab({ className, ...props }: TabProps): ReactNode {
  return (
    <BaseTabs.Tab
      {...props}
      className={cn(
        "inline-flex h-[26px] items-center rounded-sm border-0 bg-transparent px-3 font-sans text-xs font-medium text-secondary " +
          "cursor-pointer transition-colors hover:text-ink " +
          "aria-selected:bg-surface aria-selected:text-ink aria-selected:shadow-menu " +
          "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus",
        typeof className === "string" ? className : undefined,
      )}
    />
  );
}
