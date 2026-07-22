// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { createElement, useEffect, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react/i18n";
import { SelectIcon, SelectItem, SelectItemText, SelectPopup, SelectPortal, SelectPositioner, SelectRoot, SelectTrigger, SelectValue } from "@layerdraw/react/primitives";
import type { DesktopMCPConnection, DesktopMCPPort, DesktopMCPStatus, DesktopSettingsDTO, DesktopSettingsPort } from "./contracts.js";

const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

type SettingsPane = "general" | "mcp_defaults" | "agent_access";

export interface DesktopSettingsDialogProps {
  readonly settings: DesktopSettingsPort;
  readonly mcp?: DesktopMCPPort;
  /** Present only while a project is open; enables the Project nav group. */
  readonly projectName?: string;
  readonly projectID?: string;
  readonly onClose: () => void;
  /** Applies a UI locale override immediately ("system" returns to OS-follow). */
  readonly onLocaleChange?: (locale: string) => void;
}

function navItem(t: Translator, pane: SettingsPane, active: SettingsPane, label: string, select: (pane: SettingsPane) => void): ReactNode {
  return createElement("button", {
    type: "button",
    className: "ld-settings-nav-item",
    "aria-current": pane === active ? "page" : undefined,
    onClick: () => select(pane),
  }, label);
}

interface SelectOption { readonly value: string; readonly label: string }

