// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react";
import { createElement, useEffect, useState, type ReactNode } from "react";
import type { DesktopMCPConnection, DesktopMCPPermissions, DesktopMCPPort } from "./contracts.js";

const protocolVersion = "desktop-mcp-v1" as const;
const capabilityOptions = ["graph:write", "query:write", "view:write", "schema:write", "asset:write", "package:manage"] as const;
const failureLabels: Readonly<Record<string, string>> = Object.freeze({
	"desktop.mcp_transport_failed": "The local MCP transport could not be started.",
	"desktop.mcp_disabled": "Enable the local MCP surface before connecting an agent.",
	"desktop.mcp_version_mismatch": "The client version is incompatible. Upgrade the MCP client.",
	"desktop.mcp_scope_denied": "The requested agent scope exceeds the current local grant.",
	"desktop.mcp_disconnected": "The MCP client disconnected. Reconnect it to continue.",
	"desktop.mcp_host_restarted": "Desktop restarted. Reconnect this agent session.",
	"desktop.agent_delegation_failed": "The agent delegation could not be created or revoked.",
});

export interface DesktopMCPPanelProps { readonly mcp: DesktopMCPPort; readonly projectID: string }

function scopeText(t: Translator, connection: DesktopMCPConnection): string {
	return (["read", "export", "propose", "apply"] as const).filter((scope) => connection.permissions[scope]).map((scope) => t.t(`mcp.scope.${scope}`)).join(" · ") || t.t("mcp.noAccess");
}

const defaultTranslator: Translator = createTranslator("en", baseShellCatalogs);

export function DesktopMCPPanel({ mcp, projectID }: DesktopMCPPanelProps): ReactNode {
	const t = useOptionalI18n() ?? defaultTranslator;
	const [status, setStatus] = useState<Awaited<ReturnType<DesktopMCPPort["status"]>>>();
	const [connections, setConnections] = useState<readonly DesktopMCPConnection[]>([]);
	const [busy, setBusy] = useState(false);
	const [notice, setNotice] = useState("");
	const [clientID, setClientID] = useState("Local AI client");
	const [agentID, setAgentID] = useState("desktop-agent");
	const [permissions, setPermissions] = useState<DesktopMCPPermissions>({ read: true, export: false, propose: true, apply: false });
	const [confirmApply, setConfirmApply] = useState(false);
	const [capabilities, setCapabilities] = useState<readonly string[]>(["graph:write"]);

	const refresh = async (): Promise<void> => {
		const [nextStatus, nextConnections] = await Promise.all([mcp.status(), mcp.listConnections()]);
		setStatus(nextStatus); setConnections(nextConnections);
	};
	useEffect(() => { void refresh().catch(() => setNotice(failureLabels["desktop.mcp_transport_failed"] ?? t.t("mcp.failed"))); }, [mcp, projectID]);

	const run = async (operation: () => Promise<{ readonly outcome: string; readonly failure?: { readonly code: string } }>): Promise<void> => {
		setBusy(true); setNotice("");
		try {
			const result = await operation();
			setNotice(result.outcome === "success" ? t.t("mcp.updated") : failureLabels[result.failure?.code ?? ""] ?? t.t("mcp.failed"));
			await refresh();
		} catch { setNotice(failureLabels["desktop.mcp_transport_failed"] ?? t.t("mcp.failed")); }
		finally { setBusy(false); }
	};

	const connectionItems = connections.map((connection) => createElement("li", { key: connection.connection_id, className: "ld-mcp-connection", "data-status": connection.status },
		createElement("div", null, createElement("strong", null, connection.client_id), createElement("span", { className: "ld-desktop-chip", "data-status": connection.status }, connection.status.replaceAll("_", " "))),
		createElement("dl", null,
			createElement("div", null, createElement("dt", null, t.t("mcp.agent")), createElement("dd", null, connection.agent_id)),
			createElement("div", null, createElement("dt", null, t.t("mcp.scopes")), createElement("dd", null, scopeText(t, connection))),
			createElement("div", null, createElement("dt", null, t.t("mcp.capabilities")), createElement("dd", null, connection.capabilities.join(", ") || t.t("mcp.none"))),
			createElement("div", null, createElement("dt", null, t.t("mcp.expires")), createElement("dd", null, t.formatDate(connection.expires_at)))),
		connection.permissions.propose && !connection.permissions.apply ? createElement("p", { className: "ld-mcp-proposal" }, t.t("mcp.proposalOnly")) : null,
		connection.status === "connected" ? createElement("button", { type: "button", disabled: busy, onClick: () => { void run(() => mcp.revokeConnection(connection.connection_id)); } }, t.t("mcp.revoke")) : null));

	return createElement("section", { className: "ld-mcp-panel", "aria-label": t.t("mcp.title") },
		createElement("div", { className: "ld-mcp-heading" }, createElement("div", null, createElement("h2", null, t.t("mcp.title"))),
			createElement("div", null,
				createElement("button", { type: "button", disabled: busy || status === undefined, "aria-pressed": status?.enabled ?? false, onClick: () => { void run(() => mcp.setEnabled(!(status?.enabled ?? false))); } }, status?.enabled ? t.t("mcp.disable") : t.t("mcp.enable")),
				createElement("button", { type: "button", disabled: busy || status?.enabled !== true, onClick: () => { void run(() => mcp.restart()); } }, t.t("mcp.restart"))),
		),
		status?.enabled ? createElement("p", { className: "ld-mcp-instructions" }, createElement("strong", null, t.t("mcp.instructions")), createElement("code", null, status.instructions)) : createElement("p", null, t.t("mcp.off")),
		status?.enabled ? createElement("form", { onSubmit: (event) => { event.preventDefault(); void run(() => mcp.createConnection({ client_id: clientID, protocol_version: protocolVersion, document_id: projectID, agent_id: agentID, capabilities, permissions, expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(), confirm_apply: confirmApply })); }, "aria-label": t.t("mcp.connectForm") },
			createElement("label", null, t.t("mcp.clientName"), createElement("input", { required: true, value: clientID, onChange: (event) => setClientID(event.currentTarget.value) })),
			createElement("label", null, t.t("mcp.agentIdentity"), createElement("input", { required: true, value: agentID, onChange: (event) => setAgentID(event.currentTarget.value) })),
			createElement("fieldset", null, createElement("legend", null, t.t("mcp.delegatedScopes")), (["read", "export", "propose", "apply"] as const).map((scope) => createElement("label", { key: scope }, createElement("input", { type: "checkbox", checked: permissions[scope], onChange: (event) => setPermissions({ ...permissions, [scope]: event.currentTarget.checked }) }), t.t(`mcp.scope.${scope}`)))),
			createElement("fieldset", null, createElement("legend", null, t.t("mcp.authoringCapabilities")), capabilityOptions.map((capability) => createElement("label", { key: capability }, createElement("input", { type: "checkbox", checked: capabilities.includes(capability), onChange: (event) => setCapabilities(event.currentTarget.checked ? [...capabilities, capability] : capabilities.filter((value) => value !== capability)) }), capability))),
			permissions.apply ? createElement("label", { className: "ld-mcp-confirm" }, createElement("input", { type: "checkbox", required: true, checked: confirmApply, onChange: (event) => setConfirmApply(event.currentTarget.checked) }), t.t("mcp.confirmApply")) : null,
			createElement("button", { type: "submit", disabled: busy || !permissions.read || (permissions.apply && !confirmApply) }, t.t("mcp.connect"))) : null,
		createElement("p", { role: "status", "aria-live": "polite" }, notice),
		createElement("ul", { className: "ld-mcp-connections" }, connectionItems));
}
