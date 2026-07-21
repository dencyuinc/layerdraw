// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

// Lightweight message-catalog i18n for LayerDraw presentation surfaces.
//
// A custom, dependency-free translator is used instead of a library: the string
// surface is small, the catalog is plain data (adding a locale never touches a
// component), interpolation and Intl formatting are a few lines, and avoiding a
// runtime dependency keeps the shared package's delivery-bundle closure minimal
// (see brand README: token/string sources are generated/owned here, not vendored).

import {
  createContext,
  createElement,
  useContext,
  useMemo,
  type ReactNode,
} from "react";

export type Locale = "en" | "ja";
export type MessageArgs = Readonly<Record<string, string | number>>;
export type MessageCatalog = Readonly<Record<string, string>>;
export type LocaleCatalogs = Readonly<Record<Locale, MessageCatalog>>;

/** Supported locales at launch. `en` is the canonical base. */
export const SUPPORTED_LOCALES: readonly Locale[] = ["en", "ja"];
export const BASE_LOCALE: Locale = "en";

export function isLocale(value: string): value is Locale {
  return (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

/** Resolve an OS/browser locale tag (e.g. `ja-JP`) to a supported locale. */
export function resolveLocale(requested: string | undefined): Locale {
  if (requested === undefined || requested === "") return BASE_LOCALE;
  if (isLocale(requested)) return requested;
  const base = requested.split("-")[0]?.toLowerCase();
  return base !== undefined && isLocale(base) ? base : BASE_LOCALE;
}

/** Replace `{name}` placeholders with structured arguments. */
export function interpolate(template: string, args?: MessageArgs): string {
  if (args === undefined) return template;
  return template.replace(/\{(\w+)\}/gu, (whole, key: string) => {
    const value = args[key];
    return value === undefined ? whole : String(value);
  });
}

/** Layer surface-specific catalogs on top of a shared base without code changes. */
export function mergeCatalogs(base: LocaleCatalogs, overlay: Partial<LocaleCatalogs>): LocaleCatalogs {
  const result = {} as Record<Locale, MessageCatalog>;
  for (const locale of SUPPORTED_LOCALES) {
    result[locale] = { ...base[locale], ...(overlay[locale] ?? {}) };
  }
  return result;
}

const RELATIVE_UNITS: readonly (readonly [Intl.RelativeTimeFormatUnit, number])[] = [
  ["year", 365 * 24 * 60 * 60 * 1000],
  ["month", 30 * 24 * 60 * 60 * 1000],
  ["week", 7 * 24 * 60 * 60 * 1000],
  ["day", 24 * 60 * 60 * 1000],
  ["hour", 60 * 60 * 1000],
  ["minute", 60 * 1000],
  ["second", 1000],
];

function toDate(value: Date | string | number): Date {
  return value instanceof Date ? value : new Date(value);
}

export interface Translator {
  readonly locale: Locale;
  /** Resolve a catalog key, falling back to the base locale then the key itself. */
  t(key: string, args?: MessageArgs): string;
  /**
   * Present a stable diagnostic code as a translated reason plus the code in
   * parentheses (never a reason-less code; never the raw code alone).
   */
  error(code: string, args?: MessageArgs): string;
  formatDate(value: Date | string | number, options?: Intl.DateTimeFormatOptions): string;
  formatNumber(value: number, options?: Intl.NumberFormatOptions): string;
  formatRelativeTime(value: Date | string | number, now?: Date): string;
}

export function createTranslator(locale: Locale, catalogs: LocaleCatalogs): Translator {
  const active = catalogs[locale] ?? {};
  const base = catalogs[BASE_LOCALE] ?? {};
  const lookup = (key: string): string | undefined => active[key] ?? base[key];
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

  return {
    locale,
    t(key, args) {
      const template = lookup(key);
      return template === undefined ? key : interpolate(template, args);
    },
    error(code, args) {
      const reason = lookup(`error.${code}`) ?? lookup("error.generic") ?? "Something went wrong.";
      return `${interpolate(reason, args)} (${code})`;
    },
    formatDate(value, options) {
      return new Intl.DateTimeFormat(locale, options).format(toDate(value));
    },
    formatNumber(value, options) {
      return new Intl.NumberFormat(locale, options).format(value);
    },
    formatRelativeTime(value, now) {
      const delta = toDate(value).getTime() - (now ?? new Date()).getTime();
      for (const [unit, ms] of RELATIVE_UNITS) {
        if (Math.abs(delta) >= ms || unit === "second") {
          return relative.format(Math.round(delta / ms), unit);
        }
      }
      return relative.format(0, "second");
    },
  };
}

const I18nContext = createContext<Translator | undefined>(undefined);

export interface I18nProviderProps {
  readonly locale: Locale;
  readonly catalogs: LocaleCatalogs;
  readonly children?: ReactNode;
}

/** Provide the active translator; switching `locale` re-renders the subtree. */
export function I18nProvider({ locale, catalogs, children }: I18nProviderProps): ReactNode {
  const translator = useMemo(() => createTranslator(locale, catalogs), [locale, catalogs]);
  return createElement(I18nContext.Provider, { value: translator }, children);
}

export function useI18n(): Translator {
  const value = useContext(I18nContext);
  if (value === undefined) throw new Error("useI18n must be used within an I18nProvider");
  return value;
}

/**
 * Shared shell strings for the navigable Desktop/Browser shell (hub, workspace
 * frame, lifecycle, and the closed diagnostic-code reasons). `en` is canonical;
 * surface-specific strings layer on top with {@link mergeCatalogs}.
 */
export const baseShellCatalogs: LocaleCatalogs = {
  en: {
    "app.name": "LayerDraw",
    "nav.projects": "Projects",
    "nav.library": "Library",
    "nav.section": "Primary",

    "hub.title": "Projects",
    "hub.subtitle": "Open an existing project or start a new one.",
    "hub.action.new": "New project",
    "hub.action.open": "Open project",
    "hub.action.creating": "Creating…",
    "hub.action.opening": "Opening…",
    "hub.actions.label": "Project actions",
    "hub.recent.title": "Recent",
    "hub.recent.empty": "No recent projects yet. Create or open one to get started.",
    "hub.recent.missing": "Files missing",
    "hub.recent.opened": "Opened {when}",
    "hub.templates.title": "Start from a template",
    "hub.error.title": "The project couldn't be opened",
    "hub.error.showDetails": "Show details",
    "hub.error.hideDetails": "Hide details",

    "workspace.back": "Projects",
    "workspace.mode.label": "Editor mode",
    "workspace.mode.structure": "Structure",
    "workspace.mode.views": "Views",
    "workspace.views": "Views",
    "workspace.canvas": "Canvas",
    "workspace.inspector": "Project status",

    "status.starting": "Starting LayerDraw…",
    "status.closing": "LayerDraw is closing…",
    "status.unavailable": "This action is unavailable.",
    "recovery.title": "LayerDraw needs recovery before this project can open.",
    "recovery.action": "Review recovery options",

    "error.generic": "The project could not be opened.",
    "error.desktop.project_missing": "The project files could not be found at their last known location.",
    "error.desktop.permission_denied": "LayerDraw does not have permission to read the project files.",
    "error.desktop.project_conflict": "The project files on disk no longer match this LayerDraw project. Review or re-import the project.",
    "error.desktop.recovery_required": "The project needs recovery before it can open.",
    "error.desktop.reconcile_pending": "The project has pending external changes that must be reviewed first.",
    "error.desktop.adapter_unavailable": "The project could not be loaded by the LayerDraw backend.",
    "error.desktop.reconnect_failed": "The LayerDraw backend is not available.",
    "error.desktop.backend_panic": "The LayerDraw backend failed unexpectedly.",
    "error.desktop.error.lifecycle_failed": "Recovery options could not be opened.",
    "error.desktop.error.selection_failed": "The selected view could not be opened.",
    "error.desktop.error.viewer_rejected": "The view update was rejected.",
    "error.desktop.error.viewer_failed": "The view could not be displayed.",
    "error.desktop.error.context_mismatch": "A stale view update was ignored.",
  },
  ja: {
    "app.name": "LayerDraw",
    "nav.projects": "プロジェクト",
    "nav.library": "ライブラリ",
    "nav.section": "メイン",

    "hub.title": "プロジェクト",
    "hub.subtitle": "既存のプロジェクトを開くか、新規に作成します。",
    "hub.action.new": "新規プロジェクト",
    "hub.action.open": "プロジェクトを開く",
    "hub.action.creating": "作成中…",
    "hub.action.opening": "開いています…",
    "hub.actions.label": "プロジェクト操作",
    "hub.recent.title": "最近使用したプロジェクト",
    "hub.recent.empty": "最近使用したプロジェクトはありません。作成するか開いて始めましょう。",
    "hub.recent.missing": "ファイルが見つかりません",
    "hub.recent.opened": "最終使用: {when}",
    "hub.templates.title": "テンプレートから開始",
    "hub.error.title": "プロジェクトを開けませんでした",
    "hub.error.showDetails": "詳細を表示",
    "hub.error.hideDetails": "詳細を隠す",

    "workspace.back": "プロジェクト",
    "workspace.mode.label": "エディタモード",
    "workspace.mode.structure": "ストラクチャ",
    "workspace.mode.views": "ビュー",
    "workspace.views": "ビュー",
    "workspace.canvas": "キャンバス",
    "workspace.inspector": "プロジェクト状態",

    "status.starting": "LayerDraw を起動しています…",
    "status.closing": "LayerDraw を終了しています…",
    "status.unavailable": "この操作は利用できません。",
    "recovery.title": "このプロジェクトを開く前に復旧が必要です。",
    "recovery.action": "復旧オプションを確認",

    "error.generic": "プロジェクトを開けませんでした。",
    "error.desktop.project_missing": "プロジェクトファイルが以前の場所に見つかりませんでした。",
    "error.desktop.permission_denied": "プロジェクトファイルを読み取る権限がありません。",
    "error.desktop.project_conflict": "ディスク上のプロジェクトファイルがこの LayerDraw プロジェクトと一致しません。確認するか、再インポートしてください。",
    "error.desktop.recovery_required": "このプロジェクトを開く前に復旧が必要です。",
    "error.desktop.reconcile_pending": "先に確認が必要な外部変更が保留されています。",
    "error.desktop.adapter_unavailable": "LayerDraw バックエンドがプロジェクトを読み込めませんでした。",
    "error.desktop.reconnect_failed": "LayerDraw バックエンドに接続できません。",
    "error.desktop.backend_panic": "LayerDraw バックエンドが予期せず停止しました。",
    "error.desktop.error.lifecycle_failed": "復旧オプションを開けませんでした。",
    "error.desktop.error.selection_failed": "選択したビューを開けませんでした。",
    "error.desktop.error.viewer_rejected": "ビューの更新が拒否されました。",
    "error.desktop.error.viewer_failed": "ビューを表示できませんでした。",
    "error.desktop.error.context_mismatch": "古いビュー更新は無視されました。",
  },
};

/** Assert that every non-base locale defines exactly the base locale's keys. */
export function findCatalogGaps(catalogs: LocaleCatalogs): Readonly<Record<Locale, readonly string[]>> {
  const baseKeys = Object.keys(catalogs[BASE_LOCALE] ?? {});
  const gaps = {} as Record<Locale, string[]>;
  for (const locale of SUPPORTED_LOCALES) {
    if (locale === BASE_LOCALE) {
      gaps[locale] = [];
      continue;
    }
    const localeKeys = new Set(Object.keys(catalogs[locale] ?? {}));
    gaps[locale] = baseKeys.filter((key) => !localeKeys.has(key));
  }
  return gaps;
}
