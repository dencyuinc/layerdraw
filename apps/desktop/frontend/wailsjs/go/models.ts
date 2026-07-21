// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

export namespace access {
	
	export class AgentPermissions {
	    read: boolean;
	    export: boolean;
	    propose: boolean;
	    apply: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentPermissions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.read = source["read"];
	        this.export = source["export"];
	        this.propose = source["propose"];
	        this.apply = source["apply"];
	    }
	}

}

export namespace accessprotocol {
	
	export class ActorRef {
	    actor_id: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ActorRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.actor_id = source["actor_id"];
	        this.kind = source["kind"];
	    }
	}
	export class ConstraintViolation {
	    action: string;
	    code: string;
	    subject_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConstraintViolation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.code = source["code"];
	        this.subject_address = source["subject_address"];
	    }
	}
	export class AuthoringDecision {
	    access_fingerprint: string;
	    approval_rule_refs: string[];
	    authoring_impact_digest?: string;
	    constraint_violations: ConstraintViolation[];
	    decision_digest: string;
	    diagnostics: protocolcommon.ProtocolDiagnostic[];
	    evaluation_digest: string;
	    host_operation_impact_digests: string[];
	    missing_capabilities: string[];
	    outcome: string;
	    required_capabilities: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuthoringDecision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.access_fingerprint = source["access_fingerprint"];
	        this.approval_rule_refs = source["approval_rule_refs"];
	        this.authoring_impact_digest = source["authoring_impact_digest"];
	        this.constraint_violations = this.convertValues(source["constraint_violations"], ConstraintViolation);
	        this.decision_digest = source["decision_digest"];
	        this.diagnostics = this.convertValues(source["diagnostics"], protocolcommon.ProtocolDiagnostic);
	        this.evaluation_digest = source["evaluation_digest"];
	        this.host_operation_impact_digests = source["host_operation_impact_digests"];
	        this.missing_capabilities = source["missing_capabilities"];
	        this.outcome = source["outcome"];
	        this.required_capabilities = source["required_capabilities"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AuthoringGrantSummary {
	    access_fingerprint: string;
	    constrained_capabilities: string[];
	    expires_at?: string;
	    granted_capabilities: string[];
	    policy_etag: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoringGrantSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.access_fingerprint = source["access_fingerprint"];
	        this.constrained_capabilities = source["constrained_capabilities"];
	        this.expires_at = source["expires_at"];
	        this.granted_capabilities = source["granted_capabilities"];
	        this.policy_etag = source["policy_etag"];
	    }
	}
	
	export class PolicyRef {
	    policy_digest: string;
	    policy_id: string;
	    policy_version: string;
	
	    static createFrom(source: any = {}) {
	        return new PolicyRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.policy_digest = source["policy_digest"];
	        this.policy_id = source["policy_id"];
	        this.policy_version = source["policy_version"];
	    }
	}

}

export namespace desktopapp {
	
	export class BindingResult {
	    outcome: string;
	    value?: desktopcontract.ExchangeResult;
	    failure?: desktopcontract.Failure;
	
	    static createFrom(source: any = {}) {
	        return new BindingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopcontract.ExchangeResult);
	        this.failure = this.convertValues(source["failure"], desktopcontract.Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExternalBackendBinding {
	    binding_id: string;
	    connection_id: string;
	    document_id: string;
	    remote_item_id: string;
	    provider_version: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalBackendBinding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.binding_id = source["binding_id"];
	        this.connection_id = source["connection_id"];
	        this.document_id = source["document_id"];
	        this.remote_item_id = source["remote_item_id"];
	        this.provider_version = source["provider_version"];
	    }
	}
	export class ExternalProviderCapability {
	    open: boolean;
	    conditional_write: boolean;
	    lease: boolean;
	    move_detection: boolean;
	    resumable_upload: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExternalProviderCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.open = source["open"];
	        this.conditional_write = source["conditional_write"];
	        this.lease = source["lease"];
	        this.move_detection = source["move_detection"];
	        this.resumable_upload = source["resumable_upload"];
	    }
	}
	export class ExternalConnection {
	    connection_id: string;
	    provider_id: string;
	    account_label: string;
	    scope_label: string;
	    status: string;
	    capabilities: ExternalProviderCapability;
	
