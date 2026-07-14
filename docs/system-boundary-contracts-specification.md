# システム境界契約 詳細仕様

## 1. 目的と規範範囲

この文書は、[component-package-boundary-specification.md](component-package-boundary-specification.md)で分離したシステム構成部品の間を流れる契約を規定する。対象は次である。

1. Engine Protocol Boundary
2. Runtime / Host Port Boundary
3. Realtime Boundary
4. Query Execution Adapter Boundary
5. Registry Boundary
6. Render / Export Boundary
7. SDK Public Boundary
8. Version / Release Boundary

`.layerdraw`と`.ldpack`はファイルコンテナであり、Go package、TypeScript package、binary、framework shellとは別の分類である。この文書で`package`と単独表記する場合はsoftware packageを指し、ファイルコンテナは`container`または`Registry artifact`と表記する。

規範の分担は次の通りである。

| 対象 | 規範文書 |
| --- | --- |
| LDL構文、Normalized Model、StableAddress、semantic operation | [ldl-language-specification.md](ldl-language-specification.md)、[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) |
| Compiler / Workbenchの純粋処理 | [compiler-architecture.md](compiler-architecture.md) |
| component、package、binary、bundle closure | [component-package-boundary-specification.md](component-package-boundary-specification.md) |
| 本文書で扱う境界契約、状態遷移、失敗、version交渉 | 本文書 |

既存文書の概念説明と本文書のwire / port契約が競合する場合、構文と意味論はLDL仕様、実装依存方向はComponent / Package Boundary、境界上のoperationと状態遷移は本文書を優先する。

## 2. 共通契約

### 2.1 Schema authority

境界を越える型の正本はmonorepoの`schemas/`に置く言語中立schemaである。Go struct、TypeScript interface、OpenAPI document、Wails bindingを正本にしない。

```text
schemas/
  semantic/
  protocol-common/
  access-protocol/
  engine-protocol/
  runtime-protocol/
  server-application-protocol/
  realtime-protocol/
  query-adapter-protocol/
  registry-protocol/
  container/
  release/
```

`protocol-common`はrequest / response envelope、ProtocolFailure、PageInfo、BlobRef、handshake、CapabilityManifestの共通primitiveを所有する。`semantic`はAuthoringImpactを含むActor非依存domain型、`access-protocol`はAuthoringCapability、HostOperationImpact、AuthoringPolicy、Grant、Decisionのtransport非依存wire型を所有する。`server-application-protocol`はInstance、Organization、Workspace、Project directory、membership、AuthoringPolicy binding、Share、Audit、Entitlement / UsageのHost metadata APIを所有し、Policy wire型やRuntime document lifecycleを再定義しない。`realtime-protocol`はRuntime Protocolから参照される独立schema group、`query-adapter-protocol`はEngineとexecution adapterが使うadapter-facing wire groupである。これはadapter実装用の低レベルSPIであり、SDK利用者がQueryExecutionPlanを作るsemantic APIではない。`release`はrelease manifestと互換性metadataを所有する。

生成先は次に固定する。

```text
schemas/* -> gen/go/*
schemas/* -> @layerdraw/protocol/*
```

手書きGo型と手書きTS型で同じwire型を重複定義してはならない。host-language convenience typeはgenerated wire型から明示変換し、wireへ戻す時に未知fieldを黙って破棄しない。

### 2.2 Naming and serialization

- canonical wire fieldは`lower_snake_case`とする。
- operation nameは`<namespace>.<verb_noun>`とする。
- enum値は`lower_snake_case`とする。
- JSON文字列はUTF-8、時刻はUTCのRFC 3339、durationは整数nanosecondではなく単位付きISO 8601 durationまたはschemaで指定した整数millisecondとする。
- `int64`、`uint64`、decimalはJSON numberの精度に依存せず、canonical decimal stringで表す。
- digestは`<algorithm>:<lowercase-hex>`とし、既定algorithmは`sha256`とする。
- object field orderは意味を持たない。hash対象はschemaが定めるcanonical serializationを使う。

### 2.3 Request envelope

すべてのEngine、Runtime、Registry operationは同じrequest metadataを持つ。

```text
RequestEnvelope<T>
  protocol
    name
    version
  request_id
  operation
  deadline_at?
  trace_context?
  actor_context_ref?
  payload: T
```

- `request_id`は呼出側が生成し、retryでも同じ論理requestなら維持する。
- writeの重複排除は`request_id`ではなくdomain payloadの`idempotency_key`で行う。
- `actor_context_ref`はRuntime / Registry hostでだけ使う。pure EngineへActor、credential、ACLを渡さない。
- timeoutとcancelはtransportがGo `context.Context`へ写す。deadline後にcommit成否が不明なwriteは、同じ`idempotency_key`で結果照会する。

### 2.4 Response envelope

transport成功とdomain結果を混同しない。

```text
ResponseEnvelope<T>
  protocol
    name
    version
  request_id
  engine_release?
  host_release?
  outcome: success | rejected | failed | cancelled
  payload?: T
  diagnostics: Diagnostic[]
  failure?: ProtocolFailure
  page?: PageInfo
```

| outcome | 意味 |
| --- | --- |
| `success` | operationを処理し、payloadを返した。payload内のdomain statusが`committed_state_stale`等でもtransport処理自体は成功である |
| `rejected` | source、precondition、policy、compatibilityなど予測可能な入力条件で拒否した。diagnosticsを必須とする |
| `failed` | I/O、process、adapter、resource exhaustion、内部invariantなど、domain inputだけでは表現できない失敗 |
| `cancelled` | 呼出側cancelまたはdeadline。writeはpayloadに既知のpublication statusを含められる |

`failed`をHTTP 200へ偽装する必要はないが、HTTP statusだけからdomain resultを再構成してはならない。stdio、WASM、HTTP、Wails、MCPは同じ`outcome`を保持する。

### 2.5 Diagnostic and failure

`Diagnostic`は利用者がsource、operation、policy、version、stateを修正できる問題である。

```text
Diagnostic
  code
  severity: error | warning | information | hint
  message
  source?
    module_path
    span
    stable_address?
  related[]
  data?
  remediation?
```

`ProtocolFailure`はtransportまたは実行基盤の失敗である。

```text
ProtocolFailure
  code
  category: transport | io | adapter | resource | invariant | cancelled
  message
  retryable
  retry_after_ms?
  correlation_id?
  safe_details?
```

stack trace、credential、backend locator、raw provider response、SQL / Cypher statementを標準responseへ含めない。debug capabilityが有効な管理者surfaceだけ、別の安全なdiagnostic attachmentとして提供できる。

### 2.6 Opaque handles

opaque handleはStableAddress、Document ID、revisionの代用ではない。

| Handle | Scope | Binding | Invalidated by |
| --- | --- | --- | --- |
| `document_handle` | process / Worker session | source tree digest、compile options、Engine instance | close、eviction、Engine restart |
| `document_generation` | document handle内 | source tree digest | Workbench apply、source replacement |
| `query_execution_token` | process / Worker session | document generation、query、arguments、state input、plan version | complete、cancel、expiry、generation change |
| `runtime_session_id` | host session | Organization scope（server-backedの場合）、Document ID、Actor access fingerprint、opened revision | close、access revocation、host restart |
| `working_generation` | realtime room | Organization scope、room、base revision、accepted working operations | checkpoint reset、room recovery |
| `registry_transaction_id` | Registry / Runtime host | Organization / local project scope、source、resolved plan digest、project revision | commit、rollback、expiry |

handleはwire上ではopaque stringとし、内部情報をclientがdecodeして判断しない。server-side handleは十分なentropyを持ち、Actor / sessionへscopeする。

`document_handle`を参照するoperationは`document_generation`も必須とする。世代不一致は`engine.stale_document_generation`で拒否し、自動的に最新へ適用しない。

### 2.7 Cursor and pagination

cursorは次へ束縛する。

- operation name
- normalized request digest
- document revisionまたはdocument generation
- Organization / local project scope
- Actor access / redaction fingerprint
- deterministic sort key
- schema version
- expiry

cursorはopaqueかつ改ざん検知可能でなければならない。別revision、別Actor、別filterへの流用は`protocol.invalid_cursor`で拒否する。

```text
PageInfo
  result_truncated
  next_cursor?
  returned_items
  returned_bytes
  total_items?: unknown | exact integer
```

暗黙のtruncateは禁止する。すべてのlist / search / scoped readは`max_items`と`max_output_bytes`を受け、上限到達時は`result_truncated`を返す。

### 2.8 Capability handshake

packageがlinkされていることと、利用者にcapabilityが公開されることは同一ではない。clientはoperation実行前にhandshakeする。

```text
HandshakeRequest
  client_release
  protocols[]
    name
    supported_range: canonical "major.minor..major.minor", lower <= upper
    versions[]
      version: exact major.minor within supported_range
      schema_digest: exact digest for that version
  required_capabilities[]
  optional_capabilities[]
  client_limits?

HandshakeResult
  host_release
  endpoint_instance_id
  negotiated_protocols[]
  capability_statuses[]
    capability_id
    enabled
    protocol_version
    unavailable_reason?
  capability_manifest
  release_manifest_digest
```

handshake operation自体も`ResponseEnvelope<HandshakeResult>`を使う。protocol major非overlap、required capability不足、schema digest不一致は`outcome=rejected`とし、Diagnostic `data`へ10.9節の`UpgradeDiagnosticData`を格納する。接続できないclientにもcredentialや未許可capability一覧を返さない。

```text
CapabilityManifest
  manifest_version
  manifest_etag
  manifest_scope: endpoint | client | effective
  actor_scope_digest?
  operations: map<operation_name, OperationCapability>
  transports[]
  limits
  query_adapters[]
  realtime_profiles[]
  registry_sources[]
  renderer_profiles[]
  exporter_profiles[]
  search_profiles[]
  embedding_profiles[]
  required_ladybug_primitives[]
  storage_capabilities[]
  authoring_grant_summary?

OperationCapability
  enabled
  unavailable_reason?: not_linked | not_configured | not_authorized | not_entitled | incompatible | offline | degraded | unsupported
  protocol_version
  limits?
  required_authoring_capabilities[]?
```

`enabled=true`は`unavailable_reason`を禁止し、`enabled=false`は理由を必須とする。
`capability_statuses`はrequestのrequired / optional capability IDごとに一意な結果を返す。
requestの`required_capabilities`と`optional_capabilities`はそれぞれ重複を禁止し、
両集合のoverlapも禁止する。重複またはoverlapを持つHandshakeRequestはnegotiation前に
wire validation errorとしてrequest全体をrejectし、deduplicate、required/optional間の
再分類、複数statusへの展開を行わない。
未知のoptional IDはrequest全体を失敗させず、`enabled=false`かつ
`unavailable_reason=unsupported`として明示する。選択アルゴリズムとoverlap policyはIssue #28が所有する。

`supported_range`は両端を含む単一major内のcanonical rangeで、各端はunsigned 32-bitの
major/minorをleading zeroなしで表す。majorを跨ぐ場合は別offerを使う。range中の各advertise versionは`versions`の
一意なentryでexact schema digestへ結び付ける。Engine handshake envelopeのbootstrap
`protocol`は成功・拒否のどちらも`{name:"engine", version:"1.0"}`であり、これは
version選択結果ではなくhandshake wire自体のversionである。

Engine 1.0のlimit keyは9個に閉じる。byte、item、axis pixel、total pixelのunitを
schemaどおりに固定し、endpoint descriptorはhard maximum、default、client-scoped
effective maximumを区別する。effective maximumはclient ceilingがあればhost hard
maximumとの最小値、なければhost hard maximumである。compile requestの正のoverrideが
effective maximumを超える場合はrejectし、zeroまたは省略は
`min(default_value, effective_maximum)`を適用する。dispatch時の診断生成はIssue #29で行う。

release fieldはcanonical SemVer 2.0.0、release manifest identityと`manifest_etag`は
lowercase SHA-256 digestである。`manifest_etag`は自身を除くcanonical effective manifest
projectionのdigestとし、その構築はIssue #28が所有する。`endpoint_instance_id`は128文字以下の
opaque non-secret identifierで、process / Worker instanceの生存期間だけ安定する。生成、restart時の
更新、release metadataとの整合はIssue #28が所有し、schema/codegenはshapeだけを固定する。

manifestはActorとentitlementに応じて変わる。authorization情報を漏らす場合、存在自体を返さず`not_authorized`を一般化してよい。manifest変更時は`manifest_etag`を変更し、long-lived clientへcapability changed eventを通知する。

endpointは自分が実行できるcapabilityだけを宣言し、browser rendererなど接続clientの能力を代わりに宣言しない。SDKはendpoint manifestとlocal component manifestを入力に`effective` manifestを構成する。operationにremote hostとlocal serializerの両方が必要な場合は、両方がenabledの時だけeffective capabilityをenabledにする。

### 2.9 Blob transfer

大きなsource tree、container、asset、export bytesをJSONへbase64埋め込みしない。transportは共通の`BlobRef`を使う。