/** shadcn/Base UI Select composition shared by every settings row. */
function tokenSelect(ariaLabel: string, value: string, options: readonly SelectOption[], onChange: (value: string) => void): ReactNode {
  return createElement(SelectRoot, {
    value,
    onValueChange: (next: unknown) => { if (typeof next === "string") onChange(next); },
    items: Object.fromEntries(options.map((option) => [option.value, option.label])),
  },
    createElement(SelectTrigger, { "aria-label": ariaLabel },
      createElement(SelectValue, null),
      createElement(SelectIcon, null, createElement("svg", { viewBox: "0 0 16 16", width: 12, height: 12, fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round", strokeLinejoin: "round", "aria-hidden": true }, createElement("path", { d: "m4 6 4 4 4-4" })))),
    createElement(SelectPortal, null,
      createElement(SelectPositioner, { sideOffset: 4, className: "ld-settings-select-positioner" },
        createElement(SelectPopup, null, options.map((option) =>
          createElement(SelectItem, { key: option.value, value: option.value },
            createElement(SelectItemText, null, option.label)))))));
}

function settingsRow(label: string, hint: string | undefined, control: ReactNode): ReactNode {
  return createElement("div", { className: "ld-settings-row" },
    createElement("span", { className: "ld-settings-row-label" }, label,
      hint === undefined ? null : createElement("small", null, hint)),
    createElement("span", { className: "ld-settings-row-control" }, control));
}

function scopeChips(t: Translator, connection: DesktopMCPConnection): ReactNode {
  const scopes: readonly (readonly [string, boolean])[] = [
    [t.t("mcp.scope.read"), connection.permissions.read],
    [t.t("mcp.scope.propose"), connection.permissions.propose],
    [t.t("mcp.scope.apply"), connection.permissions.apply],
    [t.t("mcp.scope.export"), connection.permissions.export],
  ];
  return createElement("span", { className: "ld-settings-scopes" }, scopes.map(([label, granted]) =>
    createElement("span", { key: label, className: "ld-settings-scope", "data-granted": granted }, label)));
}

function GeneralPane({ t, settings, onLocaleChange }: { readonly t: Translator; readonly settings: DesktopSettingsPort; readonly onLocaleChange?: (locale: string) => void }): ReactNode {
  const [current, setCurrent] = useState<DesktopSettingsDTO>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void settings.load().then((result) => {
      if (!cancelled && result.outcome === "success" && result.value !== undefined) setCurrent(result.value);
    }, () => {});
    return () => { cancelled = true; };
  }, [settings]);

  const commit = (next: DesktopSettingsDTO): void => {
    setCurrent(next);
    setFailed(false);
    void settings.update(next).then((result) => {
      if (result.outcome !== "success") setFailed(true);
    }, () => setFailed(true));
  };

  if (current === undefined) return createElement("p", { role: "status" }, "…");
  const locale = current.locale === undefined || current.locale === "" ? "system" : current.locale;
  return createElement("section", { className: "ld-settings-pane" },
    createElement("h2", null, t.t("settings.nav.general")),
    createElement("p", { className: "ld-settings-desc" }, t.t("settings.general.description")),
    failed ? createElement("p", { role: "alert", className: "ld-settings-error" }, t.t("settings.saveFailed")) : null,
    createElement("div", { className: "ld-settings-card" },
      settingsRow(t.t("settings.language.label"), t.t("settings.language.hint"),
        tokenSelect(t.t("settings.language.label"), locale, [
          { value: "system", label: t.t("settings.language.system") },
          { value: "en", label: "English" },
          { value: "ja", label: "日本語" },
        ], (value) => {
          commit({ ...current, locale: value });
          onLocaleChange?.(value);
        })),
      settingsRow(t.t("settings.theme.label"), t.t("settings.theme.hint"),
        tokenSelect(t.t("settings.theme.label"), current.theme, [
          { value: "system", label: t.t("settings.theme.system") },
          { value: "light", label: t.t("settings.theme.light") },
          { value: "dark", label: t.t("settings.theme.dark") },
        ], (value) => commit({ ...current, theme: value as DesktopSettingsDTO["theme"] }))),
      settingsRow(t.t("settings.zoom.label"), t.t("settings.zoom.hint"),
        tokenSelect(t.t("settings.zoom.label"), String(current.zoom_percent), [50, 75, 90, 100, 110, 125, 150, 175, 200, 250, 300].map((percent) => ({ value: String(percent), label: `${percent}%` })),
          (value) => commit({ ...current, zoom_percent: Number(value) })))));
}

function MCPDefaultsPane({ t, mcp }: { readonly t: Translator; readonly mcp: DesktopMCPPort }): ReactNode {
  const [status, setStatus] = useState<DesktopMCPStatus>();
  const [pending, setPending] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void mcp.status().then((value) => { if (!cancelled) setStatus(value); }, () => {});
    return () => { cancelled = true; };
  }, [mcp]);
  if (status === undefined) return createElement("p", { role: "status" }, "…");
  const toggle = (): void => {
    setPending(true);
    void mcp.setEnabled(!status.enabled).then((result) => {
      if (result.outcome === "success" && result.value !== undefined) setStatus(result.value);
    }, () => {}).finally(() => setPending(false));
  };
  return createElement("section", { className: "ld-settings-pane" },
    createElement("h2", null, t.t("settings.mcpDefaults.title")),
    createElement("p", { className: "ld-settings-desc" }, t.t("settings.mcpDefaults.description")),
    createElement("div", { className: "ld-settings-card" },
      createElement("div", { className: "ld-settings-card-head" },
        t.t("mcp.title"),
        createElement("span", { className: "ld-settings-badge", "data-on": status.enabled }, status.enabled ? t.t("settings.mcp.status.on") : t.t("settings.mcp.status.off"))),
      settingsRow(t.t("settings.mcp.enable.label"), t.t("settings.mcp.enable.hint"),
        createElement("button", {
          type: "button",
          role: "switch",
          "aria-checked": status.enabled,
          "aria-label": t.t("settings.mcp.enable.label"),
          className: "ld-settings-toggle",
          disabled: pending,
          onClick: toggle,
        }))));
}

interface ConnectDraft {
  readonly clientID: string;
  readonly agentID: string;
  readonly permissions: { readonly read: boolean; readonly export: boolean; readonly propose: boolean; readonly apply: boolean };
  readonly capabilities: readonly string[];
  readonly expiryHours: number;
  readonly confirmApply: boolean;
}

