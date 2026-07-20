// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { EditorEdit } from "@layerdraw/composer";
import type { SymbolReadItem } from "@layerdraw/protocol/engine";
import type { SourceRange, StableAddress, SubjectKind } from "@layerdraw/protocol/semantic";
import {
  createElement,
  useDeferredValue,
  useEffect,
  useId,
  useMemo,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useEditorCommands } from "./provider.js";

export type NavigationAvailability = "available" | "read-only" | "denied" | "unavailable" | "partial";

/** Engine-owned identity and source data. React never parses LDL or derives an address. */
export interface NavigationItem extends Pick<SymbolReadItem, "address" | "display_name" | "kind" | "source_range"> {
  readonly availability?: NavigationAvailability;
  readonly description?: string;
}

export interface NavigationSelection {
  readonly address?: StableAddress;
  readonly lastSourceRange?: SourceRange;
  readonly stale: boolean;
}

const EMPTY_IDENTITY_REPLACEMENTS: ReadonlyMap<StableAddress, StableAddress> = new Map();

export function reconcileNavigationSelection(
  previous: NavigationSelection,
  items: readonly NavigationItem[],
  identityReplacements: ReadonlyMap<StableAddress, StableAddress> = EMPTY_IDENTITY_REPLACEMENTS,
): NavigationSelection {
  if (previous.address === undefined) return { stale: false };
  const replacement = identityReplacements.get(previous.address);
  const selectedAddress = replacement ?? previous.address;
  const exact = items.find((item) => item.address === selectedAddress);
  return exact === undefined
    ? previous.lastSourceRange === undefined
      ? { address: selectedAddress, stale: true }
      : { address: selectedAddress, lastSourceRange: previous.lastSourceRange, stale: true }
    : { address: exact.address, lastSourceRange: exact.source_range, stale: false };
}

export function filterNavigationItems(
  items: readonly NavigationItem[],
  query: string,
  kinds?: ReadonlySet<SubjectKind>,
): readonly NavigationItem[] {
  const folded = query.trim().toLocaleLowerCase();
  return items.filter((item) => (kinds === undefined || kinds.has(item.kind))
    && (folded === "" || item.display_name.toLocaleLowerCase().includes(folded)
      || item.address.toLocaleLowerCase().includes(folded)));
}

function sameSourceRange(left: SourceRange | undefined, right: SourceRange | undefined): boolean {
  return left === right || left !== undefined && right !== undefined
    && left.start_byte === right.start_byte && left.end_byte === right.end_byte
    && left.module_path === right.module_path && left.origin.kind === right.origin.kind
    && left.origin.pack_address === right.origin.pack_address;
}

export interface DocumentOutlineProps {
  /** Address-ordered structured results returned by Engine find_symbols. */
  readonly items: readonly NavigationItem[];
  readonly selection: NavigationSelection;
  readonly onSelectionChange: (selection: NavigationSelection) => void;
  readonly onNavigateSource?: (range: SourceRange, address?: StableAddress) => void;
  readonly kinds?: ReadonlySet<SubjectKind>;
  /** Engine semantic-diff before/after addresses; React never guesses rename identity. */
  readonly identityReplacements?: ReadonlyMap<StableAddress, StableAddress>;
  readonly maxVisibleItems?: number;
  readonly label?: string;
  readonly emptyLabel?: string;
}