```text
BlobRef
  blob_id
  media_type
  size
  digest
  lifetime: request | session | persistent
```

- in-process / Wailsはstreamまたはreaderを使う。
- stdioはcontrol frameとlength-prefixed binary frameを分離する。
- HTTPはupload/download endpointまたはsigned one-shot URLを使う。
- WASM Workerは`ArrayBuffer`をtransferする。
- MCPはresourceまたはattachmentへmapする。

digest確認前にcontainerを展開しない。session BlobRefを永続参照として保存しない。

### 2.10 Idempotency and transaction identity

write operationは次を区別する。

| ID | 意味 |
| --- | --- |
| `request_id` | 1回のprotocol呼出追跡 |
| `operation_id` | user / agent intentを跨いでdefinition、state、audit、eventを束ねるidentity |
| `idempotency_key` | 同じwrite intentの再実行を一度へ収束させるkey |
| `transaction_id` | Registry installやRuntime commitの内部transaction identity |

同じ`idempotency_key`へ異なるnormalized payload digestを送った場合は`runtime.idempotency_mismatch`で拒否する。結果は少なくともprovider retry windowより長く照会可能にする。

## 3. Engine Protocol Boundary

### 3.1 Ownership

Engine Protocolはpure Go Engine / Workbench capabilityをtransport-neutralに公開する。Engineは次を所有する。

- parse、compile、validate、format、organize
- SemanticIndex、SourceMap、StableAddress、hash
- scoped readとsemantic diff
- semantic operation / scoped LDL fragmentのpreviewとephemeral apply
- before / after semantic diffからのAuthoringImpact分類
- SearchDocument / SearchResult、Query plan / QueryResult、AnalysisResult normalization
- ViewData materialization
- ExportPlan generation
- `.layerdraw` / `.ldpack` validationとcanonical build

Engine ProtocolはDocumentStore、revision publication、Actor、Access、credential、Registry download、realtime room、renderer、export artifact保存を所有しない。

### 3.2 Operation catalog

operation nameは次を規範名とする。

| Operation | Purpose |
| --- | --- |
| `engine.handshake` | protocol / capability交渉 |
| `engine.compile` | 閉じたCompileInputを1回compileする |
| `engine.open_document` | source treeをEngine sessionへ開きhandleを返す |
| `engine.replace_source_tree` | expected generation付きでsource treeを置換する |
| `engine.close_document` | handleを解放する |
| `engine.list_modules` | module closureとdigestをpage取得する |
| `engine.find_symbols` | SemanticIndexを検索する |
| `engine.prepare_search` | SearchExecutionPlanとtokenを生成する |
| `engine.complete_search` | typed raw hitsをcanonical SearchResultへ変換する |
| `engine.cancel_search` | search tokenを無効化する |
| `engine.inspect_subgraph` | Entity中心の型・Layer・row・Relationをbounded取得する |
| `engine.read_declarations` | scoped LDLとmetadataを取得する |
| `engine.read_rows` | Entity / Relation row subtreeをpage取得する |
| `engine.get_neighbors` | Relation endpointをdirection / depth付きで取得する |
| `engine.find_usages` | StableAddressの参照元を取得する |
| `engine.read_scope` | owner subtreeをtoken budget付きで取得する |
| `engine.list_references` | Reference declarationを列挙する |
| `engine.read_references` | Reference本文を取得する |
| `engine.preview_operations` | semantic operation batchを適用せずpreviewする |
| `engine.preview_fragment` | scoped LDL fragmentを適用せずpreviewする |
| `engine.classify_authoring_impact` | 2つのclosed source treeまたは検証済みsemantic diffからAuthoringImpactを生成する |
| `engine.apply_to_handle` | preview済み変更をephemeral handleへ適用しgenerationを進める |
| `engine.preview_source_patch` | revision-protected raw source patchをpreviewする |
| `engine.format_scope` | 指定scopeだけcanonical formatする |
| `engine.organize_workspace` | 標準workspace配置へのmove planを作る |
| `engine.prepare_query` | QueryExecutionPlanとtokenを生成する |
| `engine.complete_query` | TypedRawRowsをcanonical QueryResultへ変換する |
| `engine.cancel_query` | query tokenを無効化する |
| `engine.prepare_analysis` | AnalysisExecutionPlanとtokenを生成する |
| `engine.complete_analysis` | typed metric rowsをcanonical AnalysisResultへ変換する |
| `engine.cancel_analysis` | analysis tokenを無効化する |
| `engine.materialize_view` | QueryResult / DiffInputからViewDataを生成する |
| `engine.plan_export` | ViewDataとExportRecipeからExportPlanを生成する |
| `engine.inspect_container` | container manifestとentryを安全に列挙する |
| `engine.validate_container` | ZIP safety、manifest、LDL、resolved treeを検証する |
| `engine.build_layerdraw` | canonical `.layerdraw` bytesを生成する |
| `engine.build_ldpack` | canonical `.ldpack` bytesを生成する |

`engine.apply_to_handle`はstorage commitではない。返されたsource treeを捨てても永続状態は変わらない。host-backed commitは`runtime.commit_operations`を使う。

### 3.3 Open document

```text
OpenEngineDocumentInput
  compile_input
  requested_limits?

OpenEngineDocumentResult
  document_handle
  document_generation
  definition_hash
  graph_hash
  project_address
  diagnostics
  effective_limits
```

compile errorがあってもlossless CSTを構築できる場合、read / diagnostics / source patch capabilityを持つhandleを返せる。TypedASTまたはNormalizedDocumentを必要とするQuery、View、ExportPlanはdisabled状態となる。

### 3.4 Workbench preview and apply

portable Engine operationはhost revisionを知らない。preconditionはEngine generationとsemantic hashへ限定する。

```text
EngineEditPreconditions
  document_generation
  expected_subject_hashes
  expected_subtree_hashes
  expected_child_sets
  expected_source_digests?
```

```text
WorkbenchPreviewResult
  preview_id
  base_generation
  proposed_generation
  changed_source_files
  source_edits
  semantic_diff
  authoring_impact
  required_authoring_capabilities[]
  authoring_impact_digest
  resulting_hashes
  conflicts
  diagnostics
  preview_digest
```

`engine.apply_to_handle`は`preview_id`、`preview_digest`、base generationを要求する。preview後にgenerationが変わった場合は再previewを要求する。

portable EngineはActor / Policyを知らないためAuthoringImpactだけを返し、allow / denyを主張しない。`allowed_kinds`、operation名、source pathはgrantではない。semantic operation、fragment、source patch、import、Registry result、restore diffが同じbefore / afterを作る場合は同じAuthoringImpactを返さなければならない。

Runtime向けin-process facadeは同じWorkbench primitiveを使うが、Host Document ID、base revision、idempotencyをRuntime envelopeに一度だけ保持する。OperationBatch内外で同じfieldを二重定義しない。

### 3.5 Transport mapping

| Transport | Mapping |
| --- | --- |
| in-process Go | typed facade method + `context.Context` |
| stdio | request envelopeをcontrol frame、BlobRefをbinary frameとしてmultiplexする |
| WASM Worker | request envelopeを`postMessage`、large bytesをtransferable `ArrayBuffer`で渡す |
| HTTP | `POST /api/engine/operations/{operation}`、blobは別endpoint |
| Wails | generated bindingが同じrequest / response型を呼ぶ。Wails method固有のdomain型を作らない |
| MCP | operationをtool / resourceへ薄くmapする。MCP固有responseからdomain resultを再構成しない |

transportによってoperationを省略する場合はCapabilityManifestで表現する。同じoperation名の意味をtransportごとに変えてはならない。

### 3.6 Go and generated TypeScript mapping

```text
schemas/engine-protocol/*.schema
  -> gen/go/engineprotocol
  -> @layerdraw/protocol/engine
```

- Go Engine facadeはgenerated request / response型を直接domain modelに渡さず、boundary mapperで検証して内部型へ変換する。
- `@layerdraw/engine-client`はgenerated型をtransportし、LDL semanticsを持たない。
- Goの`error`は`ProtocolFailure`へmapし、source diagnosticを`error`文字列へ潰さない。
- TSのexceptionはnetwork、decode、client misuseに限定し、正常に受信した`rejected`をthrowだけで表現しない。

## 4. Runtime / Host Port Boundary

### 4.1 Runtime ownership

Runtimeはhost-backed document lifecycleを所有する。

- Document IDとRuntime session
- open、commit、save、autosave、restore、reconcile
- Working DocumentとCommitted Revision
- conditional write、lease、idempotency、operation recovery
- StateBackend、HistoryStore、AssetStore、ArtifactStoreのorchestration
- Access decision適用とStateQuerySnapshot固定
- AuthoringPolicy / Grant snapshot固定、AuthoringImpactとHostOperationImpactに対するAccess decision適用
- Access適用済みSearch Index lifecycleとEmbedding Provider orchestration
- realtime / audit event publication

RuntimeはLDL parse、semantic operation適用、Query / View semantics、Registry dependency resolution、provider credential永続化を所有しない。

### 4.2 Runtime Protocol operation catalog

| Operation | Purpose |
| --- | --- |
| `runtime.handshake` | protocol / capability交渉 |
| `runtime.open_document` | Committed Revisionを開きRuntime sessionを作る |
| `runtime.close_document` | session、lease、Working Documentを解放する |
| `runtime.preview_operations` | current headに対してHostOperationPreviewInputを評価する |
| `runtime.get_authoring_grant` | current Actor / agentのeffective AuthoringGrantSummaryを返す |
| `runtime.evaluate_authoring_preview` | preview済みAuthoringImpactに対するdecisionを返す |
| `runtime.commit_operations` | LDL規範OperationBatchをcommitする |
| `runtime.commit_source_patch` | 確認済みrevision-protected source patchをcommitする |
| `runtime.commit_registry_plan` | 確認済みProjectMutationPlanをrevision precondition付きでcommitする |
| `runtime.get_operation_result` | operation IDまたはidempotency keyからpending / recovery / final結果を取得する |
| `runtime.stage_asset` | content digest検証付きtemporary asset refを作る |
| `runtime.save_document` | commit済みrevisionをexternal file / containerへmaterializeする |
| `runtime.plan_reconcile` | definition、state、provider headのreconcile planを作る |
| `runtime.apply_reconcile` | 確認済みreconcile planをcommitする |
| `runtime.list_revisions` | historyをpage取得する |
| `runtime.restore_revision` | 過去revisionを新しいCommitted Revisionとして復元する |
| `runtime.export_document` | `.ldl` source treeまたは`.layerdraw`をDocument I/Oとして出力する |
| `runtime.search` | revision / Access / indexを固定してProject Searchを実行する |
| `runtime.analyze_graph` | revision / Accessを固定したsubgraphへGraph Analysisを実行する |

`runtime.stage_asset`、asset persist / deleteは`runtime-protocol`に固定した`asset:write`のHostOperationImpactをAccessへ渡す。temporary objectを作る前に早期判定し、definitionとasset manifestを公開するcommitではcurrent grantとevaluation digestを再評価する。拒否・期限切れのtemporary objectは正本へ参照せずGCする。

```text
GetOperationResultInput
  document_id
  operation_id? | idempotency_key?

RuntimeOperationStatus
  phase: pending | staged | publication_pending | published | recovering | final
  operation_id
  idempotency_key
  operation_result?: OperationResult
  recovery_started_at?
  retry_after_ms?
```

timeout後のcallerは`runtime.get_operation_result`を使い、同じOperationBatchを別keyで再送しない。`recovering`はRuntimeがpending record、document head、state head、outboxを照合している状態であり、証明できればfinalなOperationResult、証明不能なら`needs_review`へ収束する。

### 4.3 Consumer-owned ports

port interfaceはconsumerであるRuntime側に定義する。adapter packageがinterfaceを定義してRuntimeへ押し付けない。

#### DocumentStore

```go
type DocumentStore interface {
  GetHead(context.Context, GetDocumentHeadInput) (DocumentHead, error)
  ReadRevision(context.Context, ReadRevisionInput) (RevisionSnapshot, error)
  ReadSourceBlobs(context.Context, ReadSourceBlobsInput) (SourceBlobSet, error)
  StageRevision(context.Context, StageRevisionInput) (StagedRevision, error)
  PublishHead(context.Context, PublishDocumentHeadInput) (PublishHeadResult, error)
  AbortStagedRevision(context.Context, AbortStagedRevisionInput) error

  PutOperationRecord(context.Context, PutOperationRecordInput) (OperationRecord, error)
  GetOperationRecord(context.Context, GetOperationRecordInput) (OperationRecord, error)
  FinalizeOperationRecord(context.Context, FinalizeOperationRecordInput) (OperationRecord, error)
}
```

`PublishHead`はexpected revision、definition hash、provider version token、fencing tokenを必須とする。成功したconditional updateがCommitted Revisionのpublication pointである。

#### StateBackend

`StateBackend`は[state-backends.md](state-backends.md)のhead、read、write、lease、audit、snapshot contractを実装する。RuntimeはStateBackendから読んだraw stateへAccess decisionを適用し、immutable StateQuerySnapshotを構築する。