const capabilityOptions = ["graph:write", "query:write", "view:write", "schema:write", "asset:write", "package:manage"] as const;

function scopeChipButton(label: string, granted: boolean, disabled: boolean, onToggle: () => void): ReactNode {
  return createElement("button", {
    key: label, type: "button", className: "ld-settings-scope", "data-granted": granted,
    role: "checkbox", "aria-checked": granted, disabled, onClick: onToggle,
  }, label);
}

function ConnectionConfig({ t, mcp, connectionID }: { readonly t: Translator; readonly mcp: DesktopMCPPort; readonly connectionID: string }): ReactNode {
  const [config, setConfig] = useState<string>();
  const [copied, setCopied] = useState(false);
  if (mcp.clientConfig === undefined) return null;
  if (config === undefined) {
    return createElement("div", { className: "ld-settings-agent-row" },
      createElement("button", {
        type: "button", className: "ld-btn ld-btn-secondary",
        onClick: () => { void mcp.clientConfig?.(connectionID).then(setConfig, () => {}); },
      }, t.t("settings.agentAccess.config.show")));
  }
  return createElement("div", { className: "ld-settings-config" },
    createElement("div", { className: "ld-settings-agent-row" },
      createElement("b", null, t.t("settings.agentAccess.config")),
      createElement("button", {
        type: "button", className: "ld-btn ld-btn-secondary",
        onClick: () => {
          void navigator.clipboard?.writeText(config).then(() => setCopied(true), () => {});
        },
      }, copied ? t.t("settings.agentAccess.copied") : t.t("settings.agentAccess.copy"))),
    createElement("pre", null, config),
    createElement("p", { className: "ld-settings-hint" }, t.t("settings.agentAccess.configHint")));
}

