// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

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

function scopeText(connection: DesktopMCPConnection): string {
	return (["read", "export", "propose", "apply"] as const).filter((scope) => connection.permissions[scope]).join(" · ") || "No access";
}

export function DesktopMCPPanel({ mcp, projectID }: DesktopMCPPanelProps): ReactNode {
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
	useEffect(() => { void refresh().catch(() => setNotice(failureLabels["desktop.mcp_transport_failed"] ?? "The MCP request failed.")); }, [mcp, projectID]);

	const run = async (operation: () => Promise<{ readonly outcome: string; readonly failure?: { readonly code: string } }>): Promise<void> => {
		setBusy(true); setNotice("");
		try {
			const result = await operation();
			setNotice(result.outcome === "success" ? "MCP settings updated." : failureLabels[result.failure?.code ?? ""] ?? "The MCP request failed.");
			await refresh();
		} catch { setNotice(failureLabels["desktop.mcp_transport_failed"] ?? "The MCP request failed."); }
		finally { setBusy(false); }
	};

	const connectionItems = connections.map((connection) => createElement("li", { key: connection.connection_id, className: "ld-mcp-connection", "data-status": connection.status },
		createElement("div", null, createElement("strong", null, connection.client_id), createElement("span", { className: "ld-desktop-chip", "data-status": connection.status }, connection.status.replaceAll("_", " "))),
		createElement("dl", null,
			createElement("div", null, createElement("dt", null, "Agent"), createElement("dd", null, connection.agent_id)),
			createElement("div", null, createElement("dt", null, "Session"), createElement("dd", null, connection.session_id)),
			createElement("div", null, createElement("dt", null, "Scopes"), createElement("dd", null, scopeText(connection))),
			createElement("div", null, createElement("dt", null, "Capabilities"), createElement("dd", null, connection.capabilities.join(", ") || "None")),
			createElement("div", null, createElement("dt", null, "Expires"), createElement("dd", null, new Date(connection.expires_at).toLocaleString()))),
		connection.permissions.propose && !connection.permissions.apply ? createElement("p", { className: "ld-mcp-proposal" }, "Proposal only — approval requests appear in Review. Direct apply is unavailable.") : null,
		connection.status === "connected" ? createElement("button", { type: "button", disabled: busy, onClick: () => { void run(() => mcp.revokeConnection(connection.connection_id)); } }, "Revoke access") : null));

	return createElement("section", { className: "ld-mcp-panel", "aria-label": "MCP connections" },
		createElement("div", { className: "ld-mcp-heading" }, createElement("div", null, createElement("p", { className: "ld-desktop-eyebrow" }, "Local automation"), createElement("h2", null, "MCP connections")),
			createElement("div", null,
				createElement("button", { type: "button", disabled: busy || status === undefined, "aria-pressed": status?.enabled ?? false, onClick: () => { void run(() => mcp.setEnabled(!(status?.enabled ?? false))); } }, status?.enabled ? "Disable MCP" : "Enable MCP"),
				createElement("button", { type: "button", disabled: busy || status?.enabled !== true, onClick: () => { void run(() => mcp.restart()); } }, "Restart host"))),
		status?.enabled ? createElement("p", { className: "ld-mcp-instructions" }, createElement("strong", null, "Connection instructions"), createElement("code", null, status.instructions)) : createElement("p", null, "MCP is off. No local transport is listening."),
		status?.enabled ? createElement("form", { onSubmit: (event) => { event.preventDefault(); void run(() => mcp.createConnection({ client_id: clientID, protocol_version: protocolVersion, document_id: projectID, agent_id: agentID, capabilities, permissions, expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(), confirm_apply: confirmApply })); }, "aria-label": "Connect MCP agent" },
			createElement("label", null, "Client name", createElement("input", { required: true, value: clientID, onChange: (event) => setClientID(event.currentTarget.value) })),
			createElement("label", null, "Agent identity", createElement("input", { required: true, value: agentID, onChange: (event) => setAgentID(event.currentTarget.value) })),
			createElement("fieldset", null, createElement("legend", null, "Delegated scopes"), (["read", "export", "propose", "apply"] as const).map((scope) => createElement("label", { key: scope }, createElement("input", { type: "checkbox", checked: permissions[scope], onChange: (event) => setPermissions({ ...permissions, [scope]: event.currentTarget.checked }) }), scope))),
			createElement("fieldset", null, createElement("legend", null, "Authoring capabilities"), capabilityOptions.map((capability) => createElement("label", { key: capability }, createElement("input", { type: "checkbox", checked: capabilities.includes(capability), onChange: (event) => setCapabilities(event.currentTarget.checked ? [...capabilities, capability] : capabilities.filter((value) => value !== capability)) }), capability))),
			permissions.apply ? createElement("label", { className: "ld-mcp-confirm" }, createElement("input", { type: "checkbox", required: true, checked: confirmApply, onChange: (event) => setConfirmApply(event.currentTarget.checked) }), "I confirm this agent may directly apply authorized changes.") : null,
			createElement("button", { type: "submit", disabled: busy || !permissions.read || (permissions.apply && !confirmApply) }, "Connect agent")) : null,
		createElement("p", { role: "status", "aria-live": "polite" }, notice),
		createElement("ul", { className: "ld-mcp-connections" }, connectionItems));
}