export function DocumentOutline({
  items,
  selection,
  onSelectionChange,
  onNavigateSource,
  kinds,
  identityReplacements,
  maxVisibleItems = 200,
  label = "Document outline",
  emptyLabel = "No structured results. Diagnostics remain available.",
}: DocumentOutlineProps): ReactNode {
  const searchId = useId();
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const reconciled = useMemo(
    () => reconcileNavigationSelection(selection, items, identityReplacements),
    [selection, items, identityReplacements],
  );
  const matches = useMemo(() => filterNavigationItems(items, deferredQuery, kinds), [items, deferredQuery, kinds]);
  const visibleLimit = Number.isSafeInteger(maxVisibleItems) && maxVisibleItems > 0 ? maxVisibleItems : 200;
  const visible = matches.slice(0, visibleLimit);
  const visibleSelection = visible.some((item) => item.address === reconciled.address) ? reconciled.address : undefined;

  useEffect(() => {
    if (reconciled.address !== selection.address || reconciled.stale !== selection.stale
      || !sameSourceRange(reconciled.lastSourceRange, selection.lastSourceRange)) {
      onSelectionChange(reconciled);
    }
  }, [onSelectionChange, reconciled, selection]);

  const select = (item: NavigationItem): void => {
    onSelectionChange({ address: item.address, lastSourceRange: item.source_range, stale: false });
  };
  const handleKeys = (event: KeyboardEvent<HTMLUListElement>): void => {
    if (!["ArrowDown", "ArrowUp", "Home", "End", "Enter"].includes(event.key)) return;
    const current = visible.findIndex((item) => item.address === reconciled.address);
    const index = event.key === "Home" ? 0
      : event.key === "End" ? visible.length - 1
        : event.key === "ArrowDown" ? Math.min(visible.length - 1, current + 1)
          : event.key === "ArrowUp" ? Math.max(0, current < 0 ? 0 : current - 1)
            : current;
    const item = visible[index];
    if (item === undefined) return;
    if (event.key === "Enter") onNavigateSource?.(item.source_range, item.address);
    else select(item);
    event.preventDefault();
  };

  return createElement("section", { className: "ld-navigation", "aria-label": label },
    createElement("label", { htmlFor: searchId, className: "ld-navigation-search-label" }, "Search structure"),
    createElement("input", {
      id: searchId,
      type: "search",
      value: query,
      onChange: (event: ChangeEvent<HTMLInputElement>) => setQuery(event.currentTarget.value),
      "aria-controls": `${searchId}-results`,
    }),
    reconciled.stale && createElement("button", {
      type: "button",
      className: "ld-navigation-stale",
      onClick: () => reconciled.lastSourceRange && onNavigateSource?.(reconciled.lastSourceRange, reconciled.address),
    }, "Selected item changed or was deleted. Open its last source location."),
    createElement("ul", {
      id: `${searchId}-results`,
      role: "listbox",
      tabIndex: 0,
      "aria-label": `${label} results`,
      "aria-activedescendant": visibleSelection === undefined ? undefined : `${searchId}-${encodeURIComponent(visibleSelection)}`,
      onKeyDown: handleKeys,
    }, visible.map((item) => createElement("li", {
      id: `${searchId}-${encodeURIComponent(item.address)}`,
      key: item.address,
      role: "option",
      "aria-selected": item.address === reconciled.address,
      "aria-disabled": item.availability === "denied" || item.availability === "unavailable",
      "data-address": item.address,
      "data-kind": item.kind,
      "data-availability": item.availability ?? "available",
      onClick: () => select(item),
      onDoubleClick: () => onNavigateSource?.(item.source_range, item.address),
    }, createElement("span", { className: "ld-navigation-name" }, item.display_name || item.address),
    createElement("span", { className: "ld-navigation-kind" }, item.kind)))),
    matches.length === 0
      ? createElement("p", { role: "status" }, emptyLabel)
      : matches.length > visible.length && createElement("p", { role: "status" }, `${visible.length} of ${matches.length} results shown.`));
}

export interface SemanticInspectorField {
  readonly id: string;
  readonly label: string;
  /** Host-owned controlled draft. React treats it as opaque text. */
  readonly draft: string;
  readonly onDraftChange?: (draft: string) => void;
  /** Host builder returns a complete generated-contract Composer intent. */
  readonly buildEdit?: (draft: string) => EditorEdit;
  readonly availability?: NavigationAvailability;
  readonly description?: string;
}

export interface SourceNavigationTarget {
  readonly id: string;
  readonly label: string;
  readonly source_range: SourceRange;
  readonly address?: StableAddress;
  readonly availability?: NavigationAvailability;
  readonly description?: string;
}

export interface SourceNavigationListProps {
  /** Diagnostic/source targets resolved by Engine; no range or identity inference occurs here. */
  readonly targets: readonly SourceNavigationTarget[];
  readonly onNavigateSource: (range: SourceRange, address?: StableAddress) => void;
  readonly label?: string;
}

export function SourceNavigationList({
  targets,
  onNavigateSource,
  label = "Diagnostics and source locations",
}: SourceNavigationListProps): ReactNode {
  return createElement("ul", { className: "ld-source-navigation", "aria-label": label }, targets.map((target) => {
    const state = target.availability ?? "available";
    const disabled = state === "denied" || state === "unavailable";
    return createElement("li", { key: target.id, "data-availability": state }, createElement("button", {
      type: "button",
      title: target.description,
      disabled,
      "aria-disabled": disabled,
      onClick: disabled ? undefined : () => onNavigateSource(target.source_range, target.address),
    }, target.label));
  }));
}

export interface SemanticInspectorProps {
  readonly address?: StableAddress;
  readonly fields: readonly SemanticInspectorField[];
  readonly label?: string;
}

export function SemanticInspector({ address, fields, label = "Semantic inspector" }: SemanticInspectorProps): ReactNode {
  const commands = useEditorCommands();
  const fieldPrefix = useId();
  return createElement("section", { className: "ld-semantic-inspector", "aria-label": label, "data-address": address },
    fields.map((field) => {
      const state = field.availability ?? "available";
      const disabled = field.onDraftChange === undefined || field.buildEdit === undefined || state !== "available";
      const inputId = `${fieldPrefix}-${encodeURIComponent(field.id)}`;
      return createElement("div", { key: field.id, className: "ld-inspector-field", "data-availability": state },
        createElement("label", { className: "ld-inspector-label", htmlFor: inputId }, field.label),
        createElement("input", {
          id: inputId,
          value: field.draft,
          disabled,
          "aria-disabled": disabled,
          "aria-description": field.description,
          onChange: disabled ? undefined : (event: ChangeEvent<HTMLInputElement>) => field.onDraftChange?.(event.currentTarget.value),
        }),
        createElement("button", {
          type: "button",
          disabled,
          "aria-disabled": disabled,
          title: field.description,
          onClick: disabled ? undefined : () => {
            if (field.buildEdit !== undefined) void commands.preview(field.buildEdit(field.draft));
          },
        }, "Preview change"));
    }));
}