#### AssetStore

```go
type AssetStore interface {
  Stat(context.Context, AssetRef) (AssetMetadata, error)
  Get(context.Context, AssetRef) (io.ReadCloser, error)
  PutIfAbsent(context.Context, PutAssetInput) (AssetMetadata, error)
  DeleteIfUnreferenced(context.Context, DeleteAssetInput) error
}
```

asset identityはcontent digestであり、source内のlogical asset pathとは分離する。Runtimeはmanifestでlogical pathからdigestへbindingする。

```text
StageAssetInput
  runtime_session_id
  media_type
  content_blob
  expected_digest
  logical_name?

StageAssetResult
  staged_asset_ref
  digest
  size
  media_type
  expires_at
```

staged asset refはRuntime sessionとActorへ束縛し、OperationBatchのAssetRefが同じdigestを参照してcommitされた時だけpersistent manifestへ昇格する。期限切れ、別session、digest不一致のrefを拒否する。

#### HistoryStore

```go
type HistoryStore interface {
  AppendRevision(context.Context, AppendRevisionIndexInput) (RevisionMetadata, error)
  GetRevision(context.Context, GetRevisionMetadataInput) (RevisionMetadata, error)
  ListRevisions(context.Context, ListRevisionsInput) (RevisionPage, error)
  ResolveProviderVersion(context.Context, ResolveProviderVersionInput) (ProviderRevisionRef, error)
}
```

HistoryStoreはcanonical source bytesの正本ではない。DocumentStore revisionまたはprovider-native immutable versionへのindexを持つ。

#### ArtifactStore

```go
type ArtifactStore interface {
  PutArtifact(context.Context, PutArtifactInput) (ArtifactRef, error)
  GetArtifact(context.Context, GetArtifactInput) (io.ReadCloser, error)
  ListArtifacts(context.Context, ListArtifactsInput) (ArtifactPage, error)
  DeleteArtifact(context.Context, DeleteArtifactInput) error
}
```

preview / export artifactはrevision、View address、input hash、Source Manifestへbindingする。artifactをdefinition revisionの代用にしない。

#### CredentialBroker

```go
type CredentialBroker interface {
  Acquire(context.Context, CredentialRequest) (CredentialLease, error)
  Refresh(context.Context, CredentialLeaseRef) (CredentialLease, error)
  Release(context.Context, CredentialLeaseRef) error
}
```

`CredentialLease`はadapterだけが利用できるopaque provider connectionを内包する。Engine、LDL、state、audit、MCP response、CapabilityManifestへsecretを渡さない。wireにはcredential materialではなくconnection IDとpermission summaryだけを出す。

#### EmbeddingProvider

```go
type EmbeddingProvider interface {
  Describe(context.Context) (EmbeddingCapability, error)
  EmbedDocuments(context.Context, EmbedDocumentsInput) (EmbeddingBatch, error)
  EmbedQuery(context.Context, EmbedQueryInput) (QueryEmbedding, error)
}
```

RuntimeはGo Engineが生成したSearchDocumentへAccess projectionを適用してからEmbedding Providerを呼ぶ。providerはStableAddress、LDL、Access decision、Search rankingを解釈せず、versioned Embedding Profileに従うvectorだけを返す。credentialはCredentialBrokerまたはprovider adapter内に閉じ、Engine、MCP response、SearchResultへ渡さない。

#### SearchIndexStore

```go
type SearchIndexStore interface {
  Describe(context.Context, SearchIndexIdentity) (SearchIndexStatus, error)
  ApplyPlan(context.Context, SearchIndexPlan) (SearchIndexApplyResult, error)
  Activate(context.Context, ActivateSearchIndexInput) (SearchIndexStatus, error)
  Invalidate(context.Context, InvalidateSearchIndexInput) error
}
```

SearchIndexStoreはLadybug native / WASM、sidecar service等の物理indexを管理するadapter portである。indexはhost revisionまたはportable generationを表すDocumentSnapshotRefとAccess projectionへ束縛された派生cacheであり、DocumentStoreまたはStateBackendの正本にならない。完全に構築・検証されたindexだけを`Activate`し、更新途中のindexを要求snapshotとして公開しない。

### 4.4 Browser and native adapter difference

| Concern | Browser adapter | Native adapter |
| --- | --- | --- |
| filesystem | File System Access API、upload/download、host callback | OS filesystem、repository、mounted volume |
| credential | provider OAuth SDKまたはhost callback。secretはbrowser session内 | keychain、IAM role、environment、OIDC broker |
| process | Go WASM Workerまたはremote host | in-process Go、sidecar、server |
| conditional write | provider API capabilityに依存 | filesystem lock、object generation、DB transaction |
| realtime | WebSocket / SSE client | in-process hub、WebSocket server/client |
| durable background work | page lifetimeに制約 | daemon / service jobとして実行可能 |
| embedding | browser modelまたはhost callback | local model、remote provider、service adapter |
| search index | Ladybug WASMのsession / persisted browser storageまたはremote host | Ladybug native、sidecar、service-managed index |

差はport capabilityだけであり、commit、reconcile、hash、diagnosticの意味を変えない。browserがlease / conditional writeを提供できない場合、multi-writer capabilityをdisabledにする。

### 4.5 Open lifecycle

```text
runtime.open_document
  1. Actor / Access decisionを解決
  2. DocumentStore headとsource closureを固定
  3. resolved dependency closureとasset manifestを読む
  4. Engineへsource treeをopenする
  5. backend bindingを解決しStateBackend headを読む
  6. stateをAccess projectionしStateQuerySnapshotを固定
  7. History / realtime capabilityを評価
  8. RuntimeSessionを返す
```

```text
OpenRuntimeDocumentResult
  runtime_session_id
  document_id
  committed_revision
  document_handle
  document_generation
  definition_hash
  state_summary
  access_summary
  capability_manifest
  diagnostics
```

open中にRegistryへfallback fetchしない。resolved pack treeが欠損している場合はRegistry repairを明示的に要求する。

### 4.6 Working Document

Working DocumentはRuntime session内のtransient source treeである。

- 0件以上の未commit operationを含められる。
- parse途中やsemantic errorを含められる。
- Query、View、Export、read-only MCPの既定入力にしない。
- `preview_working`を明示した同一sessionだけ、working generationへscopeしてpreviewできる。
- optional recovery storeへ暗号化snapshotを保存できるが、Committed Revision、Time Machine、監査証跡として扱わない。

Working Documentの変更は`working_generation`を進める。Committed Revision publication後、canonical source treeでWorking Documentをcheckpointする。

### 4.7 Commit lifecycle

```text
runtime.commit_operations
  1. idempotency recordを取得またはpending作成
  2. current head、lease、Actor、AuthoringPolicy / Grant、preconditionを固定
  3. Engine Workbenchでoperationを適用し完全compile、AuthoringImpactを再計算
  4. AccessでAuthoringDecisionを再評価し、allow時だけ未公開stageへ進む
  5. source / dependency / asset manifestとpending outbox eventを未公開stageへ書く
  6. definition headをconditional publishする
  7. StateBackend writeを同じoperation_idで実行する
  8. semantic operation logとauditをappendする
  9. OperationResultをfinalizeしoutbox eventをreadyへ進める
  10. Realtime adapterがready eventをpublishする
```

```text
RuntimeCommitInput
  runtime_session_id
  operation_id
  operation_batch: OperationBatch
  authoring_proof?
  expected_state_version?
  lease_token?
  state_mutation?
  trigger: explicit_save | autosave | realtime_checkpoint | agent_apply | registry_install | restore
```

`operation_batch`はLDL詳細仕様11.1節のcomplete OperationBatchであり、`document_id`、`base_revision`、`idempotency_key`、expected hash集合、operationsを外側へ重複させない。scoped fragmentはpreviewでsemantic operationsへ解決してOperationBatchとしてcommitする。

source patchだけはOperationBatchで損失なく表せないため、別operationを使う。

```text
RuntimeSourcePatchCommitInput
  runtime_session_id
  operation_id
  idempotency_key
  base_revision
  expected_source_digest
  lease_token?
  patch
    module_path
    source_range
    replacement_text
  trigger: explicit_save | autosave | realtime_checkpoint | agent_apply
  authoring_proof?
```

`runtime.commit_source_patch`も同じDocumentStore publication、OperationResult status、state / audit / outbox recoveryを通り、完全compile成功前にheadをpublishしない。source patch fieldをOperationBatchへ追加しない。

```text
RuntimeCommitResult
  operation_id
  operation_result: OperationResult
  state_version?
  semantic_diff?
  authoring_impact
  authoring_decision
  repair_actions[]
```

`OperationResult`はLDL詳細仕様11.3節をそのまま使う。head publication前の失敗は`rejected`、publication後のstate / audit不整合は`committed_state_stale`、publication成否を証明できない時だけ`needs_review`とする。wire-validなcommit requestはResponseEnvelopeの`outcome=success`でRuntimeCommitResultを返し、OperationResultの`rejected`をtransport rejectionへ潰さない。wire / session / authorization自体の拒否だけResponseEnvelopeの`outcome=rejected`を使う。

Runtime transaction phaseは次へ固定する。

```text
pending -> staged -> publication_pending -> published
published -> state_pending -> audit_pending -> outbox_ready -> final
publication_pending -> recovering -> final | needs_review
published/state_pending/audit_pending -> recovering -> final
```

pending outbox recordはhead publication前にdurableでなければならない。publication後にoutbox ready化が失敗した場合、recoveryがheadとpending eventを照合してreadyへ進める。broadcast成功はcommit条件ではないが、durable outboxなしでheadをpublishしない。state / audit repair中はOperationResultを`committed_state_stale`へ収束させ、eventにstate statusを含める。

### 4.8 Save and autosave

`save`は別の意味論を持つwrite primitiveではない。

- server-backed環境では`save`は`runtime.commit_operations(trigger=explicit_save)`へmapする。
- file-backed環境ではcommit後のcanonical source / containerをExternalFileStoreへmaterializeし、provider version tokenをrevision metadataへ記録する。
- autosaveは同じcommit protocolを`trigger=autosave`で呼ぶ。validationやpreconditionを省略しない。
- autosaveは短時間のoperationをcoalesceできるが、異なるActor、異なるidempotency key、破壊的operationを暗黙に束ねない。
- invalid Working DocumentはCommitted Revisionとしてautosaveしない。optional recovery snapshotだけを更新する。

### 4.9 Reconcile

reconcileは差分を黙って修正するoperationではなく、planとapplyに分ける。

```text
runtime.plan_reconcile
  -> ReconcilePlan
       current_definition
       current_state
       provider_head
       stale_subjects
       address_moves
       orphan_actions
       conflicts
       plan_digest

runtime.apply_reconcile(plan_digest, preconditions, decisions)
  -> RuntimeCommitResult
```

actionは`remap_state`、`refresh_state`、`tombstone_state`、`archive_orphan`、`accept_provider_head`、`publish_local_head`、`manual_review`の閉じた集合とする。reconcileも同じcommit / idempotency / audit境界を通る。

### 4.10 Server Application host metadata

Server ApplicationはGo Runtimeの外側で、Self-hostを含むserver-backed利用のHost metadataを所有する。

```text
ServerInstance
  Organization
    OrganizationMembership
    Team / ServiceAccount
    OrganizationPolicy
    AuthoringPolicy
    StorageBinding / RegistryBinding
    Workspace
      ProjectRecord
        document_id
```

規範条件:

- Server Projectはexactly one OrganizationとWorkspaceに所属する。
- Organization / Workspace IDはhost-ownedで、LDL、StableAddress、definition hash、portable container identityへ含めない。
- User Actorは複数Organization membershipを持てる。Team / Service Accountは1つのOrganizationにscopeする。
- metadata list / search、ACL、share、history、audit、asset、artifact、realtime joinはOrganization scopeをrequestまたは解決済みsessionから必ず得る。
- cross-organization resource lookupは存在を漏らさず`not_found_or_not_authorized`相当へ一般化できる。
- ProjectのWorkspace移動はdefinition revisionを作らない。Organization移動はstorage / registry binding、ACL、audit scopeを検証する明示Host operationとする。
- Go Runtimeへ渡す前にServer ApplicationがOrganization、Workspace、Actor、Access fingerprint、Document IDを解決する。Runtime / EngineがOrganization directoryを問い合わせない。
- EntitlementProviderはimmutable EntitlementSnapshotを返し、UsageSinkはscope付きUsageEventを受け取る。どちらも商用契約や請求の意味論を持たない。
- AuthoringPolicyはHost metadataであり、`.ldl`、`.layerdraw`、`.ldpack`、StateQuerySnapshotへ含めない。Project open時にpolicy / membership versionへ固定したAuthoringGrantSnapshotをRuntimeへ渡す。

`server-application-protocol`は少なくとも次のoperation groupを持つ。