function AgentAccessPane({ t, mcp, projectID }: { readonly t: Translator; readonly mcp: DesktopMCPPort; readonly projectID: string }): ReactNode {
  const [enabled, setEnabled] = useState<boolean>();
  const [instructions, setInstructions] = useState("");
  const [connections, setConnections] = useState<readonly DesktopMCPConnection[]>();
  const [generation, setGeneration] = useState(0);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState(false);
  const [draft, setDraft] = useState<ConnectDraft>({
    clientID: "Local AI client", agentID: "desktop-agent",
    permissions: { read: true, export: false, propose: true, apply: false },
    capabilities: ["graph:write"], expiryHours: 8, confirmApply: false,
  });
  useEffect(() => {
    let cancelled = false;
    void Promise.all([mcp.status(), mcp.listConnections()]).then(([status, list]) => {
      if (cancelled) return;
      setEnabled(status.enabled);
      setInstructions(status.instructions);
      setConnections(list.filter((connection) => connection.document_id === projectID));
    }, () => { if (!cancelled) { setEnabled(false); setConnections([]); } });
    return () => { cancelled = true; };
  }, [mcp, projectID, generation]);
  if (enabled === undefined || connections === undefined) return createElement("p", { role: "status" }, "…");

  const run = (operation: () => Promise<{ readonly outcome: string }>): void => {
    setBusy(true); setFailure(false);
    void operation().then((result) => { if (result.outcome !== "success") setFailure(true); }, () => setFailure(true))
      .finally(() => { setBusy(false); setGeneration((value) => value + 1); });
  };
  const connect = (): void => run(() => mcp.createConnection({
    client_id: draft.clientID, protocol_version: "desktop-mcp-v1", document_id: projectID, agent_id: draft.agentID,
    capabilities: [...draft.capabilities], permissions: draft.permissions,
    expires_at: new Date(Date.now() + draft.expiryHours * 60 * 60 * 1000).toISOString(), confirm_apply: draft.confirmApply,
  }));
  const permissionLabels: readonly (readonly ["read" | "propose" | "apply" | "export", string])[] = [
    ["read", t.t("mcp.scope.read")], ["propose", t.t("mcp.scope.propose")], ["apply", t.t("mcp.scope.apply")], ["export", t.t("mcp.scope.export")],
  ];

  return createElement("section", { className: "ld-settings-pane" },
    createElement("h2", null, t.t("settings.agentAccess.title")),
    createElement("p", { className: "ld-settings-desc" }, t.t("settings.agentAccess.description")),
    failure ? createElement("p", { role: "alert", className: "ld-settings-error" }, t.t("settings.saveFailed")) : null,
    !enabled ? createElement("p", { className: "ld-settings-empty" }, t.t("settings.agentAccess.enableFirst")) : createElement("div", null,
      createElement("div", { className: "ld-settings-card" },
        createElement("div", { className: "ld-settings-card-head" }, t.t("settings.agentAccess.connect.title")),
        settingsRow(t.t("mcp.clientName"), undefined,
          createElement("input", { className: "ld-settings-input", value: draft.clientID, "aria-label": t.t("mcp.clientName"), onChange: (event: { currentTarget: { value: string } }) => setDraft({ ...draft, clientID: event.currentTarget.value }) })),
        settingsRow(t.t("mcp.agentIdentity"), undefined,
          createElement("input", { className: "ld-settings-input", value: draft.agentID, "aria-label": t.t("mcp.agentIdentity"), onChange: (event: { currentTarget: { value: string } }) => setDraft({ ...draft, agentID: event.currentTarget.value }) })),
        createElement("div", { className: "ld-settings-agent-row ld-settings-formrow" },
          createElement("b", null, t.t("settings.agentAccess.scopes")),
          createElement("span", { className: "ld-settings-scopes" }, permissionLabels.map(([scope, label]) =>
            scopeChipButton(label, draft.permissions[scope], busy, () => setDraft({ ...draft, permissions: { ...draft.permissions, [scope]: !draft.permissions[scope] }, confirmApply: false }))))),
        createElement("div", { className: "ld-settings-agent-row ld-settings-formrow" },
          createElement("b", null, t.t("settings.agentAccess.targets")),
          createElement("span", { className: "ld-settings-scopes" }, capabilityOptions.map((capability) =>
            scopeChipButton(t.t(`settings.capability.${capability}`), draft.capabilities.includes(capability), busy,
              () => setDraft({ ...draft, capabilities: draft.capabilities.includes(capability) ? draft.capabilities.filter((value) => value !== capability) : [...draft.capabilities, capability] }))))),
        settingsRow(t.t("settings.agentAccess.expiry"), undefined,
          tokenSelect(t.t("settings.agentAccess.expiry"), String(draft.expiryHours), [
            { value: "1", label: t.t("settings.agentAccess.expiry.1h") },
            { value: "8", label: t.t("settings.agentAccess.expiry.8h") },
            { value: "168", label: t.t("settings.agentAccess.expiry.7d") },
          ], (value) => setDraft({ ...draft, expiryHours: Number(value) }))),
        !draft.permissions.apply ? null : settingsRow(t.t("mcp.confirmApply"), undefined,
          createElement("button", {
            type: "button", role: "switch", "aria-checked": draft.confirmApply, "aria-label": t.t("mcp.confirmApply"),
            className: "ld-settings-toggle", disabled: busy, onClick: () => setDraft({ ...draft, confirmApply: !draft.confirmApply }),
          })),
        createElement("div", { className: "ld-settings-formfoot" },
          createElement("button", { type: "button", className: "ld-btn ld-btn-primary", disabled: busy || !draft.permissions.read || (draft.permissions.apply && !draft.confirmApply), onClick: connect }, t.t("mcp.connect")))),
      instructions === "" ? null : createElement("p", { className: "ld-settings-hint" }, t.t("mcp.instructions"), " — ", createElement("code", null, instructions)),
      connections.length === 0
        ? createElement("p", { className: "ld-settings-empty" }, t.t("settings.agentAccess.empty"))
        : createElement("div", { className: "ld-settings-card" },
          createElement("div", { className: "ld-settings-card-head" },
            t.t("settings.agentAccess.connected"),
            createElement("span", { className: "ld-settings-badge", "data-on": connections.some((connection) => connection.status === "connected") }, String(connections.filter((connection) => connection.status === "connected").length))),
          connections.map((connection) =>
            createElement("div", { key: connection.connection_id, className: "ld-settings-agent" },
              createElement("div", { className: "ld-settings-agent-head" },
                createElement("b", null, connection.agent_id),
                createElement("span", { className: "ld-settings-badge", "data-on": connection.status === "connected" }, t.t(`mcp.state.${connection.status}`)),
                createElement("span", { className: "ld-settings-agent-exp" }, t.t("settings.agentAccess.expires", { when: t.formatDate(connection.expires_at) }))),
              createElement("div", { className: "ld-settings-agent-row" },
                createElement("b", null, t.t("settings.agentAccess.scopes")),
                scopeChips(t, connection),
                connection.status !== "connected" ? null : createElement("button", {
                  type: "button", className: "ld-settings-revoke", disabled: busy,
                  onClick: () => run(() => mcp.revokeConnection(connection.connection_id)),
                }, t.t("settings.agentAccess.revoke"))),
              createElement("div", { className: "ld-settings-agent-row" },
                createElement("b", null, t.t("settings.agentAccess.targets")),
                createElement("span", { className: "ld-settings-scopes" }, connection.capabilities.map((capability) =>
                  createElement("span", { key: capability, className: "ld-settings-scope", "data-granted": true }, t.t(`settings.capability.${capability}`))))),
              connection.status !== "connected" ? null : createElement(ConnectionConfig, { t, mcp, connectionID: connection.connection_id }))))));
}