	    static createFrom(source: any = {}) {
	        return new ExternalConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_id = source["connection_id"];
	        this.provider_id = source["provider_id"];
	        this.account_label = source["account_label"];
	        this.scope_label = source["scope_label"];
	        this.status = source["status"];
	        this.capabilities = this.convertValues(source["capabilities"], ExternalProviderCapability);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExternalConnectionRequest {
	    provider_id: string;
	    credential_ref: desktopcontract.CredentialRef;
	    account_label: string;
	    scope_label: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalConnectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider_id = source["provider_id"];
	        this.credential_ref = this.convertValues(source["credential_ref"], desktopcontract.CredentialRef);
	        this.account_label = source["account_label"];
	        this.scope_label = source["scope_label"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExternalImportRequest {
	    request_id: string;
	    profile: string;
	    extension: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.profile = source["profile"];
	        this.extension = source["extension"];
	    }
	}
	export class ExternalLease {
	    token: string;
	    expires_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalLease(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.token = source["token"];
	        this.expires_at = source["expires_at"];
	    }
	}
	
	export class ExternalReconcilePlan {
	    plan_id: string;
	    binding: ExternalBackendBinding;
	    kind: string;
	    local_revision: runtimeprotocol.CommittedRevisionRef;
	    provider_version: string;
	    requires_review: boolean;
	    restricted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExternalReconcilePlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan_id = source["plan_id"];
	        this.binding = this.convertValues(source["binding"], ExternalBackendBinding);
	        this.kind = source["kind"];
	        this.local_revision = this.convertValues(source["local_revision"], runtimeprotocol.CommittedRevisionRef);
	        this.provider_version = source["provider_version"];
	        this.requires_review = source["requires_review"];
	        this.restricted = source["restricted"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExternalReconcileResult {
	    provider_version: string;
	    converged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExternalReconcileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider_version = source["provider_version"];
	        this.converged = source["converged"];
	    }
	}
	export class ExternalRemoteSelectionRequest {
	    connection_id: string;
	    document_id: string;
	    selection_token: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalRemoteSelectionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_id = source["connection_id"];
	        this.document_id = source["document_id"];
	        this.selection_token = source["selection_token"];
	    }
	}
	export class ExternalSyncRequest {
	    connection_id: string;
	    document_id: string;
	    revision: runtimeprotocol.CommittedRevisionRef;
	
	    static createFrom(source: any = {}) {
	        return new ExternalSyncRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_id = source["connection_id"];
	        this.document_id = source["document_id"];
	        this.revision = this.convertValues(source["revision"], runtimeprotocol.CommittedRevisionRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LibraryProjectContextDTO {
	    project_id: string;
	    revision: string;
	    definition_hash: string;
	    resolved_lock_digest: string;
	    dependency_snapshot: registry.ProjectDependencySnapshot;
	
	    static createFrom(source: any = {}) {
	        return new LibraryProjectContextDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.revision = source["revision"];
	        this.definition_hash = source["definition_hash"];
	        this.resolved_lock_digest = source["resolved_lock_digest"];
	        this.dependency_snapshot = this.convertValues(source["dependency_snapshot"], registry.ProjectDependencySnapshot);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MCPConnectRequest {
	    client_id: string;
	    protocol_version: string;
	    document_id: string;
	    agent_id: string;
	    capabilities: string[];
	    permissions: access.AgentPermissions;
	    expires_at: string;
	    confirm_apply: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPConnectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.client_id = source["client_id"];
	        this.protocol_version = source["protocol_version"];
	        this.document_id = source["document_id"];
	        this.agent_id = source["agent_id"];
	        this.capabilities = source["capabilities"];
	        this.permissions = this.convertValues(source["permissions"], access.AgentPermissions);
	        this.expires_at = source["expires_at"];
	        this.confirm_apply = source["confirm_apply"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MCPConnection {
	    connection_id: string;
	    client_id: string;
	    session_id: string;
	    protocol_version: string;
	    document_id: string;
	    delegation_id: string;
	    agent_id: string;
	    capabilities: string[];
	    grant_summary: accessprotocol.AuthoringGrantSummary;
	    permissions: access.AgentPermissions;
	    expires_at: string;
	    generation: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connection_id = source["connection_id"];
	        this.client_id = source["client_id"];
	        this.session_id = source["session_id"];
	        this.protocol_version = source["protocol_version"];
	        this.document_id = source["document_id"];
	        this.delegation_id = source["delegation_id"];
	        this.agent_id = source["agent_id"];
	        this.capabilities = source["capabilities"];
	        this.grant_summary = this.convertValues(source["grant_summary"], accessprotocol.AuthoringGrantSummary);
	        this.permissions = this.convertValues(source["permissions"], access.AgentPermissions);
	        this.expires_at = source["expires_at"];
	        this.generation = source["generation"];
	        this.status = source["status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MCPStatus {
	    enabled: boolean;
	    transport: string;
	    instructions: string;
	    generation: number;
	
	    static createFrom(source: any = {}) {
	        return new MCPStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.transport = source["transport"];
	        this.instructions = source["instructions"];
	        this.generation = source["generation"];
	    }
	}
	export class NativeArtifactRef {
	    artifact_id: string;
	    logical_path: string;
	    media_type: string;
	    content_digest: string;
	
	    static createFrom(source: any = {}) {
	        return new NativeArtifactRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact_id = source["artifact_id"];
	        this.logical_path = source["logical_path"];
	        this.media_type = source["media_type"];
	        this.content_digest = source["content_digest"];
	    }
	}
	export class NativePublishRequest {
	    request_id: string;
	    artifact_id: string;
	    extension: string;
	
	    static createFrom(source: any = {}) {
	        return new NativePublishRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.request_id = source["request_id"];
	        this.artifact_id = source["artifact_id"];
	        this.extension = source["extension"];
	    }
	}
	export class NativePublishResult {
	    published: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NativePublishResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.published = source["published"];
	    }
	}
	export class NativeSerializeResult {
	    artifact: NativeArtifactRef;
	    source_manifest: number[];
	
	    static createFrom(source: any = {}) {
	        return new NativeSerializeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact = this.convertValues(source["artifact"], NativeArtifactRef);
	        this.source_manifest = source["source_manifest"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RecoveryArtifact {
	    kind: string;
	    payload?: number[];
	    reference?: string;
	
	    static createFrom(source: any = {}) {
	        return new RecoveryArtifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.payload = source["payload"];
	        this.reference = source["reference"];
	    }
	}
	export class ProjectOpenResult {
	    open: runtimeprotocol.OpenRuntimeDocumentResult;
	    history: runtimeprotocol.RevisionPage;
	    project_id: string;
	    disposition: string;
	    reconcile_pending: boolean;
	    recovery?: RecoveryArtifact;
	
	    static createFrom(source: any = {}) {
	        return new ProjectOpenResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.open = this.convertValues(source["open"], runtimeprotocol.OpenRuntimeDocumentResult);
	        this.history = this.convertValues(source["history"], runtimeprotocol.RevisionPage);
	        this.project_id = source["project_id"];
	        this.disposition = source["disposition"];
	        this.reconcile_pending = source["reconcile_pending"];
	        this.recovery = this.convertValues(source["recovery"], RecoveryArtifact);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectViewDTO {
	    address: string;
	    label: string;
	    shape: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectViewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.label = source["label"];
	        this.shape = source["shape"];
	    }
	}
	export class ProjectPublicationContext {
	    project_id: string;
	    session_generation: number;
	    display_name: string;
	    authoritative_revision: runtimeprotocol.CommittedRevisionRef;
	    open_input: runtimeprotocol.OpenRuntimeDocumentInput;
	    persistence: string;
	    views: ProjectViewDTO[];
	    library_project: LibraryProjectContextDTO;
	
	    static createFrom(source: any = {}) {
	        return new ProjectPublicationContext(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.session_generation = source["session_generation"];
	        this.display_name = source["display_name"];
	        this.authoritative_revision = this.convertValues(source["authoritative_revision"], runtimeprotocol.CommittedRevisionRef);
	        this.open_input = this.convertValues(source["open_input"], runtimeprotocol.OpenRuntimeDocumentInput);
	        this.persistence = source["persistence"];
	        this.views = this.convertValues(source["views"], ProjectViewDTO);
	        this.library_project = this.convertValues(source["library_project"], LibraryProjectContextDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectPublicationDTO {
	    phase: string;
	    project?: ProjectPublicationContext;
	
	    static createFrom(source: any = {}) {
	        return new ProjectPublicationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.project = this.convertValues(source["project"], ProjectPublicationContext);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RecentProject {
	    project_id: string;
	    display_name: string;
	    location_label?: string;
	    pinned: boolean;
	    last_opened_at: string;
	    availability: string;
	
	    static createFrom(source: any = {}) {
	        return new RecentProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.display_name = source["display_name"];
	        this.location_label = source["location_label"];
	        this.pinned = source["pinned"];
	        this.last_opened_at = source["last_opened_at"];
	        this.availability = source["availability"];
	    }
	}
	
	export class ReviewApprovalRequest {
	    proposal_id: string;
	    generation: number;
	
	    static createFrom(source: any = {}) {
	        return new ReviewApprovalRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proposal_id = source["proposal_id"];
	        this.generation = source["generation"];
	    }
	}

}

export namespace desktopcontract {
	
	export class AccessibilityReport {
	    labels_complete: boolean;
	    screen_reader_semantics: boolean;
	    focus_order_valid: boolean;
	    focus_order_failures?: string;
	    keyboard_workflow_valid: boolean;
	    reduced_motion_honored: boolean;
	    minimum_contrast: number;
	    minimum_contrast_target?: string;
	    zoom_layout_valid: boolean;
	    viewport_width?: number;
	    viewport_height?: number;
	    viewer_mode?: string;
	    renderer_backend?: string;
	    viewer_item_count?: number;
	    viewer_relation_count?: number;
	    viewer_cross_layer_relation_count?: number;
	    viewer_keyboard_selection: boolean;
	    webgl_verified: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AccessibilityReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.labels_complete = source["labels_complete"];
	        this.screen_reader_semantics = source["screen_reader_semantics"];
	        this.focus_order_valid = source["focus_order_valid"];
	        this.focus_order_failures = source["focus_order_failures"];
	        this.keyboard_workflow_valid = source["keyboard_workflow_valid"];
	        this.reduced_motion_honored = source["reduced_motion_honored"];
	        this.minimum_contrast = source["minimum_contrast"];
	        this.minimum_contrast_target = source["minimum_contrast_target"];
	        this.zoom_layout_valid = source["zoom_layout_valid"];
	        this.viewport_width = source["viewport_width"];
	        this.viewport_height = source["viewport_height"];
	        this.viewer_mode = source["viewer_mode"];
	        this.renderer_backend = source["renderer_backend"];
	        this.viewer_item_count = source["viewer_item_count"];
	        this.viewer_relation_count = source["viewer_relation_count"];
	        this.viewer_cross_layer_relation_count = source["viewer_cross_layer_relation_count"];
	        this.viewer_keyboard_selection = source["viewer_keyboard_selection"];
	        this.webgl_verified = source["webgl_verified"];
	    }
	}
	export class Blob {
	    blob_id: string;
	    bytes: number[];
	
	    static createFrom(source: any = {}) {
	        return new Blob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.blob_id = source["blob_id"];
	        this.bytes = source["bytes"];
	    }
	}
	export class CommandInvocation {
	    id: string;
	    source: string;
	    status_generation: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandInvocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.status_generation = source["status_generation"];
	    }
	}
	export class CommandStatus {
	    id: string;
	    state: string;
	    generation: string;
	
	    static createFrom(source: any = {}) {
	        return new CommandStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.state = source["state"];
	        this.generation = source["generation"];
	    }
	}
	export class CredentialRef {
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new CredentialRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class DesktopSettings {
	    schema_version: number;
	    theme: string;
	    zoom_percent: number;
	
	    static createFrom(source: any = {}) {
	        return new DesktopSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.theme = source["theme"];
	        this.zoom_percent = source["zoom_percent"];
	    }
	}
	export class Exchange {
	    operation: string;
	    control: number[];
	    blobs: Blob[];
	
	    static createFrom(source: any = {}) {
	        return new Exchange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation = source["operation"];
	        this.control = source["control"];
	        this.blobs = this.convertValues(source["blobs"], Blob);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExchangeResult {
	    operation: string;
	    control: number[];
	    blobs: Blob[];
	
	    static createFrom(source: any = {}) {
	        return new ExchangeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation = source["operation"];
	        this.control = source["control"];
	        this.blobs = this.convertValues(source["blobs"], Blob);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Failure {
	    code: string;
	    retryable: boolean;
	    component: string;
	    recovery: string;
	
	    static createFrom(source: any = {}) {
	        return new Failure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.retryable = source["retryable"];
	        this.component = source["component"];
	        this.recovery = source["recovery"];
	    }
	}
	export class Result___github_com_dencyuinc_layerdraw_internal_desktopapp_RecentProject_ {
	    outcome: string;
	    value?: desktopapp.RecentProject[];
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result___github_com_dencyuinc_layerdraw_internal_desktopapp_RecentProject_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.RecentProject);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___github_com_dencyuinc_layerdraw_internal_desktopcontract_CommandStatus_ {
	    outcome: string;
	    value?: CommandStatus[];
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result___github_com_dencyuinc_layerdraw_internal_desktopcontract_CommandStatus_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], CommandStatus);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result___github_com_dencyuinc_layerdraw_internal_exporter_Profile_ {
	    outcome: string;
	    value?: exporter.Profile[];
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result___github_com_dencyuinc_layerdraw_internal_exporter_Profile_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], exporter.Profile);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_gen_go_runtimeprotocol_CloseDocumentResult_ {
	    outcome: string;
	    value?: runtimeprotocol.CloseDocumentResult;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_gen_go_runtimeprotocol_CloseDocumentResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], runtimeprotocol.CloseDocumentResult);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalBackendBinding_ {
	    outcome: string;
	    value?: desktopapp.ExternalBackendBinding;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalBackendBinding_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ExternalBackendBinding);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalConnection_ {
	    outcome: string;
	    value?: desktopapp.ExternalConnection;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalConnection_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ExternalConnection);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalLease_ {
	    outcome: string;
	    value?: desktopapp.ExternalLease;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalLease_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ExternalLease);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalReconcilePlan_ {
	    outcome: string;
	    value?: desktopapp.ExternalReconcilePlan;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalReconcilePlan_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ExternalReconcilePlan);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalReconcileResult_ {
	    outcome: string;
	    value?: desktopapp.ExternalReconcileResult;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ExternalReconcileResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ExternalReconcileResult);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_MCPConnection_ {
	    outcome: string;
	    value?: desktopapp.MCPConnection;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_MCPConnection_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.MCPConnection);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_MCPStatus_ {
	    outcome: string;
	    value?: desktopapp.MCPStatus;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_MCPStatus_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.MCPStatus);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_NativePublishResult_ {
	    outcome: string;
	    value?: desktopapp.NativePublishResult;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_NativePublishResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.NativePublishResult);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_NativeSerializeResult_ {
	    outcome: string;
	    value?: desktopapp.NativeSerializeResult;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_NativeSerializeResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.NativeSerializeResult);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ProjectOpenResult_ {
	    outcome: string;
	    value?: desktopapp.ProjectOpenResult;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopapp_ProjectOpenResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], desktopapp.ProjectOpenResult);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopcontract_CommandStatus_ {
	    outcome: string;
	    value?: CommandStatus;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopcontract_CommandStatus_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], CommandStatus);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_desktopcontract_DesktopSettings_ {
	    outcome: string;
	    value?: DesktopSettings;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_desktopcontract_DesktopSettings_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], DesktopSettings);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_github_com_dencyuinc_layerdraw_internal_exporter_ImportPreview_ {
	    outcome: string;
	    value?: exporter.ImportPreview;
	    failure?: Failure;
	
	    static createFrom(source: any = {}) {
	        return new Result_github_com_dencyuinc_layerdraw_internal_exporter_ImportPreview_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outcome = source["outcome"];
	        this.value = this.convertValues(source["value"], exporter.ImportPreview);
	        this.failure = this.convertValues(source["failure"], Failure);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace desktopwails {
	
	export class ProjectViewMaterialization {
	    view_data: semantic.ViewData;
	    view_data_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectViewMaterialization(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.view_data = this.convertValues(source["view_data"], semantic.ViewData);
	        this.view_data_hash = source["view_data_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ReviewCommentRequest {
	    proposal_id: string;
	    generation: number;
	    comment_id: string;
	    body: string;
	    target: review.Target;
	
	    static createFrom(source: any = {}) {
	        return new ReviewCommentRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proposal_id = source["proposal_id"];
	        this.generation = source["generation"];
	        this.comment_id = source["comment_id"];
	        this.body = source["body"];
	        this.target = this.convertValues(source["target"], review.Target);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ReviewWithdrawRequest {
	    proposal_id: string;
	    generation: number;
	
	    static createFrom(source: any = {}) {
	        return new ReviewWithdrawRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proposal_id = source["proposal_id"];
	        this.generation = source["generation"];
	    }
	}

}

export namespace engineprotocol {
	
	export class ColumnCreateSubjectFields {
	    default?: semantic.RecipeScalar;
	    display_name: string;
	    enum_values?: string[];
	    format?: string;
	    max?: string;
	    max_length?: string;
	    min?: string;
	    min_length?: string;
	    required?: boolean;
	    reserved_enum_values?: string[];
	    value_type: string;
	
	    static createFrom(source: any = {}) {
	        return new ColumnCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default = this.convertValues(source["default"], semantic.RecipeScalar);
	        this.display_name = source["display_name"];
	        this.enum_values = source["enum_values"];
	        this.format = source["format"];
	        this.max = source["max"];
	        this.max_length = source["max_length"];
	        this.min = source["min"];
	        this.min_length = source["min_length"];
	        this.required = source["required"];
	        this.reserved_enum_values = source["reserved_enum_values"];
	        this.value_type = source["value_type"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConstraintCreateSubjectFields {
	    column_addresses: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConstraintCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_addresses = source["column_addresses"];
	    }
	}
	export class PlacementHint {
	    group_anchor_address?: string;
	    module_path?: string;
	    position: string;
	
	    static createFrom(source: any = {}) {
	        return new PlacementHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group_anchor_address = source["group_anchor_address"];
	        this.module_path = source["module_path"];
	        this.position = source["position"];
	    }
	}
	export class SemanticStringMapEntry {
	    key: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new SemanticStringMapEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class EntityCreateSubjectFields {
	    annotations?: SemanticStringMapEntry[];
	    description?: string;
	    display_name: string;
	    layer_address: string;
	    tags?: string[];
	    type_address: string;
	
	    static createFrom(source: any = {}) {
	        return new EntityCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.layer_address = source["layer_address"];
	        this.tags = source["tags"];
	        this.type_address = source["type_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateEntityOperation {
	    fields: EntityCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateEntityOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], EntityCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateEntityTypeColumnOperation {
	    fields: ColumnCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateEntityTypeColumnOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ColumnCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateEntityTypeConstraintOperation {
	    fields: ConstraintCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateEntityTypeConstraintOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ConstraintCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EntityTypeCreateSubjectFields {
	    annotations?: SemanticStringMapEntry[];
	    color?: string;
	    description?: string;
	    display_name: string;
	    icon?: string;
	    image?: semantic.AuthoredAssetRef;
	    representation: semantic.AuthoredEntityRepresentation;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new EntityTypeCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.color = source["color"];
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.icon = source["icon"];
	        this.image = this.convertValues(source["image"], semantic.AuthoredAssetRef);
	        this.representation = this.convertValues(source["representation"], semantic.AuthoredEntityRepresentation);
	        this.tags = source["tags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateEntityTypeOperation {
	    fields: EntityTypeCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateEntityTypeOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], EntityTypeCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LayerCreateSubjectFields {
	    annotations?: SemanticStringMapEntry[];
	    description?: string;
	    display_name: string;
	    order: string;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new LayerCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.order = source["order"];
	        this.tags = source["tags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateLayerOperation {
	    fields: LayerCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateLayerOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], LayerCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryCreateSubjectFields {
	    annotations?: SemanticStringMapEntry[];
	    description?: string;
	    display_name: string;
	    relation_where?: semantic.RecipePredicate;
	    result?: string[];
	    select: semantic.QueryRecipeSelect;
	    state_input?: string;
	    tags?: string[];
	    traverse?: semantic.QueryRecipeTraversal;
	    where?: semantic.RecipePredicate;
	
	    static createFrom(source: any = {}) {
	        return new QueryCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.relation_where = this.convertValues(source["relation_where"], semantic.RecipePredicate);
	        this.result = source["result"];
	        this.select = this.convertValues(source["select"], semantic.QueryRecipeSelect);
	        this.state_input = source["state_input"];
	        this.tags = source["tags"];
	        this.traverse = this.convertValues(source["traverse"], semantic.QueryRecipeTraversal);
	        this.where = this.convertValues(source["where"], semantic.RecipePredicate);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateQueryOperation {
	    fields: QueryCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateQueryOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], QueryCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryParameterCreateSubjectFields {
	    default?: semantic.RecipeScalar;
	    enum_values?: string[];
	    format?: string;
	    max?: string;
	    max_length?: string;
	    min?: string;
	    min_length?: string;
	    required?: boolean;
	    reserved_enum_values?: string[];
	    value_type: string;
	
	    static createFrom(source: any = {}) {
	        return new QueryParameterCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default = this.convertValues(source["default"], semantic.RecipeScalar);
	        this.enum_values = source["enum_values"];
	        this.format = source["format"];
	        this.max = source["max"];
	        this.max_length = source["max_length"];
	        this.min = source["min"];
	        this.min_length = source["min_length"];
	        this.required = source["required"];
	        this.reserved_enum_values = source["reserved_enum_values"];
	        this.value_type = source["value_type"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateQueryParameterOperation {
	    fields: QueryParameterCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateQueryParameterOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], QueryParameterCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ReferenceCreateSubjectFields {
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ReferenceCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	    }
	}
	export class CreateReferenceOperation {
	    fields: ReferenceCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateReferenceOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ReferenceCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateRelationTypeColumnOperation {
	    fields: ColumnCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateRelationTypeColumnOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ColumnCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateRelationTypeConstraintOperation {
	    fields: ConstraintCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateRelationTypeConstraintOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ConstraintCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RelationTypeCreateSubjectFields {
	    allow_self?: boolean;
	    annotations?: SemanticStringMapEntry[];
	    cardinality?: semantic.AuthoredRelationCardinality;
	    description?: string;
	    display_name: string;
	    duplicate_policy?: string;
	    export?: semantic.AuthoredRelationExport;
	    forward_label: string;
	    from: semantic.AuthoredRelationEndpointRule;
	    projections?: semantic.AuthoredRelationProjectionSet;
	    render?: semantic.AuthoredRelationRenderSet;
	    reverse_label?: string;
	    semantic_kind: string;
	    tags?: string[];
	    to: semantic.AuthoredRelationEndpointRule;
	    traversal?: semantic.AuthoredRelationTraversalPolicy;
	
	    static createFrom(source: any = {}) {
	        return new RelationTypeCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allow_self = source["allow_self"];
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.cardinality = this.convertValues(source["cardinality"], semantic.AuthoredRelationCardinality);
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.duplicate_policy = source["duplicate_policy"];
	        this.export = this.convertValues(source["export"], semantic.AuthoredRelationExport);
	        this.forward_label = source["forward_label"];
	        this.from = this.convertValues(source["from"], semantic.AuthoredRelationEndpointRule);
	        this.projections = this.convertValues(source["projections"], semantic.AuthoredRelationProjectionSet);
	        this.render = this.convertValues(source["render"], semantic.AuthoredRelationRenderSet);
	        this.reverse_label = source["reverse_label"];
	        this.semantic_kind = source["semantic_kind"];
	        this.tags = source["tags"];
	        this.to = this.convertValues(source["to"], semantic.AuthoredRelationEndpointRule);
	        this.traversal = this.convertValues(source["traversal"], semantic.AuthoredRelationTraversalPolicy);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateRelationTypeOperation {
	    fields: RelationTypeCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateRelationTypeOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], RelationTypeCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateSubjectOperation {
	
	
	    static createFrom(source: any = {}) {
	        return new CreateSubjectOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class ViewExportCreateSubjectFields {
	    exporter_profile?: semantic.ExporterProfileRef;
	    fidelity: string;
	    filename: string;
	    format: string;
	    options?: semantic.ExportOptions;
	    source_refs?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ViewExportCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exporter_profile = this.convertValues(source["exporter_profile"], semantic.ExporterProfileRef);
	        this.fidelity = source["fidelity"];
	        this.filename = source["filename"];
	        this.format = source["format"];
	        this.options = this.convertValues(source["options"], semantic.ExportOptions);
	        this.source_refs = source["source_refs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateViewExportOperation {
	    fields: ViewExportCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateViewExportOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ViewExportCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewProjectionOverrideEntry {
	    key: string;
	    value: semantic.AuthoredViewProjectionOverride;
	
	    static createFrom(source: any = {}) {
	        return new ViewProjectionOverrideEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = this.convertValues(source["value"], semantic.AuthoredViewProjectionOverride);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewCreateSubjectFields {
	    annotations?: SemanticStringMapEntry[];
	    category: string;
	    description?: string;
	    display_name: string;
	    intent?: string;
	    relation_projection_overrides?: ViewProjectionOverrideEntry[];
	    shape: semantic.AuthoredViewShape;
	    source: semantic.AuthoredOperationSource;
	    state_input?: string;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.category = source["category"];
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.intent = source["intent"];
	        this.relation_projection_overrides = this.convertValues(source["relation_projection_overrides"], ViewProjectionOverrideEntry);
	        this.shape = this.convertValues(source["shape"], semantic.AuthoredViewShape);
	        this.source = this.convertValues(source["source"], semantic.AuthoredOperationSource);
	        this.state_input = source["state_input"];
	        this.tags = source["tags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateViewOperation {
	    fields: ViewCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateViewOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ViewCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewTableColumnCreateSubjectFields {
	    aggregate?: string;
	    label?: string;
	    source: semantic.AuthoredOperationSource;
	
	    static createFrom(source: any = {}) {
	        return new ViewTableColumnCreateSubjectFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aggregate = source["aggregate"];
	        this.label = source["label"];
	        this.source = this.convertValues(source["source"], semantic.AuthoredOperationSource);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateViewTableColumnOperation {
	    fields: ViewTableColumnCreateSubjectFields;
	    id: string;
	    operation: string;
	    parent_address: string;
	    placement?: PlacementHint;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateViewTableColumnOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], ViewTableColumnCreateSubjectFields);
	        this.id = source["id"];
	        this.operation = source["operation"];
	        this.parent_address = source["parent_address"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DocumentHandle {
	    endpoint_instance_id: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new DocumentHandle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint_instance_id = source["endpoint_instance_id"];
	        this.value = source["value"];
	    }
	}
	export class DocumentGeneration {
	    document_handle: DocumentHandle;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new DocumentGeneration(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document_handle = this.convertValues(source["document_handle"], DocumentHandle);
	        this.value = source["value"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ExpectedHash {
	    address: string;
	    hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ExpectedHash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.hash = source["hash"];
	    }
	}
	export class ExpectedSourceDigest {
	    digest: string;
	    module: semantic.ModuleRef;
	
	    static createFrom(source: any = {}) {
	        return new ExpectedSourceDigest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.digest = source["digest"];
	        this.module = this.convertValues(source["module"], semantic.ModuleRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExpectedChildSet {
	    child_kind: string;
	    hash: string;
	    owner_address: string;
	
	    static createFrom(source: any = {}) {
	        return new ExpectedChildSet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_kind = source["child_kind"];
	        this.hash = source["hash"];
	        this.owner_address = source["owner_address"];
	    }
	}
	export class EngineEditPreconditions {
	    document_generation: DocumentGeneration;
	    expected_child_sets: ExpectedChildSet[];
	    expected_source_digests?: ExpectedSourceDigest[];
	    expected_subject_hashes: ExpectedHash[];
	    expected_subtree_hashes: ExpectedHash[];
	
	    static createFrom(source: any = {}) {
	        return new EngineEditPreconditions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document_generation = this.convertValues(source["document_generation"], DocumentGeneration);
	        this.expected_child_sets = this.convertValues(source["expected_child_sets"], ExpectedChildSet);
	        this.expected_source_digests = this.convertValues(source["expected_source_digests"], ExpectedSourceDigest);
	        this.expected_subject_hashes = this.convertValues(source["expected_subject_hashes"], ExpectedHash);
	        this.expected_subtree_hashes = this.convertValues(source["expected_subtree_hashes"], ExpectedHash);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	export class RowCell {
	    column_address: string;
	    value: SemanticOperationValue;
	
	    static createFrom(source: any = {}) {
	        return new RowCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_address = source["column_address"];
	        this.value = this.convertValues(source["value"], SemanticOperationValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SemanticOperationMapEntry {
	    key: string;
	    value: SemanticOperationValue;
	
	    static createFrom(source: any = {}) {
	        return new SemanticOperationMapEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = this.convertValues(source["value"], SemanticOperationValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SemanticOperationValue {
	    address?: string;
	    array?: SemanticOperationValue[];
	    blob?: protocolcommon.BlobRef;
	    boolean?: boolean;
	    decimal?: string;
	    integer?: string;
	    kind: string;
	    map?: SemanticOperationMapEntry[];
	    string?: string;
	
	    static createFrom(source: any = {}) {
	        return new SemanticOperationValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.array = this.convertValues(source["array"], SemanticOperationValue);
	        this.blob = this.convertValues(source["blob"], protocolcommon.BlobRef);
	        this.boolean = source["boolean"];
	        this.decimal = source["decimal"];
	        this.integer = source["integer"];
	        this.kind = source["kind"];
	        this.map = this.convertValues(source["map"], SemanticOperationMapEntry);
	        this.string = source["string"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RelationCreateFields {
	    annotations?: SemanticStringMapEntry[];
	    description?: string;
	    display_name?: string;
	    tags?: string[];
	
	    static createFrom(source: any = {}) {
	        return new RelationCreateFields(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.annotations = this.convertValues(source["annotations"], SemanticStringMapEntry);
	        this.description = source["description"];
	        this.display_name = source["display_name"];
	        this.tags = source["tags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NonCreateSemanticOperation {
	    action?: string;
	    endpoint?: string;
	    entity_address?: string;
	    explicit_absent_column_addresses?: string[];
	    fields?: RelationCreateFields;
	    from_address?: string;
	    id?: string;
	    layer_address?: string;
	    new_id?: string;
	    new_project_id?: string;
	    operation: string;
	    owner_address?: string;
	    parent_address?: string;
	    path?: string[];
	    placement?: PlacementHint;
	    project_address?: string;
	    relation_address?: string;
	    row_address?: string;
	    row_id?: string;
	    target_address?: string;
	    to_address?: string;
	    type_address?: string;
	    value?: SemanticOperationValue;
	    values?: RowCell[];
	
	    static createFrom(source: any = {}) {
	        return new NonCreateSemanticOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.endpoint = source["endpoint"];
	        this.entity_address = source["entity_address"];
	        this.explicit_absent_column_addresses = source["explicit_absent_column_addresses"];
	        this.fields = this.convertValues(source["fields"], RelationCreateFields);
	        this.from_address = source["from_address"];
	        this.id = source["id"];
	        this.layer_address = source["layer_address"];
	        this.new_id = source["new_id"];
	        this.new_project_id = source["new_project_id"];
	        this.operation = source["operation"];
	        this.owner_address = source["owner_address"];
	        this.parent_address = source["parent_address"];
	        this.path = source["path"];
	        this.placement = this.convertValues(source["placement"], PlacementHint);
	        this.project_address = source["project_address"];
	        this.relation_address = source["relation_address"];
	        this.row_address = source["row_address"];
	        this.row_id = source["row_id"];
	        this.target_address = source["target_address"];
	        this.to_address = source["to_address"];
	        this.type_address = source["type_address"];
	        this.value = this.convertValues(source["value"], SemanticOperationValue);
	        this.values = this.convertValues(source["values"], RowCell);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PreviewID {
	    endpoint_instance_id: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new PreviewID(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint_instance_id = source["endpoint_instance_id"];
	        this.value = source["value"];
	    }
	}
	
	
	
	
	
	export class ResultingHashes {
	    child_set_hashes: semantic.ChildSetHash[];
	    definition_hash: string;
	    graph_hash?: string;
	    mode: string;
	    pack_address?: string;
	    project_address?: string;
	    subject_hashes: semantic.SubjectHash[];
	    subtree_hashes: semantic.SubtreeHash[];
	
	    static createFrom(source: any = {}) {
	        return new ResultingHashes(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_set_hashes = this.convertValues(source["child_set_hashes"], semantic.ChildSetHash);
	        this.definition_hash = source["definition_hash"];
	        this.graph_hash = source["graph_hash"];
	        this.mode = source["mode"];
	        this.pack_address = source["pack_address"];
	        this.project_address = source["project_address"];
	        this.subject_hashes = this.convertValues(source["subject_hashes"], semantic.SubjectHash);
	        this.subtree_hashes = this.convertValues(source["subtree_hashes"], semantic.SubtreeHash);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SemanticConflict {
	    child_kind?: string;
	    kind: string;
	    owner_address?: string;
	    path?: string[];
	    target_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new SemanticConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_kind = source["child_kind"];
	        this.kind = source["kind"];
	        this.owner_address = source["owner_address"];
	        this.path = source["path"];
	        this.target_address = source["target_address"];
	    }
	}
	export class SemanticOperation {
	
	
	    static createFrom(source: any = {}) {
	        return new SemanticOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SemanticOperationBatch {
	    operations: SemanticOperation[];
	
	    static createFrom(source: any = {}) {
	        return new SemanticOperationBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operations = this.convertValues(source["operations"], SemanticOperation);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SourceEdit {
	    after_digest?: string;
	    after_module?: semantic.ModuleRef;
	    before_digest?: string;
	    before_module?: semantic.ModuleRef;
	    kind: string;
	    replacement_blob?: protocolcommon.BlobRef;
	    source_range?: semantic.SourceRange;
	
	    static createFrom(source: any = {}) {
	        return new SourceEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after_digest = source["after_digest"];
	        this.after_module = this.convertValues(source["after_module"], semantic.ModuleRef);
	        this.before_digest = source["before_digest"];
	        this.before_module = this.convertValues(source["before_module"], semantic.ModuleRef);
	        this.kind = source["kind"];
	        this.replacement_blob = this.convertValues(source["replacement_blob"], protocolcommon.BlobRef);
	        this.source_range = this.convertValues(source["source_range"], semantic.SourceRange);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SourceDiff {
	    digest: string;
	    edits: SourceEdit[];
	
	    static createFrom(source: any = {}) {
	        return new SourceDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.digest = source["digest"];
	        this.edits = this.convertValues(source["edits"], SourceEdit);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	export class WorkbenchPreviewResult {
	    authoring_impact?: semantic.AuthoringImpact;
	    authoring_impact_digest?: string;
	    base_generation: DocumentGeneration;
	    changed_source_files: semantic.ModuleRef[];
	    conflicts: SemanticConflict[];
	    diagnostics: semantic.Diagnostic[];
	    preview_digest?: string;
	    preview_id?: PreviewID;
	    proposed_generation?: DocumentGeneration;
	    required_authoring_capabilities?: string[];
	    resulting_hashes?: ResultingHashes;
	    semantic_diff: semantic.SemanticDiff;
	    source_diff: SourceDiff;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkbenchPreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authoring_impact = this.convertValues(source["authoring_impact"], semantic.AuthoringImpact);
	        this.authoring_impact_digest = source["authoring_impact_digest"];
	        this.base_generation = this.convertValues(source["base_generation"], DocumentGeneration);
	        this.changed_source_files = this.convertValues(source["changed_source_files"], semantic.ModuleRef);
	        this.conflicts = this.convertValues(source["conflicts"], SemanticConflict);
	        this.diagnostics = this.convertValues(source["diagnostics"], semantic.Diagnostic);
	        this.preview_digest = source["preview_digest"];
	        this.preview_id = this.convertValues(source["preview_id"], PreviewID);
	        this.proposed_generation = this.convertValues(source["proposed_generation"], DocumentGeneration);
	        this.required_authoring_capabilities = source["required_authoring_capabilities"];
	        this.resulting_hashes = this.convertValues(source["resulting_hashes"], ResultingHashes);
	        this.semantic_diff = this.convertValues(source["semantic_diff"], semantic.SemanticDiff);
	        this.source_diff = this.convertValues(source["source_diff"], SourceDiff);
	        this.status = source["status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace exporter {
	
	export class Artifact {
	    Role: string;
	    LogicalPath: string;
	    MediaType: string;
	    Primary: boolean;
	    Bytes: number[];
	    ContentDigest: string;
	
	    static createFrom(source: any = {}) {
	        return new Artifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Role = source["Role"];
	        this.LogicalPath = source["LogicalPath"];
	        this.MediaType = source["MediaType"];
	        this.Primary = source["Primary"];
	        this.Bytes = source["Bytes"];
	        this.ContentDigest = source["ContentDigest"];
	    }
	}
	export class ImportPreview {
	    profile: string;
	    media_type: string;
	    batch: engineprotocol.SemanticOperationBatch;
	    source_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profile = source["profile"];
	        this.media_type = source["media_type"];
	        this.batch = this.convertValues(source["batch"], engineprotocol.SemanticOperationBatch);
	        this.source_hash = source["source_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Profile {
	    format: string;
	    schema_version: number;
	    requires_shape: string[];
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.schema_version = source["schema_version"];
	        this.requires_shape = source["requires_shape"];
	    }
	}
	export class Resource {
	    digest: string;
	    bytes: number[];
	
	    static createFrom(source: any = {}) {
	        return new Resource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.digest = source["digest"];
	        this.bytes = source["bytes"];
	    }
	}
	export class Result {
	    Artifacts: Artifact[];
	    SourceManifest: semantic.ExportSourceManifest;
	    SourceManifestJSON: number[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Artifacts = this.convertValues(source["Artifacts"], Artifact);
	        this.SourceManifest = this.convertValues(source["SourceManifest"], semantic.ExportSourceManifest);
	        this.SourceManifestJSON = source["SourceManifestJSON"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SerializeInput {
	    plan: semantic.ExportPlan;
	    view_data: semantic.ViewData;
	    assets: Resource[];
	    fonts: Resource[];
	    max_input_bytes?: number;
	    max_output_bytes?: number;
	
	    static createFrom(source: any = {}) {
	        return new SerializeInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan = this.convertValues(source["plan"], semantic.ExportPlan);
	        this.view_data = this.convertValues(source["view_data"], semantic.ViewData);
	        this.assets = this.convertValues(source["assets"], Resource);
	        this.fonts = this.convertValues(source["fonts"], Resource);
	        this.max_input_bytes = source["max_input_bytes"];
	        this.max_output_bytes = source["max_output_bytes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace localdocument {
	
	export class EditorPreviewResult {
	    preview: engineprotocol.WorkbenchPreviewResult;
	    runtime: runtimeprotocol.PreviewOperationsResult;
	    grant_summary: accessprotocol.AuthoringGrantSummary;
	
	    static createFrom(source: any = {}) {
	        return new EditorPreviewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = this.convertValues(source["preview"], engineprotocol.WorkbenchPreviewResult);
	        this.runtime = this.convertValues(source["runtime"], runtimeprotocol.PreviewOperationsResult);
	        this.grant_summary = this.convertValues(source["grant_summary"], accessprotocol.AuthoringGrantSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace protocolcommon {
	
	export class BlobRef {
	    blob_id: string;
	    digest: string;
	    lifetime: string;
	    media_type: string;
	    size: string;
	
	    static createFrom(source: any = {}) {
	        return new BlobRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.blob_id = source["blob_id"];
	        this.digest = source["digest"];
	        this.lifetime = source["lifetime"];
	        this.media_type = source["media_type"];
	        this.size = source["size"];
	    }
	}
	export class CompileResourceLimitConstraints {
	    max_asset_bytes?: string;
	    max_assets?: string;
	    max_declarations?: string;
	    max_pack_bytes?: string;
	    max_pack_files?: string;
	    max_project_source_bytes?: string;
	    max_project_source_files?: string;
	    max_raster_dimension?: string;
	    max_raster_pixels?: string;
	
	    static createFrom(source: any = {}) {
	        return new CompileResourceLimitConstraints(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_asset_bytes = source["max_asset_bytes"];
	        this.max_assets = source["max_assets"];
	        this.max_declarations = source["max_declarations"];
	        this.max_pack_bytes = source["max_pack_bytes"];
	        this.max_pack_files = source["max_pack_files"];
	        this.max_project_source_bytes = source["max_project_source_bytes"];
	        this.max_project_source_files = source["max_project_source_files"];
	        this.max_raster_dimension = source["max_raster_dimension"];
	        this.max_raster_pixels = source["max_raster_pixels"];
	    }
	}
	export class JsonValue {
	    Kind: string;
	    Boolean: boolean;
	    String: string;
	    Array: JsonValue[];
	    Object: Record<string, JsonValue>;
	
	    static createFrom(source: any = {}) {
	        return new JsonValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Kind = source["Kind"];
	        this.Boolean = source["Boolean"];
	        this.String = source["String"];
	        this.Array = this.convertValues(source["Array"], JsonValue);
	        this.Object = this.convertValues(source["Object"], JsonValue, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OperationCapability {
	    enabled: boolean;
	    extensions?: Record<string, JsonValue>;
	    limits?: CompileResourceLimitConstraints;
	    protocol_version: string;
	    required_authoring_capabilities?: string[];
	    unavailable_reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new OperationCapability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.extensions = this.convertValues(source["extensions"], JsonValue, true);
	        this.limits = this.convertValues(source["limits"], CompileResourceLimitConstraints);
	        this.protocol_version = source["protocol_version"];
	        this.required_authoring_capabilities = source["required_authoring_capabilities"];
	        this.unavailable_reason = source["unavailable_reason"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TotalItems {
	    exact?: string;
	    known: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TotalItems(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exact = source["exact"];
	        this.known = source["known"];
	    }
	}
	export class PageInfo {
	    next_cursor?: string;
	    result_truncated: boolean;
	    returned_bytes: string;
	    returned_items: string;
	    total_items?: TotalItems;
	
	    static createFrom(source: any = {}) {
	        return new PageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.next_cursor = source["next_cursor"];
	        this.result_truncated = source["result_truncated"];
	        this.returned_bytes = source["returned_bytes"];
	        this.returned_items = source["returned_items"];
	        this.total_items = this.convertValues(source["total_items"], TotalItems);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProtocolDiagnosticSpan {
	    end_byte: string;
	    start_byte: string;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolDiagnosticSpan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.end_byte = source["end_byte"];
	        this.start_byte = source["start_byte"];
	    }
	}
	export class ProtocolDiagnosticSource {
	    module_path: string;
	    span: ProtocolDiagnosticSpan;
	    stable_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolDiagnosticSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.module_path = source["module_path"];
	        this.span = this.convertValues(source["span"], ProtocolDiagnosticSpan);
	        this.stable_address = source["stable_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProtocolDiagnosticRelated {
	    message: string;
	    relation: string;
	    source?: ProtocolDiagnosticSource;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolDiagnosticRelated(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.relation = source["relation"];
	        this.source = this.convertValues(source["source"], ProtocolDiagnosticSource);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProtocolDiagnostic {
	    code: string;
	    data?: Record<string, JsonValue>;
	    message: string;
	    related: ProtocolDiagnosticRelated[];
	    remediation?: string;
	    severity: string;
	    source?: ProtocolDiagnosticSource;
	
	    static createFrom(source: any = {}) {
	        return new ProtocolDiagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.data = this.convertValues(source["data"], JsonValue, true);
	        this.message = source["message"];
	        this.related = this.convertValues(source["related"], ProtocolDiagnosticRelated);
	        this.remediation = source["remediation"];
	        this.severity = source["severity"];
	        this.source = this.convertValues(source["source"], ProtocolDiagnosticSource);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	

}

export namespace registry {
	
	export class ArtifactIdentity {
	    kind: string;
	    canonical_id: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new ArtifactIdentity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.canonical_id = source["canonical_id"];
	        this.version = source["version"];
	    }
	}
	export class LockedArtifact {
	    identity: ArtifactIdentity;
	    source_id: string;
	    publisher_id: string;
	    digest: string;
	    provenance_digest: string;
	    dependency_metadata_digest: string;
	    dependencies: ArtifactIdentity[];
	    pinned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LockedArtifact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.identity = this.convertValues(source["identity"], ArtifactIdentity);
	        this.source_id = source["source_id"];
	        this.publisher_id = source["publisher_id"];
	        this.digest = source["digest"];
	        this.provenance_digest = source["provenance_digest"];
	        this.dependency_metadata_digest = source["dependency_metadata_digest"];
	        this.dependencies = this.convertValues(source["dependencies"], ArtifactIdentity);
	        this.pinned = source["pinned"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectDependencySnapshot {
	    resolved_lock_digest: string;
	    installs: LockedArtifact[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectDependencySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resolved_lock_digest = source["resolved_lock_digest"];
	        this.installs = this.convertValues(source["installs"], LockedArtifact);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace review {
	
	export class ArtifactPreview {
	    kind: string;
	    label: string;
	    digest: string;
	    media_type: string;
	
	    static createFrom(source: any = {}) {
	        return new ArtifactPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.digest = source["digest"];
	        this.media_type = source["media_type"];
	    }
	}
	export class Target {
	    kind: string;
	    stable_address?: string;
	    source_range?: semantic.SourceRange;
	    diff_key?: string;
	
	    static createFrom(source: any = {}) {
	        return new Target(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.stable_address = source["stable_address"];
	        this.source_range = this.convertValues(source["source_range"], semantic.SourceRange);
	        this.diff_key = source["diff_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Comment {
	    id: string;
	    author: accessprotocol.ActorRef;
	    body: string;
	    target: Target;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    stale: boolean;
	    base_revision: string;
	
	    static createFrom(source: any = {}) {
	        return new Comment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.author = this.convertValues(source["author"], accessprotocol.ActorRef);
	        this.body = source["body"];
	        this.target = this.convertValues(source["target"], Target);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.stale = source["stale"];
	        this.base_revision = source["base_revision"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Evidence {
	    semantic_diff: semantic.SemanticDiff;
	    source_diff: engineprotocol.SourceDiff;
	    authoring_impact: semantic.AuthoringImpact;
	    diagnostics: semantic.Diagnostic[];
	    affected_usages: string[];
	    affected_rows: string[];
	    affected_views: string[];
	    definition_preview?: ArtifactPreview;
	    render_previews: ArtifactPreview[];
	
	    static createFrom(source: any = {}) {
	        return new Evidence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.semantic_diff = this.convertValues(source["semantic_diff"], semantic.SemanticDiff);
	        this.source_diff = this.convertValues(source["source_diff"], engineprotocol.SourceDiff);
	        this.authoring_impact = this.convertValues(source["authoring_impact"], semantic.AuthoringImpact);
	        this.diagnostics = this.convertValues(source["diagnostics"], semantic.Diagnostic);
	        this.affected_usages = source["affected_usages"];
	        this.affected_rows = source["affected_rows"];
	        this.affected_views = source["affected_views"];
	        this.definition_preview = this.convertValues(source["definition_preview"], ArtifactPreview);
	        this.render_previews = this.convertValues(source["render_previews"], ArtifactPreview);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PendingCommit {
	    operation_id: string;
	    idempotency_key: string;
	    operation_batch: runtimeprotocol.RuntimeOperationBatch;
	    authoring_proof: runtimeprotocol.AuthoringProof;
	    approver: accessprotocol.ActorRef;
	    access_evaluation_digest: string;
	    access_decision_digest: string;
	    trigger: string;
	
	    static createFrom(source: any = {}) {
	        return new PendingCommit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation_id = source["operation_id"];
	        this.idempotency_key = source["idempotency_key"];
	        this.operation_batch = this.convertValues(source["operation_batch"], runtimeprotocol.RuntimeOperationBatch);
	        this.authoring_proof = this.convertValues(source["authoring_proof"], runtimeprotocol.AuthoringProof);
	        this.approver = this.convertValues(source["approver"], accessprotocol.ActorRef);
	        this.access_evaluation_digest = source["access_evaluation_digest"];
	        this.access_decision_digest = source["access_decision_digest"];
	        this.trigger = source["trigger"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Proposal {
	    id: string;
	    generation: number;
	    status: string;
	    current_revision: runtimeprotocol.CommittedRevisionRef;
	    proposed_definition_hash: string;
	    proposed_graph_hash: string;
	    operation_batch: runtimeprotocol.RuntimeOperationBatch;
	    authoring_proof: runtimeprotocol.AuthoringProof;
	    evidence: Evidence;
	    proposer: accessprotocol.ActorRef;
	    agent_delegation_digest?: string;
	    access_evaluation_digest: string;
	    access_decision_digest: string;
	    required_capabilities: string[];
	    comments: Comment[];
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    approved_by?: accessprotocol.ActorRef;
	    committed_revision?: runtimeprotocol.CommittedRevisionRef;
	    pending_commit?: PendingCommit;
	    last_failure?: string;
	
	    static createFrom(source: any = {}) {
	        return new Proposal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.generation = source["generation"];
	        this.status = source["status"];
	        this.current_revision = this.convertValues(source["current_revision"], runtimeprotocol.CommittedRevisionRef);
	        this.proposed_definition_hash = source["proposed_definition_hash"];
	        this.proposed_graph_hash = source["proposed_graph_hash"];
	        this.operation_batch = this.convertValues(source["operation_batch"], runtimeprotocol.RuntimeOperationBatch);
	        this.authoring_proof = this.convertValues(source["authoring_proof"], runtimeprotocol.AuthoringProof);
	        this.evidence = this.convertValues(source["evidence"], Evidence);
	        this.proposer = this.convertValues(source["proposer"], accessprotocol.ActorRef);
	        this.agent_delegation_digest = source["agent_delegation_digest"];
	        this.access_evaluation_digest = source["access_evaluation_digest"];
	        this.access_decision_digest = source["access_decision_digest"];
	        this.required_capabilities = source["required_capabilities"];
	        this.comments = this.convertValues(source["comments"], Comment);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.approved_by = this.convertValues(source["approved_by"], accessprotocol.ActorRef);
	        this.committed_revision = this.convertValues(source["committed_revision"], runtimeprotocol.CommittedRevisionRef);
	        this.pending_commit = this.convertValues(source["pending_commit"], PendingCommit);
	        this.last_failure = source["last_failure"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    version: number;
	    proposals: Proposal[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.proposals = this.convertValues(source["proposals"], Proposal);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace runtimeprotocol {
	
	export class CommittedRevisionRef {
	    definition_hash: string;
	    document_id: string;
	    graph_hash: string;
	    provider_version?: string;
	    revision_id: string;
	
	    static createFrom(source: any = {}) {
	        return new CommittedRevisionRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.definition_hash = source["definition_hash"];
	        this.document_id = source["document_id"];
	        this.graph_hash = source["graph_hash"];
	        this.provider_version = source["provider_version"];
	        this.revision_id = source["revision_id"];
	    }
	}
	export class AuthoringProof {
	    access_fingerprint: string;
	    base_revision: CommittedRevisionRef;
	    decision_digest: string;
	    evaluation_digest: string;
	    expires_at?: string;
	    membership_version: string;
	    policy_refs: accessprotocol.PolicyRef[];
	
	    static createFrom(source: any = {}) {
	        return new AuthoringProof(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.access_fingerprint = source["access_fingerprint"];
	        this.base_revision = this.convertValues(source["base_revision"], CommittedRevisionRef);
	        this.decision_digest = source["decision_digest"];
	        this.evaluation_digest = source["evaluation_digest"];
	        this.expires_at = source["expires_at"];
	        this.membership_version = source["membership_version"];
	        this.policy_refs = this.convertValues(source["policy_refs"], accessprotocol.PolicyRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CloseDocumentResult {
	    closed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CloseDocumentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.closed = source["closed"];
	    }
	}
	
	export class ExternalMaterializationStatus {
	    candidate_provider_version: string;
	    failure?: string;
	    provider_version?: string;
	    receipt_digest?: string;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalMaterializationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.candidate_provider_version = source["candidate_provider_version"];
	        this.failure = source["failure"];
	        this.provider_version = source["provider_version"];
	        this.receipt_digest = source["receipt_digest"];
	        this.state = source["state"];
	    }
	}
	export class LocalDocumentSource {
	    entry_path?: string;
	    kind: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalDocumentSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entry_path = source["entry_path"];
	        this.kind = source["kind"];
	        this.path = source["path"];
	    }
	}
	export class OpenRuntimeDocumentInput {
	    document_id: string;
	    local_source?: LocalDocumentSource;
	    requested_revision_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenRuntimeDocumentInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document_id = source["document_id"];
	        this.local_source = this.convertValues(source["local_source"], LocalDocumentSource);
	        this.requested_revision_id = source["requested_revision_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkingDocumentRef {
	    base_revision: CommittedRevisionRef;
	    session: RuntimeSessionRef;
	    working_generation: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkingDocumentRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base_revision = this.convertValues(source["base_revision"], CommittedRevisionRef);
	        this.session = this.convertValues(source["session"], RuntimeSessionRef);
	        this.working_generation = source["working_generation"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StateInput {
	    expected_state_version?: string;
	    kind: string;
	    snapshot?: semantic.StateQuerySnapshot;
	    snapshot_hash?: string;
	
	    static createFrom(source: any = {}) {
	        return new StateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expected_state_version = source["expected_state_version"];
	        this.kind = source["kind"];
	        this.snapshot = this.convertValues(source["snapshot"], semantic.StateQuerySnapshot);
	        this.snapshot_hash = source["snapshot_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeScope {
	    access_fingerprint: string;
	    document_id: string;
	    local_scope_id: string;
	    organization_scope_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeScope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.access_fingerprint = source["access_fingerprint"];
	        this.document_id = source["document_id"];
	        this.local_scope_id = source["local_scope_id"];
	        this.organization_scope_id = source["organization_scope_id"];
	    }
	}
	export class RuntimeSessionRef {
	    expires_at?: string;
	    runtime_session_id: string;
	    scope: RuntimeScope;
	    session_generation: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeSessionRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expires_at = source["expires_at"];
	        this.runtime_session_id = source["runtime_session_id"];
	        this.scope = this.convertValues(source["scope"], RuntimeScope);
	        this.session_generation = source["session_generation"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeItemLimitValue {
	    hard_maximum: string;
	    unit: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeItemLimitValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hard_maximum = source["hard_maximum"];
	        this.unit = source["unit"];
	    }
	}
	export class RuntimeByteLimitValue {
	    hard_maximum: string;
	    unit: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeByteLimitValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hard_maximum = source["hard_maximum"];
	        this.unit = source["unit"];
	    }
	}
	export class RuntimeLimits {
	    max_blob_bytes: RuntimeByteLimitValue;
	    max_blob_total_bytes: RuntimeByteLimitValue;
	    max_commit_operations: RuntimeItemLimitValue;
	    max_history_items: RuntimeItemLimitValue;
	    max_output_bytes: RuntimeByteLimitValue;
	    max_state_mutations: RuntimeItemLimitValue;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeLimits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max_blob_bytes = this.convertValues(source["max_blob_bytes"], RuntimeByteLimitValue);
	        this.max_blob_total_bytes = this.convertValues(source["max_blob_total_bytes"], RuntimeByteLimitValue);
	        this.max_commit_operations = this.convertValues(source["max_commit_operations"], RuntimeItemLimitValue);
	        this.max_history_items = this.convertValues(source["max_history_items"], RuntimeItemLimitValue);
	        this.max_output_bytes = this.convertValues(source["max_output_bytes"], RuntimeByteLimitValue);
	        this.max_state_mutations = this.convertValues(source["max_state_mutations"], RuntimeItemLimitValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeCapabilityManifest {
	    authoring_grant_summary?: accessprotocol.AuthoringGrantSummary;
	    limits: RuntimeLimits;
	    manifest_etag: string;
	    operations: Record<string, protocolcommon.OperationCapability>;
	    storage_capabilities: string[];
	
	    static createFrom(source: any = {}) {
	        return new RuntimeCapabilityManifest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authoring_grant_summary = this.convertValues(source["authoring_grant_summary"], accessprotocol.AuthoringGrantSummary);
	        this.limits = this.convertValues(source["limits"], RuntimeLimits);
	        this.manifest_etag = source["manifest_etag"];
	        this.operations = this.convertValues(source["operations"], protocolcommon.OperationCapability, true);
	        this.storage_capabilities = source["storage_capabilities"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenRuntimeDocumentResult {
	    access_summary: accessprotocol.AuthoringGrantSummary;
	    capability_manifest: RuntimeCapabilityManifest;
	    committed_revision: CommittedRevisionRef;
	    session: RuntimeSessionRef;
	    state_input: StateInput;
	    working_document: WorkingDocumentRef;
	
	    static createFrom(source: any = {}) {
	        return new OpenRuntimeDocumentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.access_summary = this.convertValues(source["access_summary"], accessprotocol.AuthoringGrantSummary);
	        this.capability_manifest = this.convertValues(source["capability_manifest"], RuntimeCapabilityManifest);
	        this.committed_revision = this.convertValues(source["committed_revision"], CommittedRevisionRef);
	        this.session = this.convertValues(source["session"], RuntimeSessionRef);
	        this.state_input = this.convertValues(source["state_input"], StateInput);
	        this.working_document = this.convertValues(source["working_document"], WorkingDocumentRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PreviewEvaluation {
	    authoring_decision: accessprotocol.AuthoringDecision;
	    authoring_impact: semantic.AuthoringImpact;
	
	    static createFrom(source: any = {}) {
	        return new PreviewEvaluation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authoring_decision = this.convertValues(source["authoring_decision"], accessprotocol.AuthoringDecision);
	        this.authoring_impact = this.convertValues(source["authoring_impact"], semantic.AuthoringImpact);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeOperationBatch {
	    base_revision: CommittedRevisionRef;
	    document_id: string;
	    expected_definition_hash: string;
	    operations: engineprotocol.SemanticOperationBatch;
	    preconditions: engineprotocol.EngineEditPreconditions;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeOperationBatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base_revision = this.convertValues(source["base_revision"], CommittedRevisionRef);
	        this.document_id = source["document_id"];
	        this.expected_definition_hash = source["expected_definition_hash"];
	        this.operations = this.convertValues(source["operations"], engineprotocol.SemanticOperationBatch);
	        this.preconditions = this.convertValues(source["preconditions"], engineprotocol.EngineEditPreconditions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PreviewOperationsInput {
	    operation_batch: RuntimeOperationBatch;
	    session: RuntimeSessionRef;
	
	    static createFrom(source: any = {}) {
	        return new PreviewOperationsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operation_batch = this.convertValues(source["operation_batch"], RuntimeOperationBatch);
	        this.session = this.convertValues(source["session"], RuntimeSessionRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PreviewOperationsResult {
	    authoring_proof: AuthoringProof;
	    definition_hash: string;
	    graph_hash: string;
	    preview_evaluation: PreviewEvaluation;
	
	    static createFrom(source: any = {}) {
	        return new PreviewOperationsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authoring_proof = this.convertValues(source["authoring_proof"], AuthoringProof);
	        this.definition_hash = source["definition_hash"];
	        this.graph_hash = source["graph_hash"];
	        this.preview_evaluation = this.convertValues(source["preview_evaluation"], PreviewEvaluation);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RevisionMetadata {
	    authoring_decision_digest: string;
	    committed_at: string;
	    external_materialization?: ExternalMaterializationStatus;
	    operation_id: string;
	    parent_revision_id?: string;
	    revision: CommittedRevisionRef;
	    trigger: string;
	
	    static createFrom(source: any = {}) {
	        return new RevisionMetadata(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.authoring_decision_digest = source["authoring_decision_digest"];
	        this.committed_at = source["committed_at"];
	        this.external_materialization = this.convertValues(source["external_materialization"], ExternalMaterializationStatus);
	        this.operation_id = source["operation_id"];
	        this.parent_revision_id = source["parent_revision_id"];
	        this.revision = this.convertValues(source["revision"], CommittedRevisionRef);
	        this.trigger = source["trigger"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RevisionPage {
	    items: RevisionMetadata[];
	    page: protocolcommon.PageInfo;
	
	    static createFrom(source: any = {}) {
	        return new RevisionPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], RevisionMetadata);
	        this.page = this.convertValues(source["page"], protocolcommon.PageInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	

}

export namespace semantic {
	
	export class AuthoredAssetRef {
	    digest: string;
	    media_type: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredAssetRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.digest = source["digest"];
	        this.media_type = source["media_type"];
	    }
	}
	export class AuthoredEntityRepresentation {
	    kind: string;
	    shape?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredEntityRepresentation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.shape = source["shape"];
	    }
	}
	export class AuthoredFieldPath {
	    tokens: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuthoredFieldPath(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tokens = source["tokens"];
	    }
	}
	export class RecipeScalar {
	    boolean_value?: boolean;
	    integer_value?: string;
	    kind: string;
	    number_value?: string;
	    string_value?: string;
	
	    static createFrom(source: any = {}) {
	        return new RecipeScalar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.boolean_value = source["boolean_value"];
	        this.integer_value = source["integer_value"];
	        this.kind = source["kind"];
	        this.number_value = source["number_value"];
	        this.string_value = source["string_value"];
	    }
	}
	export class AuthoredOperationSource {
	    after?: string;
	    arguments?: Record<string, RecipeScalar>;
	    before?: string;
	    column_addresses?: string[];
	    direction?: string;
	    endpoint?: string;
	    field?: string;
	    field_path?: string;
	    kind: string;
	    query_address?: string;
	    relation_type_addresses?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuthoredOperationSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after = source["after"];
	        this.arguments = this.convertValues(source["arguments"], RecipeScalar, true);
	        this.before = source["before"];
	        this.column_addresses = source["column_addresses"];
	        this.direction = source["direction"];
	        this.endpoint = source["endpoint"];
	        this.field = source["field"];
	        this.field_path = source["field_path"];
	        this.kind = source["kind"];
	        this.query_address = source["query_address"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AuthoredRelationCardinalityBound {
	    max: string;
	    min: number;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationCardinalityBound(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.max = source["max"];
	        this.min = source["min"];
	    }
	}
	export class AuthoredRelationCardinality {
	    from_per_to: AuthoredRelationCardinalityBound;
	    to_per_from: AuthoredRelationCardinalityBound;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationCardinality(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from_per_to = this.convertValues(source["from_per_to"], AuthoredRelationCardinalityBound);
	        this.to_per_from = this.convertValues(source["to_per_from"], AuthoredRelationCardinalityBound);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AuthoredRelationComposedProjection {
	    badge_endpoint?: string;
	    child_endpoint?: string;
	    conflict?: string;
	    keep_edge?: boolean;
	    mode?: string;
	    overlay_endpoint?: string;
	    parent_endpoint?: string;
	    priority?: string;
	    target_endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationComposedProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.badge_endpoint = source["badge_endpoint"];
	        this.child_endpoint = source["child_endpoint"];
	        this.conflict = source["conflict"];
	        this.keep_edge = source["keep_edge"];
	        this.mode = source["mode"];
	        this.overlay_endpoint = source["overlay_endpoint"];
	        this.parent_endpoint = source["parent_endpoint"];
	        this.priority = source["priority"];
	        this.target_endpoint = source["target_endpoint"];
	    }
	}
	export class AuthoredRelationContextProjection {
	    fact_template?: string;
	    include_attribute_rows?: boolean;
	    reverse_fact_template?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationContextProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fact_template = source["fact_template"];
	        this.include_attribute_rows = source["include_attribute_rows"];
	        this.reverse_fact_template = source["reverse_fact_template"];
	    }
	}
	export class AuthoredRelationDiagramProjection {
	    edge_label?: string;
	    include_relation_type?: boolean;
	    mode?: string;
	    source_endpoint?: string;
	    target_endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationDiagramProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.edge_label = source["edge_label"];
	        this.include_relation_type = source["include_relation_type"];
	        this.mode = source["mode"];
	        this.source_endpoint = source["source_endpoint"];
	        this.target_endpoint = source["target_endpoint"];
	    }
	}
	export class AuthoredRelationEndpointRule {
	    entity_type_addresses?: string[];
	    layer_addresses?: string[];
	    role: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationEndpointRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.layer_addresses = source["layer_addresses"];
	        this.role = source["role"];
	    }
	}
	export class AuthoredRelationExport {
	    include_endpoints?: boolean;
	    include_relation_rows?: boolean;
	    sheet_name?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationExport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.include_endpoints = source["include_endpoints"];
	        this.include_relation_rows = source["include_relation_rows"];
	        this.sheet_name = source["sheet_name"];
	    }
	}
	export class AuthoredRelationFlowProjection {
	    branch_value_column_address?: string;
	    connector_kind?: string;
	    source_endpoint?: string;
	    target_endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationFlowProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch_value_column_address = source["branch_value_column_address"];
	        this.connector_kind = source["connector_kind"];
	        this.source_endpoint = source["source_endpoint"];
	        this.target_endpoint = source["target_endpoint"];
	    }
	}
	export class AuthoredRelationMatrixProjection {
	    column_endpoint?: string;
	    include_relation_rows?: boolean;
	    row_endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationMatrixProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_endpoint = source["column_endpoint"];
	        this.include_relation_rows = source["include_relation_rows"];
	        this.row_endpoint = source["row_endpoint"];
	    }
	}
	export class AuthoredRelationTreeProjection {
	    child_endpoint?: string;
	    parent_endpoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationTreeProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_endpoint = source["child_endpoint"];
	        this.parent_endpoint = source["parent_endpoint"];
	    }
	}
	export class AuthoredRelationTableProjection {
	    include_from?: boolean;
	    include_relation_type?: boolean;
	    include_to?: boolean;
	    row_mode?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationTableProjection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.include_from = source["include_from"];
	        this.include_relation_type = source["include_relation_type"];
	        this.include_to = source["include_to"];
	        this.row_mode = source["row_mode"];
	    }
	}
	export class AuthoredRelationProjectionSet {
	    composed?: AuthoredRelationComposedProjection;
	    context?: AuthoredRelationContextProjection;
	    diagram?: AuthoredRelationDiagramProjection;
	    flow?: AuthoredRelationFlowProjection;
	    matrix?: AuthoredRelationMatrixProjection;
	    table?: AuthoredRelationTableProjection;
	    tree?: AuthoredRelationTreeProjection;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationProjectionSet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.composed = this.convertValues(source["composed"], AuthoredRelationComposedProjection);
	        this.context = this.convertValues(source["context"], AuthoredRelationContextProjection);
	        this.diagram = this.convertValues(source["diagram"], AuthoredRelationDiagramProjection);
	        this.flow = this.convertValues(source["flow"], AuthoredRelationFlowProjection);
	        this.matrix = this.convertValues(source["matrix"], AuthoredRelationMatrixProjection);
	        this.table = this.convertValues(source["table"], AuthoredRelationTableProjection);
	        this.tree = this.convertValues(source["tree"], AuthoredRelationTreeProjection);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AuthoredRelationRenderBadge {
	    icon?: string;
	    label?: string;
	    position?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationRenderBadge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.icon = source["icon"];
	        this.label = source["label"];
	        this.position = source["position"];
	    }
	}
	export class AuthoredRelationRenderEdge {
	    arrow?: string;
	    color?: string;
	    label?: string;
	    line?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationRenderEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.arrow = source["arrow"];
	        this.color = source["color"];
	        this.label = source["label"];
	        this.line = source["line"];
	    }
	}
	export class AuthoredRelationRenderNested {
	    frame_label?: string;
	    frame_style?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationRenderNested(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.frame_label = source["frame_label"];
	        this.frame_style = source["frame_style"];
	    }
	}
	export class AuthoredRelationRenderOverlay {
	    kind?: string;
	    max_items?: number;
	    position?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationRenderOverlay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.max_items = source["max_items"];
	        this.position = source["position"];
	    }
	}
	export class AuthoredRelationRenderSet {
	    badge?: AuthoredRelationRenderBadge;
	    edge?: AuthoredRelationRenderEdge;
	    nested?: AuthoredRelationRenderNested;
	    overlay?: AuthoredRelationRenderOverlay;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationRenderSet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.badge = this.convertValues(source["badge"], AuthoredRelationRenderBadge);
	        this.edge = this.convertValues(source["edge"], AuthoredRelationRenderEdge);
	        this.nested = this.convertValues(source["nested"], AuthoredRelationRenderNested);
	        this.overlay = this.convertValues(source["overlay"], AuthoredRelationRenderOverlay);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AuthoredRelationTraversalPolicy {
	    default_direction?: string;
	    participates_in_dependency_matrix?: boolean;
	    participates_in_flow?: boolean;
	    participates_in_hierarchy?: boolean;
	    participates_in_impact?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredRelationTraversalPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.default_direction = source["default_direction"];
	        this.participates_in_dependency_matrix = source["participates_in_dependency_matrix"];
	        this.participates_in_flow = source["participates_in_flow"];
	        this.participates_in_hierarchy = source["participates_in_hierarchy"];
	        this.participates_in_impact = source["participates_in_impact"];
	    }
	}
	
	export class AuthoredViewAxis {
	    entity_type_addresses?: string[];
	    label_field?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredViewAxis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.label_field = source["label_field"];
	    }
	}
	export class AuthoredViewMatrixCell {
	    attribute_column_addresses?: string[];
	    direction?: string;
	    display?: string;
	    relation_type_addresses?: string[];
	    semantic?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredViewMatrixCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attribute_column_addresses = source["attribute_column_addresses"];
	        this.direction = source["direction"];
	        this.display = source["display"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	        this.semantic = source["semantic"];
	    }
	}
	export class AuthoredViewProjectionOverride {
	    composed?: AuthoredRelationComposedProjection;
	    context?: AuthoredRelationContextProjection;
	    diagram?: AuthoredRelationDiagramProjection;
	    flow?: AuthoredRelationFlowProjection;
	    matrix?: AuthoredRelationMatrixProjection;
	    render?: AuthoredRelationRenderSet;
	    table?: AuthoredRelationTableProjection;
	    tree?: AuthoredRelationTreeProjection;
	
	    static createFrom(source: any = {}) {
	        return new AuthoredViewProjectionOverride(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.composed = this.convertValues(source["composed"], AuthoredRelationComposedProjection);
	        this.context = this.convertValues(source["context"], AuthoredRelationContextProjection);
	        this.diagram = this.convertValues(source["diagram"], AuthoredRelationDiagramProjection);
	        this.flow = this.convertValues(source["flow"], AuthoredRelationFlowProjection);
	        this.matrix = this.convertValues(source["matrix"], AuthoredRelationMatrixProjection);
	        this.render = this.convertValues(source["render"], AuthoredRelationRenderSet);
	        this.table = this.convertValues(source["table"], AuthoredRelationTableProjection);
	        this.tree = this.convertValues(source["tree"], AuthoredRelationTreeProjection);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewTableSort {
	    absent: string;
	    column_id: string;
	    direction: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewTableSort(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.absent = source["absent"];
	        this.column_id = source["column_id"];
	        this.direction = source["direction"];
	    }
	}
	export class ViewPlacement {
	    entity_address: string;
	    height: string;
	    width: string;
	    x: string;
	    y: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewPlacement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_address = source["entity_address"];
	        this.height = source["height"];
	        this.width = source["width"];
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class AuthoredViewShape {
	    abstraction?: string;
	    cell?: AuthoredViewMatrixCell;
	    column_axis?: AuthoredViewAxis;
	    composed?: boolean;
	    cycle_policy?: string;
	    detect_moves?: boolean;
	    direction?: string;
	    entity_type_addresses?: string[];
	    group_by?: string;
	    include?: string[];
	    include_entity_id?: boolean;
	    include_entity_rows?: boolean;
	    include_layer?: boolean;
	    include_relation_rows?: boolean;
	    include_type?: boolean;
	    incoming?: boolean;
	    kind: string;
	    lane_by?: string;
	    lane_column_addresses?: string[];
	    layout?: string;
	    outgoing?: boolean;
	    placements?: ViewPlacement[];
	    preserve_parallel?: boolean;
	    relation_type_addresses?: string[];
	    row_axis?: AuthoredViewAxis;
	    row_source?: string;
	    shared_child_policy?: string;
	    sorts?: ViewTableSort[];
	
	    static createFrom(source: any = {}) {
	        return new AuthoredViewShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abstraction = source["abstraction"];
	        this.cell = this.convertValues(source["cell"], AuthoredViewMatrixCell);
	        this.column_axis = this.convertValues(source["column_axis"], AuthoredViewAxis);
	        this.composed = source["composed"];
	        this.cycle_policy = source["cycle_policy"];
	        this.detect_moves = source["detect_moves"];
	        this.direction = source["direction"];
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.group_by = source["group_by"];
	        this.include = source["include"];
	        this.include_entity_id = source["include_entity_id"];
	        this.include_entity_rows = source["include_entity_rows"];
	        this.include_layer = source["include_layer"];
	        this.include_relation_rows = source["include_relation_rows"];
	        this.include_type = source["include_type"];
	        this.incoming = source["incoming"];
	        this.kind = source["kind"];
	        this.lane_by = source["lane_by"];
	        this.lane_column_addresses = source["lane_column_addresses"];
	        this.layout = source["layout"];
	        this.outgoing = source["outgoing"];
	        this.placements = this.convertValues(source["placements"], ViewPlacement);
	        this.preserve_parallel = source["preserve_parallel"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	        this.row_axis = this.convertValues(source["row_axis"], AuthoredViewAxis);
	        this.row_source = source["row_source"];
	        this.shared_child_policy = source["shared_child_policy"];
	        this.sorts = this.convertValues(source["sorts"], ViewTableSort);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SourceOrigin {
	    kind: string;
	    pack_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceOrigin(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.pack_address = source["pack_address"];
	    }
	}
	export class SourceRange {
	    end_byte: string;
	    module_path: string;
	    origin: SourceOrigin;
	    start_byte: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.end_byte = source["end_byte"];
	        this.module_path = source["module_path"];
	        this.origin = this.convertValues(source["origin"], SourceOrigin);
	        this.start_byte = source["start_byte"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GraphAuthoringFacts {
	    action_flags: string[];
	    column_addresses: string[];
	    endpoint_entity_addresses: string[];
	    entity_type_addresses: string[];
	    layer_addresses: string[];
	    relation_type_addresses: string[];
	
	    static createFrom(source: any = {}) {
	        return new GraphAuthoringFacts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action_flags = source["action_flags"];
	        this.column_addresses = source["column_addresses"];
	        this.endpoint_entity_addresses = source["endpoint_entity_addresses"];
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.layer_addresses = source["layer_addresses"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	    }
	}
	export class AuthoringImpactEntry {
	    action: string;
	    after_refs: string[];
	    before_refs: string[];
	    capability: string;
	    changed_field_paths: AuthoredFieldPath[];
	    graph_facts?: GraphAuthoringFacts;
	    owner_address?: string;
	    source_refs: SourceRange[];
	    subject_address?: string;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoringImpactEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.after_refs = source["after_refs"];
	        this.before_refs = source["before_refs"];
	        this.capability = source["capability"];
	        this.changed_field_paths = this.convertValues(source["changed_field_paths"], AuthoredFieldPath);
	        this.graph_facts = this.convertValues(source["graph_facts"], GraphAuthoringFacts);
	        this.owner_address = source["owner_address"];
	        this.source_refs = this.convertValues(source["source_refs"], SourceRange);
	        this.subject_address = source["subject_address"];
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AuthoringImpact {
	    base_definition_hash: string;
	    entries: AuthoringImpactEntry[];
	    impact_digest: string;
	    required_capabilities: string[];
	    resulting_definition_hash: string;
	    semantic_diff_hash: string;
	    source_diff_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthoringImpact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base_definition_hash = source["base_definition_hash"];
	        this.entries = this.convertValues(source["entries"], AuthoringImpactEntry);
	        this.impact_digest = source["impact_digest"];
	        this.required_capabilities = source["required_capabilities"];
	        this.resulting_definition_hash = source["resulting_definition_hash"];
	        this.semantic_diff_hash = source["semantic_diff_hash"];
	        this.source_diff_hash = source["source_diff_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ChildSetHash {
	    child_addresses: string[];
	    child_kind: string;
	    hash: string;
	    owner_address: string;
	
	    static createFrom(source: any = {}) {
	        return new ChildSetHash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_addresses = source["child_addresses"];
	        this.child_kind = source["child_kind"];
	        this.hash = source["hash"];
	        this.owner_address = source["owner_address"];
	    }
	}
	export class CompletedExportArtifactEntry {
	    content_digest: string;
	    logical_path: string;
	    media_type: string;
	    primary: boolean;
	    role: string;
	
	    static createFrom(source: any = {}) {
	        return new CompletedExportArtifactEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content_digest = source["content_digest"];
	        this.logical_path = source["logical_path"];
	        this.media_type = source["media_type"];
	        this.primary = source["primary"];
	        this.role = source["role"];
	    }
	}
	export class ViewDataStateReadRef {
	    field_path: string;
	    subject_address: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewDataStateReadRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field_path = source["field_path"];
	        this.subject_address = source["subject_address"];
	    }
	}
	export class ViewDataStateRefs {
	    reads: ViewDataStateReadRef[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDataStateRefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reads = this.convertValues(source["reads"], ViewDataStateReadRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewDataCellRef {
	    column_address: string;
	    row_address: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewDataCellRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_address = source["column_address"];
	        this.row_address = source["row_address"];
	    }
	}
	export class ViewDataSourceRefs {
	    asset_digests: string[];
	    cell_refs: ViewDataCellRef[];
	    entity_addresses: string[];
	    layer_addresses: string[];
	    relation_addresses: string[];
	    row_addresses: string[];
	    state: ViewDataStateRefs;
	    subject_addresses: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDataSourceRefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.asset_digests = source["asset_digests"];
	        this.cell_refs = this.convertValues(source["cell_refs"], ViewDataCellRef);
	        this.entity_addresses = source["entity_addresses"];
	        this.layer_addresses = source["layer_addresses"];
	        this.relation_addresses = source["relation_addresses"];
	        this.row_addresses = source["row_addresses"];
	        this.state = this.convertValues(source["state"], ViewDataStateRefs);
	        this.subject_addresses = source["subject_addresses"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContextAttribute {
	    group_key: string;
	    key: string;
	    owner_address: string;
	    row_address: string;
	    source: ViewDataSourceRefs;
	    values: Record<string, RecipeScalar>;
	
	    static createFrom(source: any = {}) {
	        return new ContextAttribute(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group_key = source["group_key"];
	        this.key = source["key"];
	        this.owner_address = source["owner_address"];
	        this.row_address = source["row_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.values = this.convertValues(source["values"], RecipeScalar, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContextFact {
	    direction: string;
	    entity_address: string;
	    key: string;
	    relation_address: string;
	    row_addresses: string[];
	    source: ViewDataSourceRefs;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ContextFact(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.direction = source["direction"];
	        this.entity_address = source["entity_address"];
	        this.key = source["key"];
	        this.relation_address = source["relation_address"];
	        this.row_addresses = source["row_addresses"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.text = source["text"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContextGroup {
	    attributes: ContextAttribute[];
	    facts: ContextFact[];
	    key: string;
	    label: string;
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new ContextGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attributes = this.convertValues(source["attributes"], ContextAttribute);
	        this.facts = this.convertValues(source["facts"], ContextFact);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContextViewData {
	    groups: ContextGroup[];
	
	    static createFrom(source: any = {}) {
	        return new ContextViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], ContextGroup);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagnosticRelated {
	    message?: string;
	    owner_address?: string;
	    range?: SourceRange;
	    relation: string;
	    subject_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticRelated(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.owner_address = source["owner_address"];
	        this.range = this.convertValues(source["range"], SourceRange);
	        this.relation = source["relation"];
	        this.subject_address = source["subject_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagnosticArgumentValue {
	    array_value?: DiagnosticArgumentValue[];
	    boolean_value?: boolean;
	    integer_value?: string;
	    kind: string;
	    number_value?: string;
	    object_value?: Record<string, DiagnosticArgumentValue>;
	    stable_address_value?: string;
	    string_value?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticArgumentValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.array_value = this.convertValues(source["array_value"], DiagnosticArgumentValue);
	        this.boolean_value = source["boolean_value"];
	        this.integer_value = source["integer_value"];
	        this.kind = source["kind"];
	        this.number_value = source["number_value"];
	        this.object_value = this.convertValues(source["object_value"], DiagnosticArgumentValue, true);
	        this.stable_address_value = source["stable_address_value"];
	        this.string_value = source["string_value"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Diagnostic {
	    arguments: Record<string, DiagnosticArgumentValue>;
	    code: string;
	    message?: string;
	    message_key: string;
	    owner_address?: string;
	    protocol_version: number;
	    range?: SourceRange;
	    related: DiagnosticRelated[];
	    severity: string;
	    subject_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.arguments = this.convertValues(source["arguments"], DiagnosticArgumentValue, true);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.message_key = source["message_key"];
	        this.owner_address = source["owner_address"];
	        this.protocol_version = source["protocol_version"];
	        this.range = this.convertValues(source["range"], SourceRange);
	        this.related = this.convertValues(source["related"], DiagnosticRelated);
	        this.severity = source["severity"];
	        this.subject_address = source["subject_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class DiagramBadge {
	    key: string;
	    label?: string;
	    relation_address: string;
	    relation_type_address: string;
	    source: ViewDataSourceRefs;
	    target_occurrence_key: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagramBadge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.relation_address = source["relation_address"];
	        this.relation_type_address = source["relation_type_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.target_occurrence_key = source["target_occurrence_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramContainer {
	    child_keys: string[];
	    key: string;
	    occurrence_key: string;
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new DiagramContainer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child_keys = source["child_keys"];
	        this.key = source["key"];
	        this.occurrence_key = source["occurrence_key"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramEdge {
	    from_occurrence_key: string;
	    key: string;
	    relation_address: string;
	    relation_type_address: string;
	    source: ViewDataSourceRefs;
	    to_occurrence_key: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagramEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from_occurrence_key = source["from_occurrence_key"];
	        this.key = source["key"];
	        this.relation_address = source["relation_address"];
	        this.relation_type_address = source["relation_type_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.to_occurrence_key = source["to_occurrence_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramOccurrence {
	    entity_address: string;
	    key: string;
	    layer_address: string;
	    parent_key?: string;
	    role: string;
	    source: ViewDataSourceRefs;
	    via_relation_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagramOccurrence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_address = source["entity_address"];
	        this.key = source["key"];
	        this.layer_address = source["layer_address"];
	        this.parent_key = source["parent_key"];
	        this.role = source["role"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.via_relation_address = source["via_relation_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramOverlay {
	    key: string;
	    overlay_entity_address: string;
	    relation_address: string;
	    relation_type_address: string;
	    source: ViewDataSourceRefs;
	    target_occurrence_key: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagramOverlay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.overlay_entity_address = source["overlay_entity_address"];
	        this.relation_address = source["relation_address"];
	        this.relation_type_address = source["relation_type_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.target_occurrence_key = source["target_occurrence_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramSupportItem {
	    entity_address?: string;
	    key: string;
	    relation_address?: string;
	    source: ViewDataSourceRefs;
	    support_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagramSupportItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_address = source["entity_address"];
	        this.key = source["key"];
	        this.relation_address = source["relation_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.support_kind = source["support_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagramViewData {
	    badges: DiagramBadge[];
	    containers: DiagramContainer[];
	    edges: DiagramEdge[];
	    occurrences: DiagramOccurrence[];
	    overlays: DiagramOverlay[];
	    support_items: DiagramSupportItem[];
	
	    static createFrom(source: any = {}) {
	        return new DiagramViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.badges = this.convertValues(source["badges"], DiagramBadge);
	        this.containers = this.convertValues(source["containers"], DiagramContainer);
	        this.edges = this.convertValues(source["edges"], DiagramEdge);
	        this.occurrences = this.convertValues(source["occurrences"], DiagramOccurrence);
	        this.overlays = this.convertValues(source["overlays"], DiagramOverlay);
	        this.support_items = this.convertValues(source["support_items"], DiagramSupportItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewDataSemanticMapEntry {
	    key: string;
	    value: ViewDataSemanticValue;
	
	    static createFrom(source: any = {}) {
	        return new ViewDataSemanticMapEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = this.convertValues(source["value"], ViewDataSemanticValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewDataSemanticValue {
	    address?: string;
	    array?: ViewDataSemanticValue[];
	    blob?: protocolcommon.BlobRef;
	    boolean?: boolean;
	    decimal?: string;
	    integer?: string;
	    kind: string;
	    map?: ViewDataSemanticMapEntry[];
	    string?: string;
	    token?: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewDataSemanticValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.array = this.convertValues(source["array"], ViewDataSemanticValue);
	        this.blob = this.convertValues(source["blob"], protocolcommon.BlobRef);
	        this.boolean = source["boolean"];
	        this.decimal = source["decimal"];
	        this.integer = source["integer"];
	        this.kind = source["kind"];
	        this.map = this.convertValues(source["map"], ViewDataSemanticMapEntry);
	        this.string = source["string"];
	        this.token = source["token"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FieldDiff {
	    after?: ViewDataSemanticValue;
	    after_present: boolean;
	    before?: ViewDataSemanticValue;
	    before_present: boolean;
	    key: string;
	    path: string[];
	
	    static createFrom(source: any = {}) {
	        return new FieldDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after = this.convertValues(source["after"], ViewDataSemanticValue);
	        this.after_present = source["after_present"];
	        this.before = this.convertValues(source["before"], ViewDataSemanticValue);
	        this.before_present = source["before_present"];
	        this.key = source["key"];
	        this.path = source["path"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiffChange {
	    after_address?: string;
	    after_source?: ViewDataSourceRefs;
	    before_address?: string;
	    before_source?: ViewDataSourceRefs;
	    fields: FieldDiff[];
	    key: string;
	    kind: string;
	    source: ViewDataSourceRefs;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after_address = source["after_address"];
	        this.after_source = this.convertValues(source["after_source"], ViewDataSourceRefs);
	        this.before_address = source["before_address"];
	        this.before_source = this.convertValues(source["before_source"], ViewDataSourceRefs);
	        this.fields = this.convertValues(source["fields"], FieldDiff);
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiffViewData {
	    changes: DiffChange[];
	
	    static createFrom(source: any = {}) {
	        return new DiffViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.changes = this.convertValues(source["changes"], DiffChange);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportArtifactEntry {
	    logical_path: string;
	    media_type: string;
	    primary: boolean;
	    role: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportArtifactEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logical_path = source["logical_path"];
	        this.media_type = source["media_type"];
	        this.primary = source["primary"];
	        this.role = source["role"];
	    }
	}
	export class ExportDimension {
	    kind: string;
	    value?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportDimension(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.value = source["value"];
	    }
	}
	export class ExportOptions {
	    background?: string;
	    bundle?: boolean;
	    diagnostics?: boolean;
	    embed_assets?: boolean;
	    fit?: string;
	    formulas?: boolean;
	    header?: boolean;
	    height?: ExportDimension;
	    hidden_ids?: boolean;
	    interactive?: boolean;
	    kind: string;
	    legend?: boolean;
	    lookup_sheets?: boolean;
	    orientation?: string;
	    page_size?: string;
	    profile?: string;
	    scale?: string;
	    source_manifest?: boolean;
	    state_summary?: boolean;
	    view_data_json?: boolean;
	    width?: ExportDimension;
	
	    static createFrom(source: any = {}) {
	        return new ExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.background = source["background"];
	        this.bundle = source["bundle"];
	        this.diagnostics = source["diagnostics"];
	        this.embed_assets = source["embed_assets"];
	        this.fit = source["fit"];
	        this.formulas = source["formulas"];
	        this.header = source["header"];
	        this.height = this.convertValues(source["height"], ExportDimension);
	        this.hidden_ids = source["hidden_ids"];
	        this.interactive = source["interactive"];
	        this.kind = source["kind"];
	        this.legend = source["legend"];
	        this.lookup_sheets = source["lookup_sheets"];
	        this.orientation = source["orientation"];
	        this.page_size = source["page_size"];
	        this.profile = source["profile"];
	        this.scale = source["scale"];
	        this.source_manifest = source["source_manifest"];
	        this.state_summary = source["state_summary"];
	        this.view_data_json = source["view_data_json"];
	        this.width = this.convertValues(source["width"], ExportDimension);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportPagination {
	    kind: string;
	    orientation?: string;
	    page_size?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportPagination(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.orientation = source["orientation"];
	        this.page_size = source["page_size"];
	    }
	}
	export class ExportPlanUnit {
	    artifact_role: string;
	    kind: string;
	    order: string;
	    role: string;
	    unit_id: string;
	    viewdata_keys: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExportPlanUnit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact_role = source["artifact_role"];
	        this.kind = source["kind"];
	        this.order = source["order"];
	        this.role = source["role"];
	        this.unit_id = source["unit_id"];
	        this.viewdata_keys = source["viewdata_keys"];
	    }
	}
	export class ViewDataStateInputRef {
	    captured_at?: string;
	    definition_hash?: string;
	    kind: string;
	    snapshot_hash?: string;
	    state_version?: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewDataStateInputRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.captured_at = source["captured_at"];
	        this.definition_hash = source["definition_hash"];
	        this.kind = source["kind"];
	        this.snapshot_hash = source["snapshot_hash"];
	        this.state_version = source["state_version"];
	    }
	}
	export class ExportRepresentation {
	    artifact_role?: string;
	    disposition: string;
	    locator?: string;
	    omission_reason?: string;
	    source: ViewDataSourceRefs;
	    unit_id?: string;
	    viewdata_key: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportRepresentation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact_role = source["artifact_role"];
	        this.disposition = source["disposition"];
	        this.locator = source["locator"];
	        this.omission_reason = source["omission_reason"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.unit_id = source["unit_id"];
	        this.viewdata_key = source["viewdata_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExporterProfileRef {
	    format: string;
	    id: string;
	    registry_digest: string;
	    registry_schema_version: number;
	    specification_digest: string;
	
	    static createFrom(source: any = {}) {
	        return new ExporterProfileRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.id = source["id"];
	        this.registry_digest = source["registry_digest"];
	        this.registry_schema_version = source["registry_schema_version"];
	        this.specification_digest = source["specification_digest"];
	    }
	}
	export class ExportPlan {
	    artifacts: ExportArtifactEntry[];
	    effective_maximum_fidelity: string;
	    exporter_profile: ExporterProfileRef;
	    fidelity_basis: string;
	    format: string;
	    invocation_hash: string;
	    layout_requirement: string;
	    native_maximum_fidelity: string;
	    pagination: ExportPagination;
	    profile_ref_hash: string;
	    profile_requirements_hash: string;
	    recipe_address: string;
	    recipe_hash: string;
	    representations: ExportRepresentation[];
	    requested_fidelity: string;
	    required_asset_digests: string[];
	    required_font_digests: string[];
	    requires_renderer: boolean;
	    schema_version: number;
	    serializer_options: ExportOptions;
	    serializer_profile: ExporterProfileRef;
	    source_manifest_path?: string;
	    source_manifest_required: boolean;
	    state_input: ViewDataStateInputRef;
	    state_policy: string;
	    state_summary_hash?: string;
	    units: ExportPlanUnit[];
	    view_data_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifacts = this.convertValues(source["artifacts"], ExportArtifactEntry);
	        this.effective_maximum_fidelity = source["effective_maximum_fidelity"];
	        this.exporter_profile = this.convertValues(source["exporter_profile"], ExporterProfileRef);
	        this.fidelity_basis = source["fidelity_basis"];
	        this.format = source["format"];
	        this.invocation_hash = source["invocation_hash"];
	        this.layout_requirement = source["layout_requirement"];
	        this.native_maximum_fidelity = source["native_maximum_fidelity"];
	        this.pagination = this.convertValues(source["pagination"], ExportPagination);
	        this.profile_ref_hash = source["profile_ref_hash"];
	        this.profile_requirements_hash = source["profile_requirements_hash"];
	        this.recipe_address = source["recipe_address"];
	        this.recipe_hash = source["recipe_hash"];
	        this.representations = this.convertValues(source["representations"], ExportRepresentation);
	        this.requested_fidelity = source["requested_fidelity"];
	        this.required_asset_digests = source["required_asset_digests"];
	        this.required_font_digests = source["required_font_digests"];
	        this.requires_renderer = source["requires_renderer"];
	        this.schema_version = source["schema_version"];
	        this.serializer_options = this.convertValues(source["serializer_options"], ExportOptions);
	        this.serializer_profile = this.convertValues(source["serializer_profile"], ExporterProfileRef);
	        this.source_manifest_path = source["source_manifest_path"];
	        this.source_manifest_required = source["source_manifest_required"];
	        this.state_input = this.convertValues(source["state_input"], ViewDataStateInputRef);
	        this.state_policy = source["state_policy"];
	        this.state_summary_hash = source["state_summary_hash"];
	        this.units = this.convertValues(source["units"], ExportPlanUnit);
	        this.view_data_hash = source["view_data_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class ViewRevision {
	    after_definition_hash?: string;
	    after_revision_id?: string;
	    before_definition_hash?: string;
	    before_revision_id?: string;
	    definition_hash?: string;
	    kind: string;
	    recipe_definition_hash?: string;
	    recipe_revision_id?: string;
	    revision_id?: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewRevision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after_definition_hash = source["after_definition_hash"];
	        this.after_revision_id = source["after_revision_id"];
	        this.before_definition_hash = source["before_definition_hash"];
	        this.before_revision_id = source["before_revision_id"];
	        this.definition_hash = source["definition_hash"];
	        this.kind = source["kind"];
	        this.recipe_definition_hash = source["recipe_definition_hash"];
	        this.recipe_revision_id = source["recipe_revision_id"];
	        this.revision_id = source["revision_id"];
	    }
	}
	export class ExportSourceManifest {
	    artifacts: CompletedExportArtifactEntry[];
	    asset_digests: string[];
	    effective_maximum_fidelity: string;
	    exporter_profile: ExporterProfileRef;
	    fidelity_basis: string;
	    font_digests: string[];
	    format: string;
	    invocation_hash: string;
	    native_maximum_fidelity: string;
	    primary_artifact: string;
	    profile_ref_hash: string;
	    profile_requirements_hash: string;
	    recipe_address: string;
	    recipe_hash: string;
	    representations: ExportRepresentation[];
	    requested_fidelity: string;
	    revision: ViewRevision;
	    schema_version: number;
	    serializer_profile: ExporterProfileRef;
	    state_input: ViewDataStateInputRef;
	    state_policy: string;
	    state_summary_hash?: string;
	    view_data_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportSourceManifest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifacts = this.convertValues(source["artifacts"], CompletedExportArtifactEntry);
	        this.asset_digests = source["asset_digests"];
	        this.effective_maximum_fidelity = source["effective_maximum_fidelity"];
	        this.exporter_profile = this.convertValues(source["exporter_profile"], ExporterProfileRef);
	        this.fidelity_basis = source["fidelity_basis"];
	        this.font_digests = source["font_digests"];
	        this.format = source["format"];
	        this.invocation_hash = source["invocation_hash"];
	        this.native_maximum_fidelity = source["native_maximum_fidelity"];
	        this.primary_artifact = source["primary_artifact"];
	        this.profile_ref_hash = source["profile_ref_hash"];
	        this.profile_requirements_hash = source["profile_requirements_hash"];
	        this.recipe_address = source["recipe_address"];
	        this.recipe_hash = source["recipe_hash"];
	        this.representations = this.convertValues(source["representations"], ExportRepresentation);
	        this.requested_fidelity = source["requested_fidelity"];
	        this.revision = this.convertValues(source["revision"], ViewRevision);
	        this.schema_version = source["schema_version"];
	        this.serializer_profile = this.convertValues(source["serializer_profile"], ExporterProfileRef);
	        this.state_input = this.convertValues(source["state_input"], ViewDataStateInputRef);
	        this.state_policy = source["state_policy"];
	        this.state_summary_hash = source["state_summary_hash"];
	        this.view_data_hash = source["view_data_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class FlowConnector {
	    branch_row_addresses: string[];
	    branch_value?: RecipeScalar;
	    from_step_key: string;
	    key: string;
	    kind: string;
	    relation_addresses: string[];
	    source: ViewDataSourceRefs;
	    to_step_key: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowConnector(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch_row_addresses = source["branch_row_addresses"];
	        this.branch_value = this.convertValues(source["branch_value"], RecipeScalar);
	        this.from_step_key = source["from_step_key"];
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.relation_addresses = source["relation_addresses"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.to_step_key = source["to_step_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FlowCycleRef {
	    branch_row_addresses: string[];
	    branch_value?: RecipeScalar;
	    connector_key: string;
	    from_step_key: string;
	    key: string;
	    kind: string;
	    relation_addresses: string[];
	    source: ViewDataSourceRefs;
	    to_step_key: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowCycleRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch_row_addresses = source["branch_row_addresses"];
	        this.branch_value = this.convertValues(source["branch_value"], RecipeScalar);
	        this.connector_key = source["connector_key"];
	        this.from_step_key = source["from_step_key"];
	        this.key = source["key"];
	        this.kind = source["kind"];
	        this.relation_addresses = source["relation_addresses"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.to_step_key = source["to_step_key"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FlowLane {
	    key: string;
	    label: string;
	    source: ViewDataSourceRefs;
	    step_keys: string[];
	
	    static createFrom(source: any = {}) {
	        return new FlowLane(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.step_keys = source["step_keys"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FlowStep {
	    branch: boolean;
	    entity_address: string;
	    join: boolean;
	    key: string;
	    lane_key: string;
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new FlowStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.branch = source["branch"];
	        this.entity_address = source["entity_address"];
	        this.join = source["join"];
	        this.key = source["key"];
	        this.lane_key = source["lane_key"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FlowViewData {
	    connectors: FlowConnector[];
	    cycle_refs: FlowCycleRef[];
	    lanes: FlowLane[];
	    steps: FlowStep[];
	
	    static createFrom(source: any = {}) {
	        return new FlowViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connectors = this.convertValues(source["connectors"], FlowConnector);
	        this.cycle_refs = this.convertValues(source["cycle_refs"], FlowCycleRef);
	        this.lanes = this.convertValues(source["lanes"], FlowLane);
	        this.steps = this.convertValues(source["steps"], FlowStep);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class MatrixAttributeItem {
	    column_address: string;
	    relation_address: string;
	    row_address: string;
	    value: RecipeScalar;
	
	    static createFrom(source: any = {}) {
	        return new MatrixAttributeItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_address = source["column_address"];
	        this.relation_address = source["relation_address"];
	        this.row_address = source["row_address"];
	        this.value = this.convertValues(source["value"], RecipeScalar);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MatrixAxisItem {
	    entity_address: string;
	    key: string;
	    label: string;
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new MatrixAxisItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_address = source["entity_address"];
	        this.key = source["key"];
	        this.label = source["label"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewDataQueryPath {
	    entity_addresses: string[];
	    relation_addresses: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDataQueryPath(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_addresses = source["entity_addresses"];
	        this.relation_addresses = source["relation_addresses"];
	    }
	}
	export class MatrixSemanticRef {
	    kind: string;
	    path?: ViewDataQueryPath;
	    relation_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new MatrixSemanticRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.path = this.convertValues(source["path"], ViewDataQueryPath);
	        this.relation_address = source["relation_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MatrixDisplayValue {
	    attributes?: MatrixAttributeItem[];
	    boolean?: boolean;
	    integer?: string;
	    kind: string;
	    string_set?: string[];
	
	    static createFrom(source: any = {}) {
	        return new MatrixDisplayValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attributes = this.convertValues(source["attributes"], MatrixAttributeItem);
	        this.boolean = source["boolean"];
	        this.integer = source["integer"];
	        this.kind = source["kind"];
	        this.string_set = source["string_set"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MatrixCell {
	    column_key: string;
	    display_value: MatrixDisplayValue;
	    key: string;
	    row_key: string;
	    semantic_refs: MatrixSemanticRef[];
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new MatrixCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_key = source["column_key"];
	        this.display_value = this.convertValues(source["display_value"], MatrixDisplayValue);
	        this.key = source["key"];
	        this.row_key = source["row_key"];
	        this.semantic_refs = this.convertValues(source["semantic_refs"], MatrixSemanticRef);
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class MatrixViewData {
	    cells: MatrixCell[];
	    column_axis: MatrixAxisItem[];
	    row_axis: MatrixAxisItem[];
	
	    static createFrom(source: any = {}) {
	        return new MatrixViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cells = this.convertValues(source["cells"], MatrixCell);
	        this.column_axis = this.convertValues(source["column_axis"], MatrixAxisItem);
	        this.row_axis = this.convertValues(source["row_axis"], MatrixAxisItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModuleRef {
	    module_path: string;
	    origin: SourceOrigin;
	
	    static createFrom(source: any = {}) {
	        return new ModuleRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.module_path = source["module_path"];
	        this.origin = this.convertValues(source["origin"], SourceOrigin);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class QueryRecipeSelect {
	    entity_type_addresses?: string[];
	    layer_addresses?: string[];
	    relation_type_addresses?: string[];
	    root_addresses?: string[];
	
	    static createFrom(source: any = {}) {
	        return new QueryRecipeSelect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.layer_addresses = source["layer_addresses"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	        this.root_addresses = source["root_addresses"];
	    }
	}
	export class QueryRecipeTraversal {
	    cycle_policy: string;
	    direction: string;
	    max_depth: string;
	    min_depth: string;
	    relation_type_addresses?: string[];
	
	    static createFrom(source: any = {}) {
	        return new QueryRecipeTraversal(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cycle_policy = source["cycle_policy"];
	        this.direction = source["direction"];
	        this.max_depth = source["max_depth"];
	        this.min_depth = source["min_depth"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	    }
	}
	export class RecipeOperandType {
	    address_kind?: string;
	    kind: string;
	    scalar_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new RecipeOperandType(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address_kind = source["address_kind"];
	        this.kind = source["kind"];
	        this.scalar_type = source["scalar_type"];
	    }
	}
	export class RecipePredicateValue {
	    address_value?: string;
	    address_values?: string[];
	    kind: string;
	    parameter_address?: string;
	    scalar_value?: RecipeScalar;
	    scalar_values?: RecipeScalar[];
	
	    static createFrom(source: any = {}) {
	        return new RecipePredicateValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address_value = source["address_value"];
	        this.address_values = source["address_values"];
	        this.kind = source["kind"];
	        this.parameter_address = source["parameter_address"];
	        this.scalar_value = this.convertValues(source["scalar_value"], RecipeScalar);
	        this.scalar_values = this.convertValues(source["scalar_values"], RecipeScalar);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RecipeRowPredicate {
	    child?: RecipeRowPredicate;
	    children?: RecipeRowPredicate[];
	    column_addresses?: string[];
	    field_path?: string;
	    kind: string;
	    operand_type?: RecipeOperandType;
	    operator?: string;
	    value?: RecipePredicateValue;
	
	    static createFrom(source: any = {}) {
	        return new RecipeRowPredicate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child = this.convertValues(source["child"], RecipeRowPredicate);
	        this.children = this.convertValues(source["children"], RecipeRowPredicate);
	        this.column_addresses = source["column_addresses"];
	        this.field_path = source["field_path"];
	        this.kind = source["kind"];
	        this.operand_type = this.convertValues(source["operand_type"], RecipeOperandType);
	        this.operator = source["operator"];
	        this.value = this.convertValues(source["value"], RecipePredicateValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RecipePredicate {
	    child?: RecipePredicate;
	    children?: RecipePredicate[];
	    field?: string;
	    field_path?: string;
	    kind: string;
	    operand_type?: RecipeOperandType;
	    operator?: string;
	    predicate?: RecipeRowPredicate;
	    quantifier?: string;
	    type_addresses?: string[];
	    value?: RecipePredicateValue;
	
	    static createFrom(source: any = {}) {
	        return new RecipePredicate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.child = this.convertValues(source["child"], RecipePredicate);
	        this.children = this.convertValues(source["children"], RecipePredicate);
	        this.field = source["field"];
	        this.field_path = source["field_path"];
	        this.kind = source["kind"];
	        this.operand_type = this.convertValues(source["operand_type"], RecipeOperandType);
	        this.operator = source["operator"];
	        this.predicate = this.convertValues(source["predicate"], RecipeRowPredicate);
	        this.quantifier = source["quantifier"];
	        this.type_addresses = source["type_addresses"];
	        this.value = this.convertValues(source["value"], RecipePredicateValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SemanticDiffEntry {
	    after_address?: string;
	    after_hash?: string;
	    before_address?: string;
	    before_hash?: string;
	    changed_field_paths: AuthoredFieldPath[];
	    kind: string;
	    owner_address?: string;
	    subject_kind: string;
	
	    static createFrom(source: any = {}) {
	        return new SemanticDiffEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.after_address = source["after_address"];
	        this.after_hash = source["after_hash"];
	        this.before_address = source["before_address"];
	        this.before_hash = source["before_hash"];
	        this.changed_field_paths = this.convertValues(source["changed_field_paths"], AuthoredFieldPath);
	        this.kind = source["kind"];
	        this.owner_address = source["owner_address"];
	        this.subject_kind = source["subject_kind"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SemanticDiff {
	    digest: string;
	    entries: SemanticDiffEntry[];
	
	    static createFrom(source: any = {}) {
	        return new SemanticDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.digest = source["digest"];
	        this.entries = this.convertValues(source["entries"], SemanticDiffEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class StateQuerySubject {
	    fields: Record<string, RecipeScalar>;
	    own_subject_hash: string;
	    redacted_field_paths: string[];
	    subject_address: string;
	
	    static createFrom(source: any = {}) {
	        return new StateQuerySubject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fields = this.convertValues(source["fields"], RecipeScalar, true);
	        this.own_subject_hash = source["own_subject_hash"];
	        this.redacted_field_paths = source["redacted_field_paths"];
	        this.subject_address = source["subject_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StateQuerySnapshot {
	    captured_at: string;
	    definition_hash: string;
	    definition_project_address: string;
	    format: string;
	    graph_hash: string;
	    inaccessible_field_paths: string[];
	    schema_version: number;
	    state_version: string;
	    subjects: StateQuerySubject[];
	
	    static createFrom(source: any = {}) {
	        return new StateQuerySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.captured_at = source["captured_at"];
	        this.definition_hash = source["definition_hash"];
	        this.definition_project_address = source["definition_project_address"];
	        this.format = source["format"];
	        this.graph_hash = source["graph_hash"];
	        this.inaccessible_field_paths = source["inaccessible_field_paths"];
	        this.schema_version = source["schema_version"];
	        this.state_version = source["state_version"];
	        this.subjects = this.convertValues(source["subjects"], StateQuerySubject);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SubjectHash {
	    address: string;
	    hash: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new SubjectHash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.hash = source["hash"];
	        this.kind = source["kind"];
	    }
	}
	export class SubtreeHash {
	    hash: string;
	    owner_address: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtreeHash(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.owner_address = source["owner_address"];
	    }
	}
	export class ViewDataValue {
	    kind: string;
	    scalar?: RecipeScalar;
	    stable_address?: string;
	    string_set?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDataValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.scalar = this.convertValues(source["scalar"], RecipeScalar);
	        this.stable_address = source["stable_address"];
	        this.string_set = source["string_set"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TableCell {
	    present: boolean;
	    source: ViewDataSourceRefs;
	    value?: ViewDataValue;
	
	    static createFrom(source: any = {}) {
	        return new TableCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.present = source["present"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.value = this.convertValues(source["value"], ViewDataValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TableColumn {
	    address?: string;
	    enum_values?: string[];
	    id: string;
	    key: string;
	    label: string;
	    source_column_addresses: string[];
	    state_field_path?: string;
	    value_type: string;
	
	    static createFrom(source: any = {}) {
	        return new TableColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.enum_values = source["enum_values"];
	        this.id = source["id"];
	        this.key = source["key"];
	        this.label = source["label"];
	        this.source_column_addresses = source["source_column_addresses"];
	        this.state_field_path = source["state_field_path"];
	        this.value_type = source["value_type"];
	    }
	}
	export class TableRow {
	    cells: Record<string, TableCell>;
	    key: string;
	    source: ViewDataSourceRefs;
	
	    static createFrom(source: any = {}) {
	        return new TableRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cells = this.convertValues(source["cells"], TableCell, true);
	        this.key = source["key"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TableViewData {
	    columns: TableColumn[];
	    rows: TableRow[];
	
	    static createFrom(source: any = {}) {
	        return new TableViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = this.convertValues(source["columns"], TableColumn);
	        this.rows = this.convertValues(source["rows"], TableRow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TreeOccurrence {
	    children: TreeOccurrence[];
	    entity_address: string;
	    key: string;
	    source: ViewDataSourceRefs;
	    via_relation_address?: string;
	
	    static createFrom(source: any = {}) {
	        return new TreeOccurrence(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.children = this.convertValues(source["children"], TreeOccurrence);
	        this.entity_address = source["entity_address"];
	        this.key = source["key"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.via_relation_address = source["via_relation_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TreeRef {
	    from_occurrence_key: string;
	    key: string;
	    relation_address: string;
	    source: ViewDataSourceRefs;
	    to_entity_address: string;
	
	    static createFrom(source: any = {}) {
	        return new TreeRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from_occurrence_key = source["from_occurrence_key"];
	        this.key = source["key"];
	        this.relation_address = source["relation_address"];
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.to_entity_address = source["to_entity_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TreeViewData {
	    cycle_refs: TreeRef[];
	    link_refs: TreeRef[];
	    roots: TreeOccurrence[];
	
	    static createFrom(source: any = {}) {
	        return new TreeViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cycle_refs = this.convertValues(source["cycle_refs"], TreeRef);
	        this.link_refs = this.convertValues(source["link_refs"], TreeRef);
	        this.roots = this.convertValues(source["roots"], TreeOccurrence);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewContextShape {
	    group_by: string;
	    include_entity_rows: boolean;
	    include_relation_rows: boolean;
	    incoming: boolean;
	    outgoing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ViewContextShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group_by = source["group_by"];
	        this.include_entity_rows = source["include_entity_rows"];
	        this.include_relation_rows = source["include_relation_rows"];
	        this.incoming = source["incoming"];
	        this.outgoing = source["outgoing"];
	    }
	}
	export class ViewTreeShape {
	    cycle_policy: string;
	    relation_type_addresses: string[];
	    shared_child_policy: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewTreeShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cycle_policy = source["cycle_policy"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	        this.shared_child_policy = source["shared_child_policy"];
	    }
	}
	export class ViewTableValueType {
	    enum_values?: string[];
	    format?: string;
	    kind: string;
	    scalar_type?: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewTableValueType(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enum_values = source["enum_values"];
	        this.format = source["format"];
	        this.kind = source["kind"];
	        this.scalar_type = source["scalar_type"];
	    }
	}
	export class ViewTableColumnSource {
	    column_addresses?: string[];
	    direction?: string;
	    endpoint?: string;
	    field?: string;
	    field_path?: string;
	    kind: string;
	    relation_type_addresses?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewTableColumnSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column_addresses = source["column_addresses"];
	        this.direction = source["direction"];
	        this.endpoint = source["endpoint"];
	        this.field = source["field"];
	        this.field_path = source["field_path"];
	        this.kind = source["kind"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	    }
	}
	export class ViewTableColumn {
	    address: string;
	    aggregate: string;
	    id: string;
	    label?: string;
	    source: ViewTableColumnSource;
	    value_type: ViewTableValueType;
	
	    static createFrom(source: any = {}) {
	        return new ViewTableColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.aggregate = source["aggregate"];
	        this.id = source["id"];
	        this.label = source["label"];
	        this.source = this.convertValues(source["source"], ViewTableColumnSource);
	        this.value_type = this.convertValues(source["value_type"], ViewTableValueType);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewTableShape {
	    automatic_relation_columns: string[];
	    columns: ViewTableColumn[];
	    entity_type_addresses?: string[];
	    include_entity_id: boolean;
	    include_layer: boolean;
	    include_type: boolean;
	    row_source: string;
	    sorts: ViewTableSort[];
	
	    static createFrom(source: any = {}) {
	        return new ViewTableShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.automatic_relation_columns = source["automatic_relation_columns"];
	        this.columns = this.convertValues(source["columns"], ViewTableColumn);
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.include_entity_id = source["include_entity_id"];
	        this.include_layer = source["include_layer"];
	        this.include_type = source["include_type"];
	        this.row_source = source["row_source"];
	        this.sorts = this.convertValues(source["sorts"], ViewTableSort);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewMatrixAxis {
	    entity_type_addresses?: string[];
	    label_field: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewMatrixAxis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entity_type_addresses = source["entity_type_addresses"];
	        this.label_field = source["label_field"];
	    }
	}
	export class ViewMatrixCell {
	    attribute_column_addresses?: string[];
	    direction: string;
	    display: string;
	    relation_type_addresses?: string[];
	    semantic: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewMatrixCell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attribute_column_addresses = source["attribute_column_addresses"];
	        this.direction = source["direction"];
	        this.display = source["display"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	        this.semantic = source["semantic"];
	    }
	}
	export class ViewMatrixShape {
	    cell: ViewMatrixCell;
	    column_axis: ViewMatrixAxis;
	    row_axis: ViewMatrixAxis;
	
	    static createFrom(source: any = {}) {
	        return new ViewMatrixShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cell = this.convertValues(source["cell"], ViewMatrixCell);
	        this.column_axis = this.convertValues(source["column_axis"], ViewMatrixAxis);
	        this.row_axis = this.convertValues(source["row_axis"], ViewMatrixAxis);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewFlowShape {
	    cycle_policy: string;
	    lane_by: string;
	    lane_column_addresses?: string[];
	    preserve_parallel: boolean;
	    relation_type_addresses: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewFlowShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cycle_policy = source["cycle_policy"];
	        this.lane_by = source["lane_by"];
	        this.lane_column_addresses = source["lane_column_addresses"];
	        this.preserve_parallel = source["preserve_parallel"];
	        this.relation_type_addresses = source["relation_type_addresses"];
	    }
	}
	export class ViewDiffShape {
	    detect_moves: boolean;
	    include: string[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDiffShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.detect_moves = source["detect_moves"];
	        this.include = source["include"];
	    }
	}
	export class ViewDiagramShape {
	    abstraction: string;
	    composed: boolean;
	    direction: string;
	    layout: string;
	    placements: ViewPlacement[];
	
	    static createFrom(source: any = {}) {
	        return new ViewDiagramShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abstraction = source["abstraction"];
	        this.composed = source["composed"];
	        this.direction = source["direction"];
	        this.layout = source["layout"];
	        this.placements = this.convertValues(source["placements"], ViewPlacement);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewRecipeShape {
	    context?: ViewContextShape;
	    diagram?: ViewDiagramShape;
	    diff?: ViewDiffShape;
	    flow?: ViewFlowShape;
	    kind: string;
	    matrix?: ViewMatrixShape;
	    table?: ViewTableShape;
	    tree?: ViewTreeShape;
	
	    static createFrom(source: any = {}) {
	        return new ViewRecipeShape(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.context = this.convertValues(source["context"], ViewContextShape);
	        this.diagram = this.convertValues(source["diagram"], ViewDiagramShape);
	        this.diff = this.convertValues(source["diff"], ViewDiffShape);
	        this.flow = this.convertValues(source["flow"], ViewFlowShape);
	        this.kind = source["kind"];
	        this.matrix = this.convertValues(source["matrix"], ViewMatrixShape);
	        this.table = this.convertValues(source["table"], ViewTableShape);
	        this.tree = this.convertValues(source["tree"], ViewTreeShape);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ViewData {
	    category: string;
	    context?: ContextViewData;
	    diagnostics: Diagnostic[];
	    diagram?: DiagramViewData;
	    diff?: DiffViewData;
	    flow?: FlowViewData;
	    kind: string;
	    matrix?: MatrixViewData;
	    project_address: string;
	    query_address?: string;
	    revision: ViewRevision;
	    shape: ViewRecipeShape;
	    source: ViewDataSourceRefs;
	    state_input: ViewDataStateInputRef;
	    state_policy: string;
	    table?: TableViewData;
	    tree?: TreeViewData;
	    view_address: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.context = this.convertValues(source["context"], ContextViewData);
	        this.diagnostics = this.convertValues(source["diagnostics"], Diagnostic);
	        this.diagram = this.convertValues(source["diagram"], DiagramViewData);
	        this.diff = this.convertValues(source["diff"], DiffViewData);
	        this.flow = this.convertValues(source["flow"], FlowViewData);
	        this.kind = source["kind"];
	        this.matrix = this.convertValues(source["matrix"], MatrixViewData);
	        this.project_address = source["project_address"];
	        this.query_address = source["query_address"];
	        this.revision = this.convertValues(source["revision"], ViewRevision);
	        this.shape = this.convertValues(source["shape"], ViewRecipeShape);
	        this.source = this.convertValues(source["source"], ViewDataSourceRefs);
	        this.state_input = this.convertValues(source["state_input"], ViewDataStateInputRef);
	        this.state_policy = source["state_policy"];
	        this.table = this.convertValues(source["table"], TableViewData);
	        this.tree = this.convertValues(source["tree"], TreeViewData);
	        this.view_address = source["view_address"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	

}