| Group | Operations |
| --- | --- |
| Instance | describe / settings / organization listing |
| Organization | create / read / update / archive / membership / Team / Service Account / policy / binding |
| Workspace | create / list / read / update / archive / membership |
| Project directory | create / list / search / move / archive / settings / open handoff |
| Access / Authoring Policy / Share | grant / revoke / AuthoringPolicy create / bind / explain / invite / link / effective access explanation |
| Audit | organization / workspace / project scoped listing |
| Entitlement / Usage | effective entitlement summary / quota status。UsageEvent ingestはtrusted internal portだけに公開する |

個別operationは共通RequestEnvelope / ResponseEnvelope、Actor context、cursor、idempotency、CapabilityManifestを使う。server-clientが手書きの別wire型を定義しない。

Project settings、backend / package policy等のmetadata writeは、`server-application-protocol`のclosed operationから`project:configure`のHostOperationImpactを生成する。Server Applicationはcurrent metadata ETag、membership、AuthoringPolicyを固定してAccess評価し、ETagとaccess fingerprintをpreconditionにconditional publicationする。RuntimeへOrganization / Workspace / Project metadata transactionを委譲しない。

AuthoringPolicy create / update / bind / unbindはAuthoring operationではなく、`server-application-protocol`のResource Access operationとして変更前snapshotの`access:manage`で判定する。HostOperationImpact、変更後Policy、`project:configure`、client自己申告grantを判定入力に使わない。Policy metadata publicationとaccess fingerprint更新は同じmetadata transactionまたは再実行可能sagaへ束縛する。

Server Applicationのrepository portは次の不変条件を守る。

- `OrganizationRepository`、`WorkspaceRepository`、`IdentityRepository`、`AccessRepository`、`AuthoringPolicyRepository`、`ProjectRepository`はGo application componentが所有するconsumer-side portであり、SQL schemaやprovider SDK型をpublic protocolへ漏らさない。
- Organization配下resourceの`get`、`list`、`search`、`update`、`archive`は`organization_id`を必須引数または解決済みscopeとして受ける。unscoped lookupを通常application pathへ公開しない。
- mutable recordはopaque host ID、monotonic versionまたはETag、status、`created_at`、`updated_at`を持ち、更新はexpected version付きconditional writeにする。
- archiveは既存Document revisionやaudit eventを物理削除せず、既定のlist / openから除外する。retentionに基づく物理削除は別operationとする。
- Organization作成とdefault Workspace作成、ProjectRecord作成とDocument ID発行・初期ACL・AuthoringPolicy binding・initial Committed Revision作成は、単一metadata transactionまたは再実行可能なsagaとして扱い、部分作成resourceを通常listへ公開しない。fixed semantic modelではPolicyを持たないinitial revisionを一時公開しない。
- cross-store operationは同じ`operation_id`とidempotency keyを使い、再試行でOrganization、Workspace、Project、ACL、Documentを重複作成しない。
- Workspace間Project移動はProjectRecordのconditional updateであり、definition revisionを生成しない。Organization間移動はcopy / rebind / ACL再評価 / audit / old-scope tombstoneを含むpreviewable planとして扱い、単純なforeign key updateにしない。
- `IdentityRepository`はexternal Actor mappingとprovider bindingだけを保持し、password、OAuth token、storage credentialを保持しない。
- Access decisionへ渡すmembership、role、policy、entitlement inputは、同じOrganization scopeとpolicy / membership versionへ固定する。判定中に別scopeのrecordを混ぜない。

### 4.11 Authoring Access enforcement

Authoring Accessのcapability、impact、policy、decisionの規範は[authoring-access-control.md](authoring-access-control.md)に従う。

```text
HostOperationPreviewResult
  workbench_preview
  host_operation_impacts[]
  authoring_decision
  authoring_proof

AuthoringProof
  base_revision
  evaluation_digest
  decision_digest
  access_fingerprint
  policy_versions[]
  membership_version
  expires_at?
```

host-backed previewはEngineのAuthoringImpactとowner protocolが宣言したHostOperationImpactをAccessへ渡し、decisionとproofを返す。portable Engine previewはAuthoringImpactだけを返す。

AuthoringProofはpreviewとcommitの同一性preconditionであり、grant tokenではない。clientがproofを改変・省略しても権限を取得できず、Runtimeはtrusted repositoryからGrantSnapshotとPolicyを再解決する。

Runtime commitは次を必須とする。

1. current head、membership、AuthoringPolicy、agent delegationを同じrequest scopeへ固定する。
2. current headへ変更を再previewし、AuthoringImpact、HostOperationImpact、両者を束縛するevaluation digestを再計算する。
3. resulting definitionに対して全AuthoringPolicy constraint address / actionの互換性を検証する。
4. supplied proofのbase revision、evaluation digest、policy / membership versionを比較する。
5. Accessが`apply` / `publish` decisionを再評価する。
6. `allow`の場合だけDocumentStore stage / publicationへ進む。

decisionが`deny`または`approval_required`ならdefinition、state、asset manifest、history、outboxをpublishしない。mixed-capability batchを部分適用しない。Runtime handlerが自由文字列のoperation名やsubject kindからcapabilityを再分類してはならず、versioned owner protocolに固定されたHostOperationImpactだけを使う。

source patch、import、Registry plan、Template適用、restore、reconcile、realtime checkpoint、autosaveはすべて同じAuthoringImpact / Decision gateを通る。asset、Project Host設定、Package transaction等のLDL外authoring writeはHostOperationImpact / Decision gateを通る。一つのintentが両者を含む場合はrequired capabilityの和集合を同じevaluation digestで原子的に判定する。AuthoringPolicy変更は変更前snapshotのResource Access decisionを通す。`package:manage`だけでRegistry resultのSchema変更を許可しない。

restricted Projectのprovider headがRuntime外で変わった場合は自動採用せず、Actorとauthorizationを解決できるreconcile previewか`authoring.external_change_quarantined`へ進める。

Schema / Pack変更でPolicy StableAddressが削除・移動・kind変更される場合、authorized policy migrationをdefinitionと同じHost transactionへ含めるか`authoring.policy_incompatible`で拒否する。Policyを先に緩めてunrestricted windowを作らない。

## 5. Realtime Boundary

### 5.1 Ownership split

| Concern | Owner |
| --- | --- |
| semantic operation、validation、canonical source | Engine / Workbench |
| Working Document、commit、revision、conflict result | Runtime |
| room、connection、ordering、presence、broadcast | Realtime adapter |
| CRDT / OT / command-log内部表現 | Realtime strategy adapter |
| Actor / room join permission | Access + Runtime host |
| working change AuthoringImpact / decision / contribution attribution | Engine + Access + Runtime |

Realtime adapterはsourceをcommitせず、Engine semanticsを実装しない。

Runtimeが所有するport名を`RealtimeProvider`、接続済みroom handleを`RealtimeRoom`へ固定する。

```go
type RealtimeProvider interface {
  OpenRoom(context.Context, OpenRealtimeRoomInput) (RealtimeRoom, error)
  ResumeRoom(context.Context, ResumeRealtimeRoomInput) (RealtimeRoom, error)
  Publish(context.Context, PublishRealtimeEventInput) error
  CloseRoom(context.Context, CloseRealtimeRoomInput) error
}

type RealtimeRoom interface {
  Snapshot(context.Context) (RealtimeRoomSnapshot, error)
  Submit(context.Context, RealtimeClientMessage) (RealtimeSubmitResult, error)
  Events() <-chan RealtimeEvent
  Close() error
}
```

`RealtimeStore`という別componentは定義しない。provider implementationは必要に応じてephemeral room stateやSyncStrategyLogを内部保存できる。

### 5.2 Durable and transient logs

二つのlogを区別する。

- `SemanticOperationLog`: Runtimeが永続化する、Committed Revisionへ採用されたcanonical operation batchとmetadata。
- `SyncStrategyLog`: CRDT / OT / command-log adapterがWorking Document同期のために使う内部log。checkpoint後にcompactでき、Time Machineの正本ではない。

transport messageやキー入力をSemanticOperationLogへそのまま保存しない。

### 5.3 Room protocol

```text
realtime.join
  input: document_id, requested_revision?, actor_context_ref, client_id, capability
  output: room_id, participant_id, committed_revision, working_generation,
          presence_snapshot, capability_manifest, authoring_grant_summary,
          resume_token, checkpoint_token

realtime.submit_working_change
  input: room_id, working_generation, base_revision, change, client_event_id
  output: accepted | rejected, contribution_id?, next_working_generation, authoring_impact?,
          authoring_decision?, diagnostics, conflicts

realtime.commit_checkpoint
  input: room_id, working_generation, checkpoint_token, contribution_set_digest
  output: RuntimeCommitResult

realtime.update_presence
  input: room_id, presence_sequence, pointer?, selection?, active_view?, typing?

realtime.leave
```

server eventは次のclosed unionである。

```text
RealtimeEvent
  room_sequence
  event_id
  kind:
    participant_joined
    participant_left
    presence_updated
    working_change_accepted
    working_change_rejected
    working_diagnostics
    commit_published
    commit_state_stale
    checkpoint_reset
    capability_changed
  payload
```

`room_sequence`はroom内のdelivery orderであり、document revisionではない。presence eventはlossyでよいが、`commit_published`をlossyにしてはならない。reconnect時はresume tokenを試し、gapがあればcurrent committed revisionとcheckpointを再取得する。

`checkpoint_token`はroom ID、runtime session ID、checkpoint trigger Actor、Access fingerprint、base revisionへ束縛する。`commit_checkpoint`はtokenからRuntime sessionとtriggerを復元し、callerに`runtime_session_id`や任意のOperationBatchを重複送信させない。room、session、trigger Actor、Access fingerprint、Working generation、contribution setのいずれかが一致しなければ`realtime.checkpoint_scope_mismatch`で拒否する。

受理したworking changeは`contribution_id`、origin Actor / agent delegation、operation digest、AuthoringImpact、decision、Access fingerprint、Policy versionへ束縛する。checkpoint時はRuntimeがcurrent headと固定済みcontribution setからcanonical OperationBatchとfinal AuthoringImpactを再生成し、全impact entryが原因contributionのcurrent grantでcoveredか再評価する。merge / cascadeで生じ、原因contributionへ決定論的に帰属できないentryはrebaseまたはreviewを要求する。一件でもdeny、stale、uncoveredならcheckpoint全体をpublishしない。

checkpoint triggerとautosave service actorはpublication開始権限だけを持ち、contributionのAuthoring Capabilityを代替しない。SemanticOperationLogとAuditはrevision、contribution ID、origin Actor、decision digestの対応を保持する。

presenceの`pointer`はdiagram上の座標でありpagination cursorではない。selectionとactive viewは送信前とbroadcast前の両方でAccessを評価し、read権限のないStableAddress / Viewはeventから除去する。participant sessionはjoin時のAccess fingerprintへ束縛し、permission / AuthoringPolicy変更時はcapability changed、AuthoringGrantSummary、presence snapshot再同期を行う。

### 5.4 Conflict contract

```text
SemanticConflict
  kind:
    stale_revision
    stale_subject
    stale_subtree
    child_set_changed
    same_field_update
    delete_versus_update
    duplicate_symbol
    endpoint_changed
    access_revoked
    working_generation_changed
  target_addresses[]
  base_value?
  current_value?
  proposed_value?
  participants[]
  resolution_options[]
```

auto-mergeできるのは、preconditionが証明する非重複subject / field変更だけである。display nameの一致や最終write時刻だけで上書き順を決めない。

### 5.5 CRDT / OT / command-log adapter rule

strategy adapterは`WorkingChange`を受け、次のどちらかをRuntimeへ返す。

- schema-valid semantic operation batch
- expected source digestとscopeを持つrevision-protected source patch

CRDT documentやOT deltaをEngineへ直接渡さない。strategyが異なっても、commit時には同じWorkbench、precondition、canonical format、OperationResultを通す。

### 5.6 Commit and broadcast ordering

1. Runtimeがpending operation recordとpending `commit_published` outbox eventをdurableにstageする。
2. Runtimeがdefinition headをpublishする。
3. RuntimeがOperationResultを確定し、pending outbox eventへrevisionとstate statusを記録してreadyにする。
4. Realtime adapterがready eventをbroadcastする。
5. ready化またはdelivery失敗時はrecovery / outbox replayで再開する。

broadcast成功をcommit成立条件にしないが、durable pending outboxなしでheadをpublishしない。clientは`commit_published`を受け取れなくてもrevisionを再取得できる。

### 5.7 Capability conditions

Desktop、VSCode、SDKにRealtime codeをbundleできても、RealtimeProvider、multi-writer lease、Access、event transportが構成されていなければcapabilityはdisabledである。Feature x Deliveryの対応は「組込み可能」を意味し、local-only modeで常時有効という意味ではない。

## 6. Search / Query / Analysis Execution Adapter Boundary

### 6.1 Boundary rule

Search、Structural Query、Graph Analysisの意味評価はGo Engineだけが所有する。execution adapterはGo Engineが生成したparameterized planを実行し、typed raw rowsを返す。adapterは次を行わない。

- SearchDocumentの文字列化、embedding生成、Hybrid fusion
- Query recipe、filter、traversal、orderingの解釈
- Analysis scope、algorithm resultのStableAddress binding
- StableAddressの構築または補完
- access / redaction decision
- semantic deduplication
- SearchResult、QueryResult、AnalysisResult、ViewData、StateRefsの生成