export function DesktopSettingsDialog({ settings, mcp, projectName, projectID, onClose, onLocaleChange }: DesktopSettingsDialogProps): ReactNode {
  const t = useOptionalI18n() ?? defaultTranslator;
  const [pane, setPane] = useState<SettingsPane>("general");

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return createElement("div", { className: "ld-settings-overlay", onClick: (event: ReactMouseEvent<HTMLDivElement>) => { if (event.target === event.currentTarget) onClose(); } },
    createElement("div", { role: "dialog", "aria-modal": true, "aria-label": t.t("settings.title"), className: "ld-settings-window" },
      createElement("header", { className: "ld-settings-titlebar" },
        createElement("span", null, t.t("settings.title")),
        createElement("button", { type: "button", className: "ld-settings-close", "aria-label": t.t("settings.close"), onClick: onClose }, "×")),
      createElement("div", { className: "ld-settings-frame" },
        createElement("nav", { className: "ld-settings-nav", "aria-label": t.t("settings.title") },
          createElement("span", { className: "ld-settings-nav-group" }, t.t("settings.group.application")),
          navItem(t, "general", pane, t.t("settings.nav.general"), setPane),
          mcp === undefined ? null : navItem(t, "mcp_defaults", pane, t.t("settings.nav.mcpDefaults"), setPane),
          mcp === undefined || projectName === undefined ? null : createElement("span", { className: "ld-settings-nav-group" },
            t.t("settings.group.project"),
            createElement("small", null, projectName)),
          mcp === undefined || projectName === undefined || projectID === undefined ? null : navItem(t, "agent_access", pane, t.t("settings.nav.agentAccess"), setPane)),
        createElement("div", { className: "ld-settings-body" },
          pane === "general" ? createElement(GeneralPane, { t, settings, ...(onLocaleChange === undefined ? {} : { onLocaleChange }) }) : null,
          pane === "mcp_defaults" && mcp !== undefined ? createElement(MCPDefaultsPane, { t, mcp }) : null,
          pane === "agent_access" && mcp !== undefined && projectID !== undefined ? createElement(AgentAccessPane, { t, mcp, projectID }) : null))));
}
