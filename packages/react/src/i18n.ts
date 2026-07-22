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

/** Access the active translator without requiring a provider (undefined if absent). */
export function useOptionalI18n(): Translator | undefined {
  return useContext(I18nContext);
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
    "hub.recent.untitled": "(Untitled project)",
    "hub.templates.title": "Start from a template",
    "hub.templates.blank": "Blank project",
    "hub.templates.hint": "Connect a Library source to see templates here.",
    "hub.error.title": "The project couldn't be opened",
    "hub.error.showDetails": "Show details",
    "hub.error.hideDetails": "Hide details",
    "hub.error.dismiss": "Dismiss",

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

    "workspace.status.conflict": "Needs review",
    "workspace.status.reconcile_pending": "External changes pending",
    "workspace.status.denied": "Access denied",
    "settings.title": "Settings",
    "settings.close": "Close settings",
    "settings.group.application": "Application",
    "settings.group.project": "Project",
    "settings.nav.general": "General",
    "settings.nav.mcpDefaults": "AI access defaults",
    "settings.nav.agentAccess": "AI permissions",
    "settings.general.description": "Application-wide preferences. Nothing here depends on a project.",
    "settings.language.label": "Language",
    "settings.language.hint": "Menus, UI, and diagnostic messages",
    "settings.language.system": "System",
    "settings.theme.label": "Theme",
    "settings.theme.hint": "Light, dark, or follow the OS",
    "settings.theme.system": "System",
    "settings.theme.light": "Light",
    "settings.theme.dark": "Dark",
    "settings.zoom.label": "Interface zoom",
    "settings.zoom.hint": "Scales the whole window (50–300%)",
    "settings.mcpDefaults.title": "AI access defaults",
    "settings.mcpDefaults.description": "AI clients such as Claude connect to LayerDraw over MCP. These are the connection defaults; per-project settings always win.",
    "settings.mcp.enable.label": "Allow AI clients on this device to connect",
    "settings.mcp.enable.hint": "While off, AI has no access to LayerDraw at all",
    "settings.mcp.restart.hint": "Disconnects every AI session and starts a fresh host generation",
    "settings.mcp.status.on": "On",
    "settings.mcp.status.off": "Off",
    "settings.agentAccess.title": "AI permissions",
    "settings.agentAccess.description": "Per-agent permissions for AI connected to this project.",
    "settings.agentAccess.empty": "No AI clients have connected to this project.",
    "settings.agentAccess.revoke": "Revoke connection",
    "settings.agentAccess.delete": "Delete connection record",
    "settings.agentAccess.expires": "Expires {when}",
    "settings.agentAccess.scopes": "Allowed",
    "settings.agentAccess.enableFirst": "Turn on AI connections in \"AI access defaults\" first.",
    "settings.agentAccess.config": "Client configuration",
    "settings.agentAccess.config.show": "Show configuration",
    "settings.agentAccess.copy": "Copy",
    "settings.agentAccess.copied": "Copied",
    "settings.agentAccess.configHint": "Paste into your MCP client configuration (for Claude Desktop: claude_desktop_config.json). LayerDraw Desktop must be running.",
    "settings.agentAccess.connected": "Connected AI",
    "settings.agentAccess.connect.title": "Connect an AI client",
    "settings.agentAccess.targets": "Targets",
    "settings.agentAccess.expiry": "Expires in",
    "settings.agentAccess.expiry.1h": "1 hour",
    "settings.agentAccess.expiry.8h": "8 hours",
    "settings.agentAccess.expiry.7d": "7 days",
    "settings.capability.graph_write": "Entities & relations",
    "settings.capability.query_write": "Queries",
    "settings.capability.view_write": "View definitions",
    "settings.capability.schema_write": "Schema (type definitions)",
    "settings.capability.asset_write": "Assets",
    "settings.capability.package_manage": "Pack management",
    "settings.saveFailed": "The setting could not be saved. Try again.",
    "workspace.pane.layers": "Layers",
    "workspace.persistence.clean": "Saved",
    "workspace.persistence.preview_pending": "Previewing",
    "workspace.persistence.ephemeral": "Not saved",
    "workspace.persistence.durable_pending": "Saving…",
    "workspace.persistence.reconcile_pending": "Sync pending",
    "workspace.statusbar.ok": "No validation issues",
    "workspace.statusbar.attention": "Needs attention",
    "workspace.statusbar.view": "View: {name}",
    "workspace.statusbar.entities": "{count} entities",
    "workspace.statusbar.relations": "{count} relations",
    "workspace.statusbar.undo": "Undo ⌘Z · Redo ⇧⌘Z",
    "workspace.kind.view": "View",
    "workspace.structure.empty": "Structure editing arrives with the authoring milestone.",
    "workspace.empty.select": "Select a view to begin.",
    "workspace.empty.view": "This view is empty.",
    "workspace.attention": "The view needs attention.",

    "inspector.section.editing": "Editing",
    "inspector.section.storage": "External storage",
    "inspector.section.library": "Library",
    "inspector.section.review": "Review",
    "inspector.section.mcp": "AI access (MCP)",

    "editor.commands": "Authoring commands",
    "editor.apply": "Apply",
    "editor.undo": "Undo",
    "editor.redo": "Redo",
    "editor.retry": "Retry",
    "editor.cancelPreview": "Cancel preview",
    "editor.diagnostics": "Diagnostics ({count})",
    "editor.conflicts": "{count} conflicts require attention.",

    "storage.provider": "Provider",
    "storage.account": "Account",
    "storage.scope": "Scope",
    "storage.lastSync": "Last sync",
    "storage.pending": "Pending changes",
    "storage.review": "Review external changes before publishing.",
    "storage.consequence": "Disconnecting keeps the local project and stops external sync.",
    "storage.disconnect": "Disconnect",
    "storage.unavailable": "Unavailable",
    "storage.never": "Never",

    "library.title": "Library",
    "library.subtitle": "Browse trusted packs and templates.",
    "library.search": "Search",
    "library.kind": "Kind",
    "library.kind.all": "All",
    "library.kind.packs": "Packs",
    "library.kind.templates": "Templates",
    "library.browse": "Browse",
    "library.results": "Registry results",
    "library.failed": "The Library request failed ({code}).",
    "library.searchPlaceholder": "Search packs and templates…",
    "library.empty.noSources": "No registry source is connected.",
    "library.empty.noSourcesHint": "Connect a source below to browse trusted packs and templates.",
    "library.empty.noResults": "Nothing matched your search.",
    "library.selected.title": "Selected",
    "library.action.label": "Action",
    "library.action.install": "Install",
    "library.action.update": "Update",
    "library.template.hint": "Creates a new project from this template.",
    "library.preview": "Preview change",
    "library.plan.title": "Confirm registry change",
    "library.plan.artifacts": "{count} artifact(s)",
    "library.plan.migration": "Migration review is required.",
    "library.plan.noMigration": "No state migration is required.",
    "library.plan.apply": "Confirm and apply",
    "library.recover": "Recover registry transaction",
    "library.sources": "Sources",
    "library.source.connected": "Connected",
    "library.source.disconnected": "Disconnected",
    "library.source.connect": "Connect",
    "library.source.disconnect": "Disconnect",
    "library.source.connectionRef": "Connection reference",
    "library.source.add": "Add source",
    "library.source.id": "Source ID",
    "library.source.kind": "Source kind",
    "library.source.endpoint": "Endpoint",

    "review.title": "Review",
    "review.refresh": "Refresh",
    "review.empty": "Select a proposal to inspect it.",

    "mcp.title": "AI access (MCP)",
    "mcp.state.connected": "Connected",
    "mcp.state.revoking": "Revoking…",
    "mcp.state.revoked": "Revoked",
    "mcp.state.expired": "Expired",
    "mcp.state.host_restarted": "Host restarted",
    "mcp.enable": "Allow AI connections",
    "mcp.disable": "Stop AI connections",
    "mcp.restart": "Restart host",
    "mcp.off": "AI access is off. No local connections are accepted.",
    "mcp.instructions": "Connection instructions",
    "mcp.agent": "Agent",
    "mcp.scopes": "Scopes",
    "mcp.capabilities": "Capabilities",
    "mcp.expires": "Expires",
    "mcp.none": "None",
    "mcp.noAccess": "No access",
    "mcp.proposalOnly": "Proposal only — approval requests appear in Review. Direct apply is unavailable.",
    "mcp.revoke": "Revoke access",
    "mcp.clientName": "Client name",
    "mcp.agentIdentity": "Agent identity",
    "mcp.delegatedScopes": "Delegated scopes",
    "mcp.authoringCapabilities": "Authoring capabilities",
    "mcp.confirmApply": "I confirm this agent may directly apply authorized changes.",
    "mcp.connect": "Connect agent",
    "mcp.connectForm": "Connect an AI agent",
    "mcp.updated": "AI access settings updated.",
    "mcp.failed": "The request failed.",
    "mcp.scope.read": "Read",
    "mcp.scope.export": "Export",
    "mcp.scope.propose": "Propose",
    "mcp.scope.apply": "Apply",
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
    "hub.recent.untitled": "(名称未設定プロジェクト)",
    "hub.templates.title": "テンプレートから開始",
    "hub.templates.blank": "空のプロジェクト",
    "hub.templates.hint": "ライブラリのソースを接続すると、ここにテンプレートが表示されます。",
    "hub.error.title": "プロジェクトを開けませんでした",
    "hub.error.showDetails": "詳細を表示",
    "hub.error.hideDetails": "詳細を隠す",
    "hub.error.dismiss": "閉じる",

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

    "workspace.status.conflict": "要確認",
    "workspace.status.reconcile_pending": "外部変更が保留中",
    "workspace.status.denied": "アクセス拒否",
    "settings.title": "設定",
    "settings.close": "設定を閉じる",
    "settings.group.application": "アプリケーション",
    "settings.group.project": "プロジェクト",
    "settings.nav.general": "一般",
    "settings.nav.mcpDefaults": "AI連携の既定",
    "settings.nav.agentAccess": "AIの権限",
    "settings.general.description": "アプリ全体の設定。プロジェクトに依存しない項目のみ。",
    "settings.language.label": "言語",
    "settings.language.hint": "メニュー・UI・診断メッセージの言語",
    "settings.language.system": "システム",
    "settings.theme.label": "テーマ",
    "settings.theme.hint": "ライト / ダーク / OSに従う",
    "settings.theme.system": "システム",
    "settings.theme.light": "ライト",
    "settings.theme.dark": "ダーク",
    "settings.zoom.label": "表示倍率",
    "settings.zoom.hint": "ウィンドウ全体を拡大縮小します(50〜300%)",
    "settings.mcpDefaults.title": "AI連携の既定",
    "settings.mcpDefaults.description": "Claude等のAIクライアントは MCP という共通規格でLayerDrawに接続します。ここはその接続の初期値で、プロジェクト側の設定が常に優先されます。",
    "settings.mcp.enable.label": "この端末のAIクライアントに接続を許可",
    "settings.mcp.enable.hint": "オフの間、AIはLayerDrawに一切アクセスできません",
    "settings.mcp.restart.hint": "すべてのAIセッションを切断して新しい世代で再起動します",
    "settings.mcp.status.on": "オン",
    "settings.mcp.status.off": "オフ",
    "settings.agentAccess.title": "AIの権限",
    "settings.agentAccess.description": "このプロジェクトに接続しているAIごとの権限です。",
    "settings.agentAccess.empty": "このプロジェクトに接続したAIクライアントはまだありません。",
    "settings.agentAccess.revoke": "接続を取り消す",
    "settings.agentAccess.delete": "接続の記録を削除",
    "settings.agentAccess.expires": "{when} に期限切れ",
    "settings.agentAccess.scopes": "できること",
    "settings.agentAccess.enableFirst": "先に「AI連携の既定」で接続を許可してください。",
    "settings.agentAccess.config": "接続設定 (JSON)",
    "settings.agentAccess.config.show": "接続設定を表示",
    "settings.agentAccess.copy": "コピー",
    "settings.agentAccess.copied": "コピーしました",
    "settings.agentAccess.configHint": "AIクライアントのMCP設定に貼り付けてください(Claude Desktopの場合は claude_desktop_config.json)。LayerDraw Desktop が起動している必要があります。",
    "settings.agentAccess.connected": "接続中のAI",
    "settings.agentAccess.connect.title": "AIクライアントを接続",
    "settings.agentAccess.targets": "対象範囲",
    "settings.agentAccess.expiry": "有効期限",
    "settings.agentAccess.expiry.1h": "1時間",
    "settings.agentAccess.expiry.8h": "8時間",
    "settings.agentAccess.expiry.7d": "7日",
    "settings.capability.graph_write": "エンティティ/関係",
    "settings.capability.query_write": "クエリ",
    "settings.capability.view_write": "ビュー定義",
    "settings.capability.schema_write": "スキーマ(型定義)",
    "settings.capability.asset_write": "アセット",
    "settings.capability.package_manage": "パック管理",
    "settings.saveFailed": "設定を保存できませんでした。もう一度お試しください。",
    "workspace.pane.layers": "レイヤー",
    "workspace.persistence.clean": "保存済み",
    "workspace.persistence.preview_pending": "プレビュー中",
    "workspace.persistence.ephemeral": "未保存",
    "workspace.persistence.durable_pending": "保存中…",
    "workspace.persistence.reconcile_pending": "同期待ち",
    "workspace.statusbar.ok": "検証エラーなし",
    "workspace.statusbar.attention": "要対応",
    "workspace.statusbar.view": "ビュー: {name}",
    "workspace.statusbar.entities": "エンティティ {count}",
    "workspace.statusbar.relations": "リレーション {count}",
    "workspace.statusbar.undo": "元に戻す ⌘Z · やり直す ⇧⌘Z",
    "workspace.kind.view": "ビュー",
    "workspace.structure.empty": "ストラクチャ編集はオーサリングマイルストーンで提供されます。",
    "workspace.empty.select": "ビューを選択してください。",
    "workspace.empty.view": "このビューは空です。",
    "workspace.attention": "このビューには対応が必要です。",

    "inspector.section.editing": "編集",
    "inspector.section.storage": "外部ストレージ",
    "inspector.section.library": "ライブラリ",
    "inspector.section.review": "レビュー",
    "inspector.section.mcp": "AI連携 (MCP)",

    "editor.commands": "編集操作",
    "editor.apply": "適用",
    "editor.undo": "元に戻す",
    "editor.redo": "やり直す",
    "editor.retry": "再試行",
    "editor.cancelPreview": "プレビューを取消",
    "editor.diagnostics": "診断 ({count})",
    "editor.conflicts": "{count} 件の競合に対応が必要です。",

    "storage.provider": "プロバイダ",
    "storage.account": "アカウント",
    "storage.scope": "対象範囲",
    "storage.lastSync": "最終同期",
    "storage.pending": "保留中の変更",
    "storage.review": "公開する前に外部変更を確認してください。",
    "storage.consequence": "接続を解除してもローカルのプロジェクトは残り、外部同期のみ停止します。",
    "storage.disconnect": "接続を解除",
    "storage.unavailable": "取得できません",
    "storage.never": "未同期",

    "library.title": "ライブラリ",
    "library.subtitle": "信頼済みのパックとテンプレートを探せます。",
    "library.search": "検索",
    "library.kind": "種類",
    "library.kind.all": "すべて",
    "library.kind.packs": "パック",
    "library.kind.templates": "テンプレート",
    "library.browse": "探す",
    "library.results": "検索結果",
    "library.failed": "ライブラリへのリクエストが失敗しました ({code})。",
    "library.searchPlaceholder": "パックやテンプレートを検索…",
    "library.empty.noSources": "レジストリソースが未接続です。",
    "library.empty.noSourcesHint": "下のソース設定から接続すると、信頼済みのパックとテンプレートを探せます。",
    "library.empty.noResults": "検索に一致するものはありません。",
    "library.selected.title": "選択中",
    "library.action.label": "操作",
    "library.action.install": "インストール",
    "library.action.update": "更新",
    "library.template.hint": "このテンプレートから新しいプロジェクトを作成します。",
    "library.preview": "変更をプレビュー",
    "library.plan.title": "レジストリ変更の確認",
    "library.plan.artifacts": "{count} 件のアーティファクト",
    "library.plan.migration": "移行のレビューが必要です。",
    "library.plan.noMigration": "状態の移行は不要です。",
    "library.plan.apply": "確認して適用",
    "library.recover": "レジストリトランザクションを復旧",
    "library.sources": "ソース",
    "library.source.connected": "接続中",
    "library.source.disconnected": "未接続",
    "library.source.connect": "接続",
    "library.source.disconnect": "切断",
    "library.source.connectionRef": "接続リファレンス",
    "library.source.add": "ソースを追加",
    "library.source.id": "ソースID",
    "library.source.kind": "ソース種別",
    "library.source.endpoint": "エンドポイント",

    "review.title": "レビュー",
    "review.refresh": "更新",
    "review.empty": "提案を選択すると内容を確認できます。",

    "mcp.title": "AI連携 (MCP)",
    "mcp.state.connected": "接続中",
    "mcp.state.revoking": "取り消し中…",
    "mcp.state.revoked": "取り消し済み",
    "mcp.state.expired": "期限切れ",
    "mcp.state.host_restarted": "ホスト再起動",
    "mcp.enable": "AIからの接続を許可",
    "mcp.disable": "AIからの接続を停止",
    "mcp.restart": "ホストを再起動",
    "mcp.off": "AI連携はオフです。接続は受け付けていません。",
    "mcp.instructions": "接続手順",
    "mcp.agent": "エージェント",
    "mcp.scopes": "できること",
    "mcp.capabilities": "対象範囲",
    "mcp.expires": "有効期限",
    "mcp.none": "なし",
    "mcp.noAccess": "アクセスなし",
    "mcp.proposalOnly": "提案のみ — 変更はレビューでの承認後に適用されます。直接適用はできません。",
    "mcp.revoke": "接続を取り消す",
    "mcp.clientName": "クライアント名",
    "mcp.agentIdentity": "エージェント識別子",
    "mcp.delegatedScopes": "許可する操作",
    "mcp.authoringCapabilities": "編集できる対象",
    "mcp.confirmApply": "このエージェントによる承認済み変更の直接適用を許可します。",
    "mcp.connect": "エージェントを接続",
    "mcp.connectForm": "AIエージェントを接続",
    "mcp.updated": "AI連携の設定を更新しました。",
    "mcp.failed": "リクエストに失敗しました。",
    "mcp.scope.read": "読み取り",
    "mcp.scope.export": "エクスポート",
    "mcp.scope.propose": "変更を提案",
    "mcp.scope.apply": "直接適用",
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