native Ladybug adapterとbrowser Ladybug WASM adapterは同じprepare / execute / complete契約を通る。意味と永続性の分離は[search-query-and-analysis.md](search-query-and-analysis.md)を規範とする。

### 6.2 Adapter capability

```text
QueryAdapterCapability
  adapter_id
  backend: ladybug_native | ladybug_wasm
  plan_protocol_range
  supported_scalar_types[]
  max_parameters
  max_rows
  max_result_bytes
  supports_cancellation
  supports_streamed_batches
  supports_read_transaction
  physical_primitives[]
  backend_version
```

Engineはcapabilityの範囲内でだけplanを生成する。capability不足時に別のQuery意味へdegradeせず、`query.adapter_capability_missing`で拒否する。

`physical_primitives`はbackendが実行できる閉じた物理能力IDであり、少なくとも`structural_match`、`typed_predicate`、`directed_traversal`、`shortest_path`、`fts_bm25`、`vector_hnsw`、`vector_filtered_search`、`algo_page_rank`、`algo_k_core`、`algo_louvain`、`algo_scc`、`algo_wcc`を定義する。これはLDL operatorやpublic manual query APIではない。どのphysical primitiveへcompileするかはEngine plannerとplan protocolの責務である。

公式Query-capable bundleは上記primitiveをすべて必須とし、欠落時はrelease conformance failureとする。third-party adapterはsubsetを表明できるが、必要primitive不足時にFTS-only、全件走査、別algorithmへ暗黙fallbackしない。

### 6.3 Execution port

native executionではEngine facadeがconsumer-owned portを定義する。

```go
type QueryExecutionPort interface {
  Capabilities(context.Context) (QueryAdapterCapability, error)
  Execute(context.Context, ExecuteQueryPlanInput, QueryRowSink) (QueryAdapterExecutionResult, error)
  ExecuteSearch(context.Context, ExecuteSearchPlanInput, SearchHitSink) (QueryAdapterExecutionResult, error)
  ExecuteAnalysis(context.Context, ExecuteAnalysisPlanInput, AnalysisRowSink) (QueryAdapterExecutionResult, error)
  Cancel(context.Context, CancelQueryExecutionInput) error
}

type QueryRowSink interface {
  AppendBatch(context.Context, TypedRawRowBatch) error
  Complete(context.Context, CompleteRawRowsInput) error
}
```

```text
ExecuteQueryPlanInput
  adapter_execution_id
  plan
  deadline_at?

QueryAdapterExecutionResult
  adapter_execution_id
  status: complete | cancelled | failed
  row_count
  byte_count
  backend_correlation_id?
  safe_failure?
```

native Ladybug adapterはこのGo portを実装する。browserでは`@layerdraw/query-adapter-ladybug-wasm`がgenerated `query-adapter-protocol`型を実装し、`@layerdraw/engine-client`がprepare、adapter execute、completeをorchestrateする。どちらも同じplanとTypedRawRows fixtureを使う。package名の`query-adapter`はLadybug execution SPI全体を指し、Structural Queryだけへ限定しない。

Engineのin-process convenience facadeはprepare、port Execute、completeを一回で呼べるが、内部段階を省略しない。Engine Protocolの権威operationはSearch、Query、Analysisそれぞれの`prepare`、`complete`、`cancel`であり、execution adapter呼出はEngine Protocolとは別の内部portである。

### 6.4 QueryExecutionPlan

`QueryExecutionPlan`はEngineとexecution adapter間だけのversioned internal wire型である。SDK利用者がbackend queryを手書きするpublic APIではない。

```text
QueryExecutionPlan
  plan_protocol_version
  plan_id
  plan_digest
  backend
  document_generation
  definition_hash
  state_input_digest
  query_address
  argument_digest
  parameter_schema[]
  parameters[]
  raw_row_schema
  backend_payload
  limits
    max_rows
    max_bytes
    deadline_at?
  required_capabilities[]
```

`backend_payload`はEngineが生成したparameterized statementとbindingであり、adapterが文字列補間しない。log、diagnostic、telemetryへraw statementを既定出力しない。

#### 6.4.1 SearchExecutionPlan

```text
SearchExecutionPlan
  plan_protocol_version
  plan_id
  plan_digest
  backend
  document_generation
  document_snapshot_ref
  index_identity_digest
  search_mode
  filter_schema
  parameter_schema[]
  parameters[]
  raw_hit_schema
  backend_payload
  limits
  required_capabilities[]
```

Go EngineはSearchRequestを正規化し、Hostが固定したDocumentSnapshotRef、Access projection、index identity、Embedding Providerが返したquery vectorをtyped inputとしてplanへ束縛する。query vectorはadapter入力に含められるがSearchResult、log、MCP responseへ返さない。Hybridの場合、adapterはFTS候補とVector候補のrank / raw scoreを分けて返し、RRFはGo Engineが`complete_search`で行う。

```text
PrepareSearchInput
  document_handle
  document_generation
  document_snapshot_ref
  search_request
  index_identity
  query_embedding?
  adapter_capability_id

PrepareSearchResult
  search_execution_token
  plan
  expires_at

CompleteSearchResult
  search_result
  search_result_hash
```

search tokenはdocument generation、DocumentSnapshotRef、query digest、index identity、Access fingerprint、plan digestへ束縛する。cursorはcomplete後のcanonical SearchResult paginationに対して発行し、raw backend cursorを外部へ出さない。

#### 6.4.2 AnalysisExecutionPlan

```text
AnalysisExecutionPlan
  plan_protocol_version
  plan_id
  plan_digest
  backend
  document_generation
  document_snapshot_ref
  input_subgraph_hash
  algorithm
  algorithm_profile_id
  parameters[]
  raw_metric_schema
  backend_payload
  limits
  required_capabilities[]
```

```text
PrepareAnalysisInput
  document_handle
  document_generation
  document_snapshot_ref
  scope: query_result_hash | explicit_subgraph
  algorithm
  typed_parameters
  adapter_capability_id

PrepareAnalysisResult
  analysis_execution_token
  plan
  expires_at

CompleteAnalysisResult
  analysis_result
  analysis_result_hash
```

Analysis scopeはAccess適用済みcanonical QueryResultまたはendpoint closureを満たす明示address集合に限定する。Search cursorや順位条件をplanへ持ち込まない。adapterはmetric rowを返し、StableAddress binding、float normalization、community / component ordering、result hashはGo Engineが行う。

### 6.5 TypedRawRows

```text
RawRowSchema
  schema_digest
  columns[]
    column_id
    scalar_type
    nullable
    cardinality: one | many

TypedRawRows
  plan_id
  plan_digest
  schema_digest
  batches[]
    batch_sequence
    rows[]
      cells[]
  row_count
  byte_count
  complete
```

cellはclosed tagged scalar unionとする。

```text
RawScalar
  null
  boolean
  int64_decimal_string
  uint64_decimal_string
  decimal_string
  float64
  string
  bytes_blob_ref
  date
  timestamp
  duration
  stable_address_string
  list<RawScalar>
```

object / arbitrary JSONをraw scalarとして許可しない。必要なstructured valueはschema version付きの専用columnへ展開する。rowの意味、source ref、orderingはEngineがplan bindingから復元する。

各rowのcell数と順序は`RawRowSchema.columns`に完全一致しなければならない。`cardinality=one`は単一scalarまたはnullable列のnull、`cardinality=many`はlistだけを許可する。non-nullable列のnull、nested list、列schemaと異なるscalar tag、余剰 / 欠損cellはoperationに応じた`query.raw_schema_mismatch`、`search.raw_schema_mismatch`、`analysis.raw_schema_mismatch`で拒否する。

batchは`batch_sequence`を0から昇順かつ欠番なく渡す。streaming adapterは対応するRowSinkへ複数batchを送れるが、最後に`Complete`を一度だけ呼ぶ。途中切断、adapter failure、cancelで`complete=false`のcollectionはすべて破棄し、対応するEngine `complete`へ渡さない。resume cursorは提供せず、新しいtokenで再prepare / reexecuteする。resource上限到達はpartial successではなくoperation固有の`result_limit_exceeded`とする。

### 6.6 State machine

```text
prepared -> executing -> completed
    |           |           |
    +--------> cancelled <---+
    +--------> expired
    +--------> invalidated
    +--------> adapter_failed
```

- `engine.prepare_query`、`engine.prepare_search`、`engine.prepare_analysis`は各tokenとplanを生成し`prepared`にする。
- adapter execution開始時に`executing`へ進めてよいが、状態報告はtransport最適化であり意味結果ではない。
- 対応する`engine.complete_*`成功でtokenをsingle-use消費し`completed`にする。
- document generation、state input、plan versionの変更で`invalidated`になる。
- cancel / expiry後のcompleteはoperation固有の`token_invalid`で拒否する。
- retryする場合は新しいtokenとplanをprepareする。古いraw rowsを新tokenへ流用しない。
- complete、cancel、expiry、invalidated、adapter failureのすべてでEngineはtoken accumulatorを破棄し、native portへCancelまたはreleaseを通知する。
- browser clientはadapter executionをcancelし、buffered batchとtransferable bufferを解放してからtokenを破棄する。
- adapter failureは`adapter_execution_id`とsafe backend correlation IDをProtocolFailureへ写し、raw backend errorを返さない。

### 6.7 Prepare and complete

```text
PrepareQueryInput
  document_handle
  document_generation
  query_address
  typed_arguments
  state_input
  adapter_capability_id
  limits?

PrepareQueryResult
  query_execution_token
  plan
  expires_at
```

`state_input`は`none`またはAccess適用済みimmutable StateQuerySnapshotである。`layerdraw-engine`はStateBackendやcredentialを持たず、snapshotのschema、hash、definition compatibility、redaction markerだけを検証する。

```text
CompleteQueryInput
  query_execution_token
  typed_raw_rows

CompleteQueryResult
  query_result
  query_result_hash
  state_refs
  omission_diagnostics
```

Engineはtoken binding、plan digest、row schema、row / byte count、batch sequenceを検証してからordering、deduplication、StableAddress resolution、StateRefs、canonical hashを生成する。

### 6.8 Failure contract

| Code | Outcome | Meaning |
| --- | --- | --- |
| `query.unknown_query` | rejected | Query addressが存在しない |
| `query.invalid_arguments` | rejected | typed argumentがschema不一致 |
| `query.state_required` | rejected | required state snapshotが無い |
| `query.state_incompatible` | rejected | definition / state hash不一致 |
| `query.state_redacted` | rejected | 必須fieldがredacted |
| `query.adapter_capability_missing` | rejected | planを実行できるadapter能力が無い |
| `query.token_invalid` | rejected | tokenがexpired、consumed、別generation |
| `query.raw_schema_mismatch` | rejected | TypedRawRowsがplan schema不一致 |
| `query.result_limit_exceeded` | failed | adapter resultが固定上限を超えた |
| `query.adapter_failed` | failed | Ladybug実行失敗 |
| `query.cancelled` | cancelled | 呼出側cancel |

SearchとAnalysisのstable failure codeは[search-query-and-analysis.md](search-query-and-analysis.md) 13章を規範とする。特にHybridのVector失敗をlexical successへdegradeせず、Search Index revision不一致をempty resultへ偽装しない。

adapter固有error textを利用者向けdomain diagnosticへ変換しない。safe failure codeとcorrelation IDを返す。

## 7. Registry Boundary

### 7.1 Ownership

```text
Registry service
  catalog、publisher metadata、artifact distribution

Go Registry component
  source identity、trust policy、signature decision、dependency resolution、transaction plan

Go Engine package facade
  ZIP safety、container manifest、LDL、resolved tree、canonical digest

Runtime / Workbench
  open projectへのsource / resolved lock / state migrationの原子的適用

TS Registry client / Library / MCP
  typed transport、表示、確認、進捗。意味論を持たない
```

### 7.2 Registry source contract

```text
RegistrySource
  source_id
  kind: official | organization_private | self_hosted | local_directory | git
  endpoint_ref
  trust_policy_id
  auth_connection_ref?
  cache_policy
  priority
```

raw access token、private key、provider credentialをsource configへ保存しない。`auth_connection_ref`はCredentialBrokerが解決する。

source identityはartifact identityの一部ではない。同じcanonical artifact IDを複数sourceが提示した場合、trust policyとsource priorityで候補を選ぶが、digestが異なる同一versionを黙って同一物として扱わない。

### 7.3 Artifact identity and dependencies

```text
RegistryArtifactIdentity
  kind: pack | template
  canonical_id
  version

ArtifactDependency
  canonical_id
  version_range
  digest_constraint?
```

- Packは`.ldpack`、Templateは`.layerdraw`だけを配布する。
- dependencyはすべてmandatoryとする。optional / peer dependencyを導入しない。
- 1 project内で同じcanonical Pack IDの複数versionを同時解決しない。
- dependency cycleは拒否する。
- version rangeはSemVerの閉じた規則で評価し、pre-releaseはrequestが明示した場合だけ候補にする。
- resolverはcanonical ID昇順、version降順、source priority順の決定的探索を行う。
- `layerdraw.resolved.json`にはexact version、artifact digest、source identity、dependency graph digestを固定する。

