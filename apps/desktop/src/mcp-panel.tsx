// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import { baseShellCatalogs, createTranslator, useOptionalI18n, type Translator } from "@layerdraw/react";
import { useEffect, useState, type ReactNode } from "react";
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

	const connectionItems = connections.map((connection) => (
		<li key={connection.connection_id} className="ld-mcp-connection" data-status={connection.status}>
			<div><strong>{connection.client_id}</strong><span className="ld-desktop-chip" data-status={connection.status}>{connection.status.replaceAll("_", " ")}</span></div>
			<dl>
				<div><dt>{t.t("mcp.agent")}</dt><dd>{connection.agent_id}</dd></div>
				<div><dt>{t.t("mcp.scopes")}</dt><dd>{scopeText(t, connection)}</dd></div>
				<div><dt>{t.t("mcp.capabilities")}</dt><dd>{connection.capabilities.join(", ") || t.t("mcp.none")}</dd></div>
				<div><dt>{t.t("mcp.expires")}</dt><dd>{t.formatDate(connection.expires_at)}</dd></div>
			</dl>
			{connection.permissions.propose && !connection.permissions.apply ? <p className="ld-mcp-proposal">{t.t("mcp.proposalOnly")}</p> : null}
			{connection.status === "connected" ? <button type="button" disabled={busy} onClick={() => { void run(() => mcp.revokeConnection(connection.connection_id)); }}>{t.t("mcp.revoke")}</button> : null}
		</li>
	));

	return (
		<section className="ld-mcp-panel" aria-label={t.t("mcp.title")}>
			<div className="ld-mcp-heading">
				<div><h2>{t.t("mcp.title")}</h2></div>
				<div>
					<button type="button" disabled={busy || status === undefined} aria-pressed={status?.enabled ?? false} onClick={() => { void run(() => mcp.setEnabled(!(status?.enabled ?? false))); }}>{status?.enabled ? t.t("mcp.disable") : t.t("mcp.enable")}</button>
					<button type="button" disabled={busy || status?.enabled !== true} onClick={() => { void run(() => mcp.restart()); }}>{t.t("mcp.restart")}</button>
				</div>
			</div>
			{status?.enabled ? <p className="ld-mcp-instructions"><strong>{t.t("mcp.instructions")}</strong><code>{status.instructions}</code></p> : <p>{t.t("mcp.off")}</p>}
			{status?.enabled ? (
				<form onSubmit={(event) => { event.preventDefault(); void run(() => mcp.createConnection({ client_id: clientID, protocol_version: protocolVersion, document_id: projectID, agent_id: agentID, capabilities, permissions, expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(), confirm_apply: confirmApply })); }} aria-label={t.t("mcp.connectForm")}>
					<label>{t.t("mcp.clientName")}<input required value={clientID} onChange={(event) => setClientID(event.currentTarget.value)} /></label>
					<label>{t.t("mcp.agentIdentity")}<input required value={agentID} onChange={(event) => setAgentID(event.currentTarget.value)} /></label>
					<fieldset>
						<legend>{t.t("mcp.delegatedScopes")}</legend>
						{(["read", "export", "propose", "apply"] as const).map((scope) => (
							<label key={scope}>
								<input type="checkbox" checked={permissions[scope]} onChange={(event) => setPermissions({ ...permissions, [scope]: event.currentTarget.checked })} />
								{t.t(`mcp.scope.${scope}`)}
							</label>
						))}
					</fieldset>
					<fieldset>
						<legend>{t.t("mcp.authoringCapabilities")}</legend>
						{capabilityOptions.map((capability) => (
							<label key={capability}>
								<input type="checkbox" checked={capabilities.includes(capability)} onChange={(event) => setCapabilities(event.currentTarget.checked ? [...capabilities, capability] : capabilities.filter((value) => value !== capability))} />
								{capability}
							</label>
						))}
					</fieldset>
					{permissions.apply ? (
						<label className="ld-mcp-confirm">
							<input type="checkbox" required checked={confirmApply} onChange={(event) => setConfirmApply(event.currentTarget.checked)} />
							{t.t("mcp.confirmApply")}
						</label>
					) : null}
					<button type="submit" disabled={busy || !permissions.read || (permissions.apply && !confirmApply)}>{t.t("mcp.connect")}</button>
				</form>
			) : null}
			<p role="status" aria-live="polite">{notice}</p>
			<ul className="ld-mcp-connections">{connectionItems}</ul>
		</section>
	);
}
