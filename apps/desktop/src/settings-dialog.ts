// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { createElement, useEffect, useState, type ChangeEvent, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react/i18n";
import type { DesktopMCPConnection, DesktopMCPPort, DesktopMCPStatus, DesktopSettingsDTO, DesktopSettingsPort } from "./contracts.js";

const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

type SettingsPane = "general" | "mcp_defaults" | "agent_access";

export interface DesktopSettingsDialogProps {
  readonly settings: DesktopSettingsPort;
  readonly mcp?: DesktopMCPPort;
  /** Present only while a project is open; enables the Project nav group. */
  readonly projectName?: string;
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
        createElement("select", {
          value: locale,
          "aria-label": t.t("settings.language.label"),
          onChange: (event: ChangeEvent<HTMLSelectElement>) => {
            const value = event.target.value;
            commit({ ...current, locale: value });
            onLocaleChange?.(value);
          },
        },
          createElement("option", { value: "system" }, t.t("settings.language.system")),
          createElement("option", { value: "en" }, "English"),
          createElement("option", { value: "ja" }, "日本語"))),
      settingsRow(t.t("settings.theme.label"), t.t("settings.theme.hint"),
        createElement("select", {
          value: current.theme,
          "aria-label": t.t("settings.theme.label"),
          onChange: (event: ChangeEvent<HTMLSelectElement>) => commit({ ...current, theme: event.target.value as DesktopSettingsDTO["theme"] }),
        },
          createElement("option", { value: "system" }, t.t("settings.theme.system")),
          createElement("option", { value: "light" }, t.t("settings.theme.light")),
          createElement("option", { value: "dark" }, t.t("settings.theme.dark")))),
      settingsRow(t.t("settings.zoom.label"), t.t("settings.zoom.hint"),
        createElement("select", {
          value: String(current.zoom_percent),
          "aria-label": t.t("settings.zoom.label"),
          onChange: (event: ChangeEvent<HTMLSelectElement>) => commit({ ...current, zoom_percent: Number(event.target.value) }),
        }, [50, 75, 90, 100, 110, 125, 150, 175, 200, 250, 300].map((percent) =>
          createElement("option", { key: percent, value: String(percent) }, `${percent}%`))))));
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

function AgentAccessPane({ t, mcp }: { readonly t: Translator; readonly mcp: DesktopMCPPort }): ReactNode {
  const [connections, setConnections] = useState<readonly DesktopMCPConnection[]>();
  const [generation, setGeneration] = useState(0);
  useEffect(() => {
    let cancelled = false;
    void mcp.listConnections().then((value) => { if (!cancelled) setConnections(value); }, () => { if (!cancelled) setConnections([]); });
    return () => { cancelled = true; };
  }, [mcp, generation]);
  if (connections === undefined) return createElement("p", { role: "status" }, "…");
  return createElement("section", { className: "ld-settings-pane" },
    createElement("h2", null, t.t("settings.agentAccess.title")),
    createElement("p", { className: "ld-settings-desc" }, t.t("settings.agentAccess.description")),
    connections.length === 0
      ? createElement("p", { className: "ld-settings-empty" }, t.t("settings.agentAccess.empty"))
      : createElement("div", { className: "ld-settings-card" }, connections.map((connection) =>
        createElement("div", { key: connection.connection_id, className: "ld-settings-agent" },
          createElement("div", { className: "ld-settings-agent-head" },
            createElement("b", null, connection.agent_id),
            createElement("span", { className: "ld-settings-badge", "data-on": connection.status === "connected" }, t.t(`mcp.state.${connection.status}`)),
            createElement("span", { className: "ld-settings-agent-exp" }, t.t("settings.agentAccess.expires", { when: t.formatDate(connection.expires_at) }))),
          createElement("div", { className: "ld-settings-agent-row" },
            createElement("b", null, t.t("settings.agentAccess.scopes")),
            scopeChips(t, connection),
            connection.status !== "connected" ? null : createElement("button", {
              type: "button",
              className: "ld-settings-revoke",
              onClick: () => { void mcp.revokeConnection(connection.connection_id).finally(() => setGeneration((value) => value + 1)); },
            }, t.t("settings.agentAccess.revoke")))))));
}

export function DesktopSettingsDialog({ settings, mcp, projectName, onClose, onLocaleChange }: DesktopSettingsDialogProps): ReactNode {
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
          mcp === undefined || projectName === undefined ? null : navItem(t, "agent_access", pane, t.t("settings.nav.agentAccess"), setPane)),
        createElement("div", { className: "ld-settings-body" },
          pane === "general" ? createElement(GeneralPane, { t, settings, ...(onLocaleChange === undefined ? {} : { onLocaleChange }) }) : null,
          pane === "mcp_defaults" && mcp !== undefined ? createElement(MCPDefaultsPane, { t, mcp }) : null,
          pane === "agent_access" && mcp !== undefined ? createElement(AgentAccessPane, { t, mcp }) : null))));
}