既存lockと両立しない要求は最新版へ勝手に更新せず、`registry.dependency_conflict`を返す。

### 7.4 Download contract

```text
DownloadArtifactInput
  source_id
  artifact_identity
  expected_digest?
  expected_size?

DownloadArtifactResult
  blob_ref
  resolved_source
  media_type
  size
  digest
  cache_status
  transport_attestation?
```

- download中はsize上限を強制する。
- expected digestがある場合、digest一致前にverification済みcacheへ昇格しない。
- cache keyはsource ID、canonical ID、version、digestである。
- offline modeはexact digest付きcache hitだけを許可する。
- mirrorから取得してもpublisher signatureとartifact digestを再検証する。
- resumeはtransport能力であり、最終digestが同じなら意味結果を変えない。

### 7.5 Signature contract

signature envelopeはartifact bytesそのものではなく、次のcanonical statementへ署名する。

```text
ArtifactSignatureStatement
  artifact_identity
  artifact_digest
  manifest_digest
  dependency_metadata_digest
  publisher_id
  issued_at
  expires_at?
```

```text
SignatureEnvelope
  profile
  key_id
  algorithm
  statement_digest
  signature
  certificate_chain?
  transparency_proof?
```

trust policyはsourceごとに、required signature、trusted publisher / key、revocation、expiry、local unsigned exceptionを定義する。`ed25519`署名profileとSigstore-compatible bundle profileを表現可能にするが、profileを自動変換しない。

key rotationは同じpublisher identityの署名付きkey metadataで行う。revoked keyで署名された未install artifactは拒否する。既にinstall済みartifactは即時削除せず、open時に`registry.signature_revoked`診断とdisable / replace / retainのpolicy decisionを要求する。

### 7.6 Install plan

```text
registry.plan_install
  input
    project_dependency_snapshot
    requested_artifact
    source_policy
    host_capabilities
  output: RegistryInstallPlan
```

```text
RegistryInstallPlan
  registry_transaction_id
  action: install | update | pin | remove | repair | create_from_template
  base_project_revision?
  requested_artifact
  resolved_artifacts[]
  dependency_graph
  signature_decisions[]
  compatibility_decisions[]
  staged_tree_manifest
  resolved_lock_delta
  required_source_edits[]
  authoring_impact
  host_operation_impact
    operation_kind: package_transaction
    required_authoring_capabilities: [package:manage]
  address_migration_plan?
  state_migration_proposal?
  rollback_checkpoint
  plan_digest
  expires_at
```

Registry planはproject sourceを直接変更しない。Go Engine package facadeのvalidation結果とhostのrenderer / exporter capabilityを入力にし、unsupported primitiveを`degraded`または`disabled`として明示する。

download / expansion前にRegistry hostは`registry-protocol`のclosed actionから`package:manage`のHostOperationImpactを生成して早期Access判定する。これは最終grantではなく、apply時にRuntimeが同じprotocol descriptorからHostOperationImpactを再生成し、plan内digest、resulting AuthoringImpact、current Policyと合わせて再評価する。clientがPlan内のrequired capabilityやdigestを省略・置換しても権限を取得できない。

```text
SignatureDecision
  artifact_identity
  artifact_digest
  status: verified | unsigned_allowed | missing | invalid | revoked | expired
  trust_policy_id
  publisher_id?
  key_id?
  evidence_digest?

CompatibilityDecision
  subject
  required_version_or_capability
  available_version_or_capability
  status: compatible | degraded | disabled | incompatible | migration_required
  diagnostics[]
```

`unsigned_allowed`はtrust policyが明示したlocal sourceでだけ成立する。open時のrevocation判定はRegistry componentがdecisionを作り、Runtime / Accessへ`disable`、`retain_readonly`、`replace_required`のclosed policy resultとして渡す。UIが独自にretain可否を決めない。

### 7.7 Install state machine

```text
planned
  -> downloading
  -> verified
  -> expanded_staged
  -> compiled
  -> awaiting_confirmation
  -> applying_project_change
  -> committed

any pre-commit state -> rolled_back
post-publication inconsistency -> repair_required
rollback failure -> needs_review
repair_required -> repairing -> committed
repairing -> superseded | needs_review
```

各遷移はtransaction recordへappendし、retry時に完了済みstepをdigestで検証して再利用する。

`rolled_back`はdefinition publication前だけの終端状態である。publication後に旧revisionへ戻す場合はRuntimeが新しいrestore revisionを発行し、元transactionを`superseded`としてそのrevisionへ参照させる。履歴から公開済みrevisionを消さない。

### 7.8 Apply handoff

Registry componentはRuntime sessionをimportしない。適用は次のhandoffで行う。

```text
registry.prepare_apply(registry_transaction_id, plan_digest, base_project_revision)
  -> verified staged tree + ProjectMutationPlan

runtime.commit_registry_plan(RuntimeRegistryCommitInput)
  -> RuntimeCommitResult

registry.finalize_apply(registry_transaction_id, plan_digest, committed_revision)
  -> RegistryInstallResult
```

```text
ProjectMutationPlan
  registry_transaction_id
  plan_digest
  base_project_revision
  expected_definition_hash
  expected_resolved_lock_digest
  staged_tree_manifest
  resolved_lock_delta
  source_edits[]
  address_migration_plan?
  state_migration_proposal?
  trust_policy_digest
  mutation_digest
  authoring_impact_digest
  host_operation_impact_digest
  evaluation_digest
```

`runtime.commit_registry_plan`は通常のRuntime request envelopeによるActor / Access判定に加え、`operation_id`、`idempotency_key`、base revision、expected definition hash、lease token、transaction / plan / mutation / AuthoringImpact / HostOperationImpact / evaluation digestを必須とする。計画後にproject revision、resolved lock、trust policy、AuthoringPolicy / membership、host capabilityが変わった場合は`registry.plan_stale`または`authoring.policy_changed`で拒否し、再planを要求する。

`ProjectMutationPlan`は`pack/` tree replacement、`layerdraw.resolved.json`更新、明示import edit、address migrationを含む。Workbenchがsource editを適用し、Engineが新しいclosure全体をcompileする。

```text
RuntimeRegistryCommitInput
  runtime_session_id
  operation_id
  idempotency_key
  base_project_revision
  expected_definition_hash
  expected_resolved_lock_digest
  lease_token?
  registry_transaction_id
  plan_digest
  mutation_plan: ProjectMutationPlan
  authoring_proof?
```

Registry applyはLDL OperationBatchではないため、`runtime.commit_operations`へ偽装しない。ただし`package:manage`を要求するHostOperationImpactとresulting definitionのAuthoringImpactを同じevaluation digest / Decision gateへ束縛し、DocumentStore publication、OperationResult status、state / audit / outbox recoveryを共有する。`package:manage`に加えてresulting diffが要求する`schema:write`、`query:write`、`view:write`等を全て満たさなければならない。

state migrationはRegistryがproposalを作り、RuntimeがStateBackend preconditionとAccess policyの下で適用する。RegistryがStateBackendへ直接writeしない。

Templateからの新規project作成も、Registryがverified template closureを返し、Workspace / Runtimeが新しいDocument IDを発行してinitial Committed Revisionを作る。template内のsource Document IDを引き継がない。

### 7.9 Rollback and repair

```text
RollbackCheckpoint
  base_project_revision
  base_definition_hash
  base_resolved_lock_digest
  current_pack_tree_manifest
  staged_object_refs[]
  pending_source_edit_digest
  pending_state_migration_digest?
```

publication前はcheckpointに従ってstaged treeとsource editを破棄し、旧resolved lockをcurrentのまま維持する。破棄できないcontent-addressed blobはunreferencedとして回収対象にし、project headから参照しない。rollback自体の成否を証明できなければ`needs_review`とする。publication後にstate / registry transaction finalizeが失敗した場合、definition revisionは消さず`repair_required`とする。

repair trigger:

- expanded tree欠損
- digest mismatch
- resolved lockとtree不一致
- revoked / expired signature
- incomplete transaction
- source importとdependency closure不一致
- Runtime state migration未完了

repair action:

- exact digestを再download / re-expand
- lockをverified treeへ再生成
- 旧revisionへ明示restore
- artifactをdisable
- previous exact versionへpin
- state migrationを再開
- manual review

repair planもpreview、plan digest、confirmation、Runtime commitを通す。修復完了で同じtransactionを`committed`へ収束させ、restore revisionを発行した場合は`superseded`、証明不能なら`needs_review`へ進める。UIがfilesystemを推測して修復しない。

### 7.10 Registry failure codes

`registry.unavailable`、`registry.policy_denied`、`registry.signature_missing`、`registry.signature_invalid`、`registry.signature_revoked`、`registry.dependency_conflict`、`registry.dependency_cycle`、`registry.artifact_corrupt`、`registry.unsupported_format`、`registry.incompatible_capability`、`registry.migration_required`、`registry.plan_stale`、`registry.repair_required`、`registry.rollback_failed`をstable codeとする。

## 8. Render / Export Boundary

### 8.1 Canonical pipeline

```text
Master Graph + ViewRecipe + fixed StateInput
  -> Go View Materializer
  -> ViewData

ViewData + RenderRecipe
  -> TS Render
  -> RenderData

ViewData + ExportRecipe
  -> Go Export Planner
  -> ExportPlan

ExportPlan + ViewData + optional RenderData + resolved assets/fonts
  -> versioned serializer
  -> ExportArtifact + Source Manifest
```

正本graphからrenderer / serializerへ直接飛ばさない。

### 8.2 RenderRecipe

RenderRecipeはpresentation inputでありLDL Viewの意味を変更しない。projectに永続化するView hintと、session固有viewportを区別する。

```text
RendererProfileRef
  profile_id
  profile_version
  specification_digest

RenderRecipe
  renderer_profile: RendererProfileRef
  shape
  layout_algorithm
  layout_seed
  density
  orientation?
  viewport?
  theme
  locale
  timezone
  font_policy
  asset_policy
  rasterizer_profile?
  interaction_policy?
```

arbitrary option mapをpublic APIにしない。profileごとにclosed option schemaを持つ。unknown optionは無視せずcompatibility diagnosticを返す。

### 8.3 RenderData

RenderDataは`@layerdraw/render`が所有するpublic TS typeであるが、semantic authorityではない。Go generated semantic schemaへ含めない。

共通field:

```text
RenderDataBase
  render_data_schema_version
  renderer_profile: RendererProfileRef
  view_data_hash
  render_input_hash
  shape
  layout_seed
  locale
  timezone
  bounds
  source_binding[]
  resolved_asset_digests[]
  resolved_font_digests[]
  diagnostics[]
```

shape固有typeはDiagram、Table、Matrix、Tree、Flow、Context、Diffのclosed unionとする。すべてのvisual occurrenceはViewData occurrence keyとsource refsへ逆引きできる。

RenderDataをproject正本、revision、QueryResultの代用として永続化しない。preview cacheへ保存する場合はinput hash、`render_data_schema_version`、renderer profile ID / version / specification digestを必須とする。schema majorまたはrenderer profileの非互換変更を跨いでcacheを再利用しない。

### 8.4 Semantic and visual determinism

determinismを二段階に分ける。

| Level | Guarantee |
| --- | --- |
| semantic determinism | 同じclosed inputからViewData / ExportPlanのcanonical bytesとhashが一致する |
| visual determinism | 同じViewData、RenderRecipe、renderer profile、font bytes、asset bytes、rasterizer profileからgeometryとartifact bytesが一致する |

interactive browser renderingはOS font fallbackやGPU差でbyte-level一致を保証しない。ただしsource binding、occurrence identity、semantic orderingを変えてはならない。

publish用exportはsystem font fallbackを禁止し、font bytes / asset bytes / rasterizer profileを固定する。font不足時に似たfontへ黙って置換しない。

### 8.5 ExportPlan and serializer input

ExportPlanはGo Engineが所有し、次を完全に指定する。

- semantic section、sheet、page、slide
- source refsとordering
- fidelity
- required assets / fonts
- layout requirement
- pagination requirement
- serializer profileとclosed options
- Source Manifest requirement

serializerはExportPlanに無いsheet、page、source mapping、business narrativeを推測しない。

```text
SerializerInput
  export_plan
  view_data
  render_data?
  assets[]
  fonts[]
  serializer_profile
  rasterizer_profile?
```

`render_data`はExportPlanが`requires_renderer=true`の場合だけ必須とする。ExportPlanとRenderDataの`view_data_hash`、profile、layout requirementが一致しなければ拒否する。

### 8.6 Export input hash

`input_hash`は次のcanonical digestから作る。

- ViewData canonical digest
- ExportRecipe normalized digest
- ExportPlan canonical digest
- exporter / serializer profile IDとversion
- RenderData digest（required時）
- asset digest集合
- font digest集合
- state snapshot hashまたはnormative no-state marker
- locale、timezone、pagination / rasterizer profile

