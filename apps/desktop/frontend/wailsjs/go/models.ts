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
	export class RecentProject {
	    project_id: string;
	    pinned: boolean;
	    last_opened_at: string;
	    availability: string;
	
	    static createFrom(source: any = {}) {
	        return new RecentProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.pinned = source["pinned"];
	        this.last_opened_at = source["last_opened_at"];
	        this.availability = source["availability"];
	    }
	}

}

export namespace desktopcontract {
	
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

}

export namespace protocolcommon {
	
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

}