clock、temporary path、machine hostname、Actor display nameをinputへ含めない。

### 8.7 Source Manifest

```text
ExportSourceManifest
  manifest_version
  artifact_digest
  artifact_media_type
  input_hash
  generated_at
  generator_release
  document_id?
  committed_revision?
  project_address
  view_address
  view_data_hash
  export_recipe_hash
  export_plan_hash
  render_data_hash?
  render_input_hash?
  state_input
  source_refs
  source_module_digests
  resolved_dependency_digest
  asset_digests
  font_digests
  exporter_profile
  serializer_profile
  rasterizer_profile?
  locale
  timezone
  pagination_profile?
  fidelity
  redaction
```

`generated_at`はartifact byte determinismの対象外metadataとしてsidecar manifestへ置くか、reproducible build modeでは固定clockを注入する。artifact内部に現在時刻を埋めて毎回digestを変えない。

### 8.8 Document I/O, plain export, document generation

三つを混同しない。

| Category | Input | Output |
| --- | --- | --- |
| Document I/O | project source / container model | `.ldl` source tree、`.layerdraw` |
| Plain View Export | 1つのViewData + ExportRecipe | SVG、PNG、PDF、HTML、CSV、TSV、XLSX、Markdown、PPTX、DOCX、Mermaid、BPMN、draw.io等 |
| Document Generation | 1つ以上のViewData + DocumentTemplate + narrative | business PDF、PPTX、DOCX |

plain PDF / PPTX / DOCXは1つのViewのvisual / traceable表現である。章立て、説明文、複数View、承認文脈を持つ成果物はDocument Generationであり、ExportRecipeへ押し込まない。

### 8.9 Export failure codes

`export.unsupported_shape_format`、`export.fidelity_unavailable`、`export.profile_missing`、`export.profile_incompatible`、`export.render_required`、`export.render_input_mismatch`、`export.asset_missing`、`export.font_missing`、`export.source_manifest_invalid`、`export.serializer_failed`をstable codeとする。

## 9. SDK Public Boundary

### 9.1 General rules

- SDKはGo Engine / Runtime / Registry semanticsを再実装しない。
- public surfaceは`package.json#exports`だけである。
- `src/`、generated private path、binary layoutへのdeep importを禁止する。
- clientは作成時にhandshakeし、capabilityをcacheする。
- global singleton client、global credential、暗黙のdefault hostを持たない。
- host dependencyはconstructor / factory引数へinjectionする。

### 9.2 Viewer variant

minimum public surface:

```ts
export interface Viewer {
  setViewData(input: ViewDataEnvelope): Promise<void>;
  applyViewDataUpdate(update: ViewDataUpdate): Promise<void>;
  setSelection(selection: ViewerSelection): void;
  export(request: ViewerExportRequest): Promise<ExportArtifactRef>;
  dispose(): Promise<void>;
}

export function createViewer(options: ViewerOptions): Viewer;
```

```text
ViewerExportRequest
  export_plan
  serializer_profile
  render_recipe?
  persistence: return_blob | host_artifact_store

ViewerExportResult
  artifact_blob? | artifact_ref?
  source_manifest
```

ViewerはExportPlanを生成しない。`export_plan`を必須とし、現在のViewDataとview address、revision、ViewData hash、state inputが一致しない場合は`export.render_input_mismatch`で拒否する。`host_artifact_store`はRuntime-backed ArtifactStoreが注入されている場合だけ有効である。

Viewer host injection:

```text
ViewerOptions
  renderer_profile
  asset_resolver
  font_resolver
  export_serializers?
  capability_manifest
  event_sink?
```

Viewerは`.ldl`、`.layerdraw`、DocumentStore、Engine clientを受け取らない。ViewDataとExportPlanを同時に渡す場合、view address、revision、input hashを検証する。不一致を自動修正しない。

### 9.3 Browser Editor variant

```ts
export interface BrowserEditor {
  open(input: BrowserDocumentInput): Promise<BrowserDocumentSession>;
  preview(edit: EditorEdit): Promise<WorkbenchPreviewResult>;
  apply(edit: EditorEdit): Promise<EditorApplyResult>;
  materializeView(input: MaterializeViewInput): Promise<ViewData>;
  close(): Promise<void>;
}

export function createBrowserEditor(options: BrowserEditorOptions): BrowserEditor;
```

```text
BrowserEditorOptions
  engine_client
  runtime_client?
  authoring_access_client?
  document_provider?
  asset_resolver
  registry_client?
  capability_manifest
  approval_handler?
```

- local Engine WASMだけの場合、applyはephemeral handleまたはhost callbackへ返し、Committed Revisionを偽装しない。
- Runtime clientがある場合、writeはRuntime commit protocolを使う。
- Authoring Policyが設定されたProjectではRuntime clientまたはtrusted AuthoringAccessClientを必須とし、Engine WASMのimpact表示だけでauthoritative enforcementを主張しない。
- remote hostへ送るsource、state、assetはAccessとuser consentの対象とする。

### 9.4 Server variant

public factoryを用途別に分ける。

```ts
createRemoteLayerDrawClient(options)
createLocalHostClient(options)
createPortableEngineClient(options)
```

- remote clientは`layerdraw-server`へ接続する。
- local host clientは`layerdraw-host` sidecarを起動し、storage / MCP / Runtime capabilityを使う。
- portable engine clientは`layerdraw-engine`だけを起動し、compile / preview / Structural Query / 明示subgraph Analysis / View / ExportPlanを利用する。Project Searchは完全なlocal indexと、semantic / hybridでは互換query embeddingをcallerが供給した場合だけ利用する。

一つの`mode`文字列で挙動が曖昧に変わるfactoryを公開しない。`@layerdraw/native`はplatform binary解決とdigest確認だけを行い、domain APIを持たない。

### 9.5 Host injection interfaces

SDKへ注入できるhost capabilityは次のtyped interfaceに限定する。

```text
DocumentProvider
  open, read, write_with_precondition, close

AssetResolver
  resolve(logical_ref), put(bytes), describe_capability

HistoryProvider
  list_revisions, read_revision, restore_request

CredentialConnectionProvider
  connect, refresh, disconnect

EmbeddingConnectionProvider
  describe_profile, embed_documents, embed_query

AuthoringAccessClient
  get_effective_grant, evaluate_preview

ApprovalHandler
  request_approval(preview), report_result

EventSink
  emit(typed_event)
```

TS host injectionはGo Runtime portそのものではない。Browser Editorがremote Runtimeを使わずhost callbackへ保存またはembedding生成する時のclient-side contractである。embedding textはGo Engineが生成したAccess適用済みSearchDocumentだけを渡し、TS callbackがcorpusを再構成しない。AuthoringAccessClientはtrusted Go Access / Runtime endpointへのtransportであり、TS製policy evaluatorではない。server-backed durability、multi-actor commit、Authoring Policy強制、state reconcileが必要ならRuntime Protocolを使う。

### 9.6 Capability discovery behavior

- remote endpoint、sidecar、WASM Engineへ接続するfactoryは作成時にprotocol handshakeする。
- Viewer-only factoryはendpointを要求せず、注入されたlocal / effective CapabilityManifestとrenderer / serializer profile digestを検証する。
- required capability不足なら作成をfail-fastする。
- optional capability不足ならAPIを削除せず、typed `CapabilityUnavailable`を返しUI actionをdisabledにする。
- capability changed eventを受けたらmanifestを再取得する。
- package存在やmethod existenceからcapabilityを推測しない。
- AuthoringGrantSummaryはUI最適化に使えるが、commit authorizationの代用にしない。policy / membership version変更時はmanifestとgrantを再取得する。

### 9.7 MCP public scope

MCP Hostは次のgroupを公開する。

| Group | Engine-only | Runtime-required |
| --- | --- | --- |
| discovery / scoped read / Reference | yes | project listing / openだけRuntime |
| lexical / semantic / hybrid Search | closed source + complete local index、semantic / hybridでは互換query embeddingがあればyes | revision / Access / Embedding Provider / durable indexはRuntime |
| compile / validate / format preview | yes | commitはRuntime |
| AuthoringImpact / proposal | impact分類はyes | Actor decision / apply / approvalはRuntime + Access |
| Query / ViewData | closed inputならyes | state/backend固定はRuntime |
| Graph Analysis | closed explicit subgraphならyes | revision / Access固定はRuntime |
| ExportPlan | yes | host asset / artifact保存はRuntime |
| Plain artifact serialization | serializerと全assetがあればyes | persistent artifact / package exportはRuntime |
| Registry browse | Registry client必要 | install / update / repairはRuntime handoff必要 |
| history / realtime / share | no | yes |

規範tool名:

```text
layerdraw.get_capabilities
layerdraw.list_modules
layerdraw.find_symbols
layerdraw.search
layerdraw.read_declarations
layerdraw.read_rows
layerdraw.get_neighbors
layerdraw.inspect_subgraph
layerdraw.find_usages
layerdraw.list_references
layerdraw.read_references
layerdraw.preview_operations
layerdraw.preview_fragment
layerdraw.preview_source_patch
layerdraw.apply_operations
layerdraw.apply_source_patch
layerdraw.stage_asset
layerdraw.format_scope
layerdraw.organize_workspace
layerdraw.run_query
layerdraw.analyze_graph
layerdraw.materialize_view
layerdraw.plan_export
layerdraw.serialize_export
layerdraw.import_document
layerdraw.export_document
layerdraw.list_revisions
layerdraw.restore_revision
layerdraw.registry_search
layerdraw.registry_plan_install
layerdraw.registry_apply_install
```

`import_document` / `export_document`はDocument I/O、`serialize_export`はView plain exportである。名前とrequest typeを共有しない。host capabilityが無いtoolはadvertiseしないか、MCP clientが静的tool setを要求する環境では`capability_unavailable`を返す。

`find_symbols`は正確なsymbol lookup、`search`はFTS / Vector / Hybridによる順位付き候補発見であり、同じoperation名やresponse型を共有しない。`search`、`inspect_subgraph`、`analyze_graph`は[search-query-and-analysis.md](search-query-and-analysis.md) 9章のbounded AI workflowを守る。raw Cypher、Ladybug procedure、embedding vectorを標準MCP toolにしない。

`get_capabilities`はeffective AuthoringGrantSummaryを返し、preview toolsはAuthoringImpact、Required Capability、decisionを返す。toolを非表示にしても`apply_operations`、source patch、import、Registry、restoreからの迂回をRuntimeが拒否する。`agent:propose`はproposal保存を許可できるが、Required Capabilityまたはauthorized approverなしにapplyしない。

`layerdraw.serialize_export`のnon-persistent resultは`artifact_blob: BlobRef`と`source_manifest: ExportSourceManifest`を必須とし、Runtimeが無い場合はrequest / session lifetimeのblobとして返す。ArtifactStoreへ保存するoperationはRuntime-backed hostだけが公開する。

MCP Appsはconnected hostのtoolsを呼び、ViewData / ExportPlanをrenderする。独自Runtime、Compiler、Registry semanticsを持たない。

### 9.8 Public TS package exports

| Package | Required public entrypoints |
| --- | --- |
| `@layerdraw/protocol` | `/semantic`、`/common`、`/engine`、`/runtime`、`/server-application`、`/realtime`、`/query-adapter`、`/registry`、`/container`、`/release`。`/query-adapter`はadapter SPIでありmanual Query APIではない |
| `@layerdraw/engine-client` | root、`/wasm`、`/stdio`、`/http`、`/wails` |
| `@layerdraw/query-adapter-ladybug-wasm` | root。generated execution / index planだけを受けるadapter SPI |
| `@layerdraw/embedding-provider-wasm` | root、profile-specific provider entrypoint。SearchDocument生成APIは公開しない |
| `@layerdraw/server-client` | root、`/http`、`/realtime` |
| `@layerdraw/registry-client` | root、`/http`、`/host`、`/mcp` |
| `@layerdraw/render` | root、shape-specific renderer entrypoints |
| `@layerdraw/export` | root、format profile entrypoints |
| `@layerdraw/viewer` | root |
| `@layerdraw/client-sdk` | root、`/viewer`、`/editor` |
| `@layerdraw/server-sdk` | root、`/remote`、`/local-host`、`/portable-engine` |

rootから全generated typeや全format serializerを一括exportしない。optional formatをimportしただけで全serializerをbundleさせない。

## 10. Version / Release Boundary

release manifestと互換性の意味論はこの章を規範とする。repository、Changesets、CI、artifact build / publish、配布先、SaaS promotion、署名 / provenance、license enforcementは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)、licenseの法的適用範囲は[legal/README.md](legal/README.md)を規範とする。

### 10.1 Version axes

| Axis | Purpose | Compatibility authority |
| --- | --- | --- |
| LayerDraw release SemVer | official binary / WASM / TS package release set | release manifest |
| LDL generation | source syntax / semantics | LDL language specs |
| Engine Protocol version | Engine operations / wire | engine protocol schema |
| Runtime Protocol version | host lifecycle / commit / state | runtime protocol schema |
| Server Application Protocol version | Instance / Organization / Workspace / Project directory / Access management wire | server application protocol schema |
| Realtime Protocol version | room / event wire | realtime protocol schema |
| Query Plan Protocol version | Engine / adapter wire | query adapter schema |
| Search / Analysis Plan Protocol version | Engine / adapter wire | query adapter schema |
| Registry Protocol version | registry API / transaction | registry protocol schema |
| Access Protocol version | AuthoringCapability / HostOperationImpact / AuthoringPolicy / Grant / Decision | access protocol schema |
| semantic schema version | AuthoringImpact / SearchResult / QueryResult / AnalysisResult / ViewData / ExportPlan | semantic schema |
| RenderData schema version | public RenderData / preview cache shape | `@layerdraw/render` schema |
| `.layerdraw` / `.ldpack` format version | container read / write | container schema |
| renderer / exporter / Search / Embedding profile version | visual、byte generation、ranking、vector compatibility | profile registry |

package SemVerが同じでもprotocol互換を推測しない。handshake結果を使う。

### 10.2 Protocol version rule

protocol versionは`major.minor`とする。

- major変更: operation削除、field意味変更、required field追加、enum narrowing、canonicalization変更。
- minor変更: optional field追加、operation追加、capability追加、enum拡張可能と宣言したfieldへの値追加。
- patchはprotocol schemaへ使わず、実装bug fixはLayerDraw release patchで表す。

clientとhostはmajor一致かつminor range overlapを要求する。unknown optional fieldを保持できないclientは、そのpayloadをread-modify-writeへ使ってはならない。

### 10.3 Release manifest

```text
LayerDrawReleaseManifest
  manifest_version
  release_version
  source_revision
  built_at
  artifacts[]
    artifact_id
    platform?
    media_type
    size
    digest
  protocols[]
    name
    supported_range
    versions[]
      version
      schema_digest
  ldl_generations[]
  container_versions[]
  renderer_profiles[]
  exporter_profiles[]
  search_profiles[]
  embedding_profiles[]
  required_ladybug_primitives[]
  generated_packages[]
    package_name
    package_version
    schema_digests[]
  signature?
```

renderer / exporter profile entryは次を必須とする。

```text
ReleaseProfileEntry
  profile_id
  profile_version
  specification_digest
  supported_shapes[]?
  supported_formats[]?
  status: enabled | disabled
  disabled_reason?
  replacement_profile?
```

release manifestの`renderer_profiles`、`exporter_profiles`、`search_profiles`、`embedding_profiles`はprofile種別に応じたclosed entryを使う。profile ID / versionだけ一致しspecification digestが異なるartifact setを同一releaseとして配布しない。公式Query-capable artifactは`required_ladybug_primitives`を全てadvertiseし、packaged conformanceで実行確認する。

`@layerdraw/engine-wasm`、`@layerdraw/native-*`、Go binaries、generated TS protocol packageはrelease manifestのdigestへ固定する。arbitrary postinstall downloadを行わない。

### 10.4 Client / host compatibility

| Client | Host | Behavior |
| --- | --- | --- |
| older | newer | protocol rangeがoverlapし、required capabilityがあれば接続。未知capabilityは使わない |
| newer | older | overlap範囲へnegotiateし、未提供operationをdisabledにする |
| major non-overlap | any | handshakeで拒否し、upgrade requirementを返す |
| schema digest mismatch within claimed exact version | any | build / deployment corruptionとして拒否する |

SDKはremote hostのrelease versionだけで接続可否を決めない。

### 10.5 Language and container upgrade

- older language / container minorはreaderが対応range内ならreadできる。
- writeはcurrent canonical language / container versionだけを生成する。
- migrationがsource text、StableAddress、resolved dependency、state mappingを変える場合、previewable migration planとexplicit applyを要求する。
- sourceを開いただけでLDLを黙って書き換えない。
- newer unsupported majorはreadonly fallbackで誤魔化さず拒否する。ただし安全なcontainer entry listingは`inspect_container`で提供できる。
- `.layerdraw` import-as-newは新しいhost Document IDを発行する。

### 10.6 Registry artifact upgrade

- resolved projectはexact artifact digestを使う。
- Registry上の新version公開で既存projectを暗黙更新しない。
- updateはRegistry plan、Engine validation、Runtime commitを通す。
- same canonical Pack IDのcompatible updateでもdeclaration semantic hashの変更を診断する。
- publisher replacementは署名付きreplacement metadataとexplicit migrationを要求する。

### 10.7 Renderer and exporter profile upgrade

- ExportRecipeはprofile ID、profile version、specification digestを固定する。
- exact profileが利用可能なら再現実行する。
- profileが無い場合、近いversionへ黙って置換しない。
- compatible replacementはpreviewでartifact / layout diffを提示し、recipe更新を明示applyする。
- security fixでprofileを実行禁止にする場合も、診断とreplacement候補を返し、過去artifactのSource Manifestを改変しない。

### 10.8 TypeScript SemVer

breaking change:

- exported symbol / entrypoint削除
- required parameter追加
- result意味変更
- exceptionとtyped outcomeの変更
- host injection interfaceのrequired method追加
- generated enumの閉集合変更

minor-compatible change:

- optional API追加
- optional parameter追加
- capabilityでguardされたoperation追加
- open enumとして宣言したgenerated fieldの値追加

deprecated APIは同一major中はfacadeとして維持し、意味論をforkしない。削除は次のmajor releaseでだけ行う。

### 10.9 Upgrade diagnostics

upgrade関連diagnosticは少なくとも次を持つ。

```text
UpgradeDiagnosticData
  current_version
  required_version_or_range
  affected_artifacts[]
  migration_available
  migration_plan_ref?
  readonly_possible
  replacement_profile?
```

## 11. Cross-Boundary Flows

### 11.1 Browser local edit

```text
Browser Editor
  -> Engine WASM handshake
  -> engine.open_document
  -> engine.preview_operations
  -> engine.apply_to_handle
  -> host DocumentProvider.write_with_precondition
```

このflowはCommitted Revisionを持つRuntimeではない。host providerがrevision semanticsを提供しない場合、single-user file save capabilityとして表示する。

### 11.2 Server-backed edit and realtime

```text
Client / MCP / Agent
  -> Runtime handshake / open
  -> Realtime join
  -> working changes
  -> runtime.commit_operations
     -> Engine Workbench
     -> DocumentStore publish
     -> StateBackend / audit
     -> outbox
  -> commit_published broadcast
```

### 11.3 State-aware query and render

```text
Runtime
  -> StateBackend read
  -> Access projection
  -> immutable StateQuerySnapshot
  -> Engine prepare_query
  -> Ladybug adapter execute
  -> Engine complete_query
  -> Engine materialize_view
  -> TS Render
```

credential、backend locator、raw unauthorized stateはEngine / rendererへ渡さない。

### 11.4 AI search to structural query

```text
MCP / Search Workbench
  -> Runtime revision + Access projection固定
  -> Search Index identity検証
  -> Engine prepare_search
  -> optional Embedding Provider
  -> Ladybug FTS / Vector execute
  -> Engine complete_search / Hybrid RRF
  -> SearchHit StableAddressを選択
  -> inspect_subgraph
  -> explicit root QueryをWorkbench preview
  -> optional run_query / analyze_graph / materialize_view
  -> Runtime commit_operations
```

SearchHitのrank、score、cursorをQuery recipeへ保存せず、確認済みStableAddressまたはtyped predicateだけをLDLへcommitする。Graph AnalysisはQueryResultまたは明示subgraphを入力にし、結果を正本へ暗黙書込みしない。

### 11.5 Registry install into open project

```text
Library / MCP
  -> Registry search / plan
  -> download / signature / dependency
  -> Engine package validation
  -> user / policy confirmation
  -> Runtime commit_registry_plan
  -> Registry finalize
  -> capability / project refresh
```

### 11.6 Plain export

```text
Committed Revision + fixed StateInput
  -> QueryResult
  -> ViewData
  -> ExportPlan
  -> optional RenderData
  -> serializer
  -> ArtifactStore + Source Manifest
```

### 11.7 Fixed semantic model edit

```text
Tenant user / AI agent
  -> runtime.get_authoring_grant
  -> read existing EntityType / RelationType / Layer
  -> graph semantic operations
  -> Engine Workbench preview
     -> AuthoringImpact(required: graph:write)
  -> Access constraint evaluation
  -> Runtime commit re-evaluation
  -> Committed Revision
```

Schema operation、source patch、Pack update、restore等で`schema:write`が必要になった場合は同じflowでdenyまたはauthorized proposalへ進める。generic transport、MCP、SDK、realtimeから固定Schemaを迂回しない。

## 12. Security and Conformance

### 12.1 Security invariants

- Engineへcredential、Actor token、backend locatorを渡さない。
- Registry artifactを検証前にproject treeへ展開しない。
- external asset URLを暗黙fetchしない。
- cursor、handle、resume token、transaction IDを別Actorへ流用できない。
- MCP / SDKはAccessとCapabilityManifestを迂回しない。
- server-backed handle、cursor、cache key、search、history、asset、artifact、realtime room、audit queryはOrganization scopeを迂回しない。
- cross-organization lookupはresourceの存在、ID、digest、件数、timing差を不必要に漏らさない。
- exporterはSource Manifestからredaction markerを削除しない。
- Realtime presenceを監査ログやstateの正本にしない。
- AuthoringPolicy / Grantを`.ldl`、portable container、client自己申告inputから解決しない。
- UI / MCP tool visibilityをAuthoring Accessの最終enforcementにしない。
- restricted Projectのuntrusted provider headを自動採用しない。

### 12.2 Conformance suites

| Suite | Required comparison |
| --- | --- |
| Engine transport | in-process / WASM / stdio / HTTP / Wailsで同じoperation result |
| Protocol codegen | Go / TS canonical fixtureのround tripとunknown field preservation |
| Handle / cursor | generation、revision、Actor、expiry違反の拒否 |
| Runtime transaction | conditional write、timeout、duplicate retry、partial state failure、recovery |
| Authoring Access | operation / fragment / patch / import / Registry / restoreのimpact一致、policy change、mixed batch拒否、constraint、approval |
| Realtime | ordering、reconnect gap、conflict、checkpoint、outbox replay |
| Graph execution adapter | native / WASMのTypedRawRows schemaとcanonical SearchResult / QueryResult / AnalysisResult一致 |
| Search / Analysis adapter | FTS、Vector、Hybrid順位、PageRank、K-Core、Louvain、SCC、WCC、Access pre-filter、stale index拒否のnative / Node WASM / browser WASM一致 |
| Registry | dependency resolution、signature、rollback、repairのgolden transaction |
| Render | source binding、geometry invariant、profile option validation |
| Export | ExportPlanからartifact / Source Manifest、fixed profileでreproducible digest |
| SDK | capability不足、host injection、remote/local modeの同じtyped outcome |
| Upgrade | client/host range、old container migration、profile replacement、major拒否 |

### 12.3 Forbidden shortcuts

- TSでLDL parser、Query normalizer、ViewData materializer、ExportPlan plannerを実装すること
- HTTP handler、Wails method、MCP tool、React componentへdomain semanticsを置くこと
- `save`、`autosave`、`realtime commit`で異なるcommit semanticsを作ること
- Registry UIがdependency / signature / repair decisionを独自実装すること
- browser Ladybug adapterがraw rowsをQueryResultへ変換すること
- TS / MCP adapterがSearchDocument、embedding text、Hybrid score、AnalysisResultを独自生成すること
- TS、MCP、Runtime handlerがsubject kindやoperation名からAuthoring Capabilityを独自分類すること
- `dsl:write`、`agent:apply`、`package:manage`をSchema / Graph変更の包括grantとして扱うこと
- renderer / exporterがViewDataに無いsource relationやbusiness meaningを推測すること
- release SemVer一致だけでprotocol互換を判定すること

### 12.4 完了条件

- 各operationが一つのownerと一つのschema groupを持つ。
- stateful operationに状態遷移、idempotency、cancel、recoveryがある。
- public handleとcursorのscope / invalidationが定義されている。
- Runtime portがprovider固有APIを漏らさない。
- Query native / WASMが同じprepare / complete contractを通る。
- FTS / Vector / Algoが公式Query-capable bundleで必須primitiveとして適合試験される。
- SearchHitがStableAddress、DocumentSnapshotRef、index identity、score reasonへ束縛される。
- Definition write経路がEngine AuthoringImpact、LDL外write経路がtyped HostOperationImpactを生成し、Accessが合成decisionを返してcommit時にpolicy / membership versionを再検証する。
- fixed semantic modelでSchema write不可ActorがGraph instanceを編集でき、source patch / import / Registry / restoreからSchema制限を迂回できない。
- Registry planとRuntime applyの責務が循環していない。
- ViewData、RenderData、ExportPlan、Artifactの変換責務が分離されている。
- SDK variantがcapabilityを偽装しない。
- version axisとrelease SemVerを混同していない。
- 全提供形態が同じgenerated protocolとconformance fixtureを利用できる。
