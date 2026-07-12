# LayerDraw Server 方針

## 1. 基本方針

LayerDraw Server はGo Engineそのものではなく、LayerDraw documentを複数のclientと保存先から扱うためのGo製host基盤である。

Go Engineは`.ldl`のparse、検証、操作、Query、ViewData、ExportPlan、package semanticsを担当する。LayerDraw ServerはGo RuntimeとAccessを組み込み、Instance / Organization / Workspace管理、認証、Resource / Authoring権限、永続化、共同編集、履歴、MCP操作面を担当するmulti-organization application hostである。visual renderingはTS Presentationまたは専用serializerへ委譲する。

Runtime / Host Port、Realtime、Registry、SDK、Version / Releaseの具体的契約は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

要件上、サーバーは特定のストレージサービスへ直結しない。バックエンドには必ず何らかの永続ストレージが必要だが、保存先は adapter pattern で差し替え可能にする。

```text
LayerDraw clients
  -> LayerDraw Server API
    -> AuthProvider
    -> OrganizationRepository
    -> WorkspaceRepository
    -> IdentityRepository
    -> AccessRepository
    -> ProjectRepository
    -> DocumentStore
    -> StateBackend
    -> ArtifactStore
    -> RealtimeProvider
    -> HistoryStore
    -> EntitlementProvider
    -> UsageSink
      -> local filesystem / S3 / Azure Blob / Google Cloud Storage
      -> SharePoint / Google Drive / OneDrive
      -> SQL DB / object storage / external document storage
```

LayerDraw Serverの中核はstandalone serverに閉じ込めない。Go製LayerDraw Runtime libraryとして切り出し、外部host application、self-host server、managed cloud、Desktop、local MCP bridgeが同じdocument runtimeを内包できるようにする。

```text
LayerDraw Server Application
  -> Organization / Workspace / Access / Share / Audit
  -> Go LayerDraw Runtime
    -> Go LayerDraw Engine
    -> Go Access component
    -> DocumentStore
    -> StateBackend
    -> ArtifactStore
    -> HistoryStore
    -> EventPublisher
    -> LeaseManager
```

このruntime library自体は常駐DBを必須にしない。短命computeはin-memory cache、ephemeral query index、temporary ViewData / artifact cacheを持ってよいが、durable source of truthはstorage adapter側に置く。ただしmulti-organization Server Applicationはmembership、Workspace、ACL、audit、search metadataのdurabilityにSQLite / PostgreSQL等のmetadata repositoryを必須とする。

## 2. サーバーの責務

LayerDraw Server が持つ責務:

- Actor 解決と認証プロバイダ連携
- Server Instance administration
- Organization、membership、Team、Service Account管理
- Workspace / Project metadata と権限管理
- project-local `.ldl` source tree の Committed Revision 保存
- state backend との接続
- `.layerdraw` package artifact の import / export / 生成
- revision history と time machine
- realtime collaboration と presence
- MCP server / agent API
- storage adapter との接続
- EntitlementSnapshotの適用とUsageEventの発行
- embedded Web editor、VSCode extension、desktop app、外部 Web app から使える API

LayerDraw Server が持たない責務:

- エディタ UI の内部状態
- canvas 操作そのもの
- 特定ストレージサービス専用の domain model
- 独自password / credentialを正本とするIdentity Provider。Actor認証は外部IdP等へ委譲できる
- managed service固有の商用・流通機能
- LDL parser、formatter、hash、Query、ViewDataをServer handler内で再実装すること
- React / Wails / VSCode固有のpresentation semantics

### 2.1 Multi-organization model

```text
Server Instance
  Instance Admin
  Organization
    Members / Teams / Service Accounts
    Organization Policy
    Storage / Registry Bindings
    Workspace
      Project
        Document / Revision / Realtime Room
```

- Organizationはtenant、security、membership、policy、storage / registry binding、audit scopeの境界である。
- Workspaceは1つのOrganizationに所属し、Projectの分類、権限継承、共同管理を行う。
- Projectは1つのWorkspaceに所属する。共同編集とrevisionの同期境界はOrganizationではなくDocument / Realtime Roomである。
- 単一組織のdeploymentでも同じmodelを使い、bootstrap時にdefault Organization / Workspaceを作る。
- Userは複数Organizationへ所属できる。Team / Service AccountはOrganizationにscopeする。
- cache、search、history、asset、artifact、realtime room、audit queryはOrganization IDを必須scopeとする。
- dedicated Serverは物理分離要件のために選択できるが、論理tenantごとのServer複製を通常要件にしない。
- Organization / Workspace所属はHost metadataであり、LDL Project StableAddressやportable `.layerdraw` definitionへ含めない。

## 3. Document と Artifact の分離

編集正本は `.ldl` とする。

`.layerdraw` は zip package 形式の共有・配布・成果物 bundle として扱う。サーバー内部では、project-local `.ldl` source tree の Committed Revision と `.layerdraw` artifact を分離する。単一 `document.ldl` は合法だが、標準構成は `schema/`、`layers/`、`views/`、`references/` に分割されるため、revision は常に source tree 単位で扱う。

```text
Project
  projectId              # host-owned immutable Document/Project ID
  organizationId         # host-owned tenant scope
  workspaceId            # host-owned grouping scope
  metadata
  accessPolicyRef        # AccessRepositoryへのhost-owned reference
  authoringPolicyRefs    # Instance / Organization / Workspace / Project policy chain

DocumentRevision
  projectId
  revision
  parentRevision
  sourceTreeManifest
    path -> sourceBlobDigest
  resolvedMetadataDigest
  dependencyTreeDigest
  normalizedDefinitionHash
  changedOwnSubjectSemanticHashes
  changedSubtreeSemanticHashes
  operationIds
  actor
  createdAt

PackageArtifact
  document_id
  revision
  project.layerdraw
  generated_at
  source
```

上のcamelCase名はserver実装内部のhost-language adapter modelを説明する記号であり、永続化JSON、manifest、HTTP、MCP、operation wireではない。外部境界はLDL命名規則に従うlower_snake_caseだけを使い、たとえば`projectId`は`project_id`、`parentRevision`は`parent_revision`、`operationIds`は`operation_ids`へ変換する。

Server modelの`organization_id`、`workspace_id`、`document_id`はhost-owned IDであり、LDLの`project <id>`から構成されるProject StableAddressとは別物である。revisionはDocumentとdefinitionを`definition_project_address`で結ぶ。Server Applicationはsemantic operation sessionと物理state namespaceを`organization_id + document_id`でscopeし、RuntimeへOrganization-scoped BackendBindingと`document_id + StableAddress`のsubject refを渡す。Runtime / EngineはOrganization directoryを解釈しない。

原則:

- 通常保存は Working Document に semantic operation を適用し、検証成功時に canonical source tree の Committed Revision を発行する
- `.layerdraw` は revision から生成できる artifact として扱う
- アップロードされた `.layerdraw` は import して中の `document.ldl` と project-local source tree を一つの revision に取り込む
- package 内の assets / previews / exports は ArtifactStore または DocumentStore の関連 blob として扱う
- time machine は source tree の Committed Revision を基準にする
- Entity / Relation / View の `createdAt` / `updatedAt` / actor は current state index または audit log から導出する
- source tree revision は宣言 DSL として保持し、system fields / provenance を canonical `.ldl` に混ぜない
- `.ldl` standalone export は definition-only export とする
- state を含めて共有する場合は `.ldstate.json` または `.layerdraw` の `state/` を使う
- remote state backend の binding は `.ldl` ではなく `project.ldbackend.json` または `layerdraw.backend.json` で扱う
- actor / source を落とす場合は public redacted state export として明示する

## 4. ストレージ分離

LayerDraw Server は「サーバー」と「ストレージ」を分離する。

サーバーは HTTP / WebSocket / MCP の操作面と整合性制御を担当し、実データの保存先は adapter に委譲する。これにより、オンプレミス、クラウド、既存グループウェア、外部ドライブ連携を同じ server contract で扱えるようにする。

### 4.1 Adapter 種別

`OrganizationRepository`

- Organization metadata / lifecycle
- membership / Team / Service Account
- Organization policy
- storage / registry binding references

`WorkspaceRepository`

- Organization-scoped Workspace metadata
- Workspace membership / settings
- Project directory

`IdentityRepository`

- external IdP / host Actor ID とLayerDraw Actorの対応
- Actor profile referenceとidentity provider binding
- password、OAuth token、provider credentialの正本は保持しない

`AccessRepository`

- Instance / Organization / Workspace / Project role and ACL
- share grant / link metadata
- policy version / access fingerprint

`AuthoringPolicyRepository`

- closed Authoring Capability rule
- Graph Type / RelationType / Layer / Column / action constraint
- Instance / Organization / Workspace / Project binding
- policy version / digest
- approval rule reference

AuthoringPolicyはHost metadataであり、`.ldl`、`.layerdraw`、`.ldpack`、StateBackendへ保存しない。EngineはDefinition差分をAuthoringImpactへ分類し、versioned owner protocolはasset、Package transaction、Project設定等をHostOperationImpactへ固定する。Accessは両impactを合成判定し、document / asset / package適用はRuntime、Project Host metadataはServer Applicationがpublication直前に再評価する。source patch、import、Registry、restore、reconcile、realtimeからの迂回を許さない。AuthoringPolicyのcreate / update / bindは変更前snapshotの`access:manage`で認可し、変更後Policyによる自己昇格を禁止する。

`ProjectRepository`

- Project metadata
- organization_id / workspace_id
- project list
- updatedAt
- search metadata

`DocumentStore`

- canonical project-local `.ldl` source blobs
- revision ごとの source tree manifest
- current revision
- revision manifests
- operation log
- current head metadata
- conflict detection metadata

`StateBackend`

- current state snapshot
- provenance index
- state head metadata
- lease / lock
- audit event append
- state export / redaction

`ArtifactStore`

- `.layerdraw` package
- preview images
- SVG / PNG / PDF exports
- attached assets

`HistoryStore`

- revision stream
- operation log
- actor and timestamp
- restore metadata

`RealtimeProvider` / `RealtimeRoom`

- active room state
- transient presence
- transient Working Document / collaboration state
- selected adapter state for command log、CRDT、or OT synchronization

`ExternalFileStore`

- Google Drive / OneDrive / SharePoint など、ファイルと ACL が一体になった保存先
- file picker / permission / file metadata / conflict token

`EntitlementProvider`

- Organization-scoped immutable EntitlementSnapshot
- static / external quota input
- 商用契約や請求の意味論を所有しない

`UsageSink`

- Organization / Workspace / Project scope付きUsageEvent
- idempotent delivery
- 請求額計算を所有しない

### 4.2 対応したい保存先

必須候補:

- local filesystem
- S3 compatible storage
- Azure Blob Storage
- Google Cloud Storage
- Cloudflare R2
- MinIO

外部ファイルサービス候補:

- SharePoint
- Google Drive
- OneDrive
- GitHub repository
- GitLab repository
- Nextcloud

SQL / metadata 候補:

- PostgreSQL
- SQLite
- Cloudflare D1
- managed SQL service

## 5. 保存先ごとの扱い

### 5.1 Local filesystem

ローカル開発、desktop app、single-user self-host の最小構成。

- `.ldl` と artifact をローカルディレクトリに保存する
- Project metadata は JSON または SQLite で保存できる
- 認証なし、または dev auth で運用できる

### 5.2 Object storage

S3、Azure Blob Storage、Google Cloud Storage、R2、MinIO は blob storage adapter として扱う。

- canonical source tree manifest と content-addressed `.ldl` blob
- operation log
- current head metadata
- `.layerdraw` package
- assets
- previews
- exports

definition revisionのdurabilityはSQL DBを必須とせず、object storageの`head.json`、source manifest、content-addressed source blob、operation log、asset blobを直接source of truthにできる。一方、`layerdraw-server`のOrganization / Workspace / identity mapping / Project directory / ACL / audit / search metadataは、各Repository portを実装するdurable metadata storeを必須とする。

AuthoringPolicyとProject bindingもserver-backed security metadataであるためdurable metadata storeを必須とする。definition headとPolicy bindingを別々に公開して一時的なunrestricted Projectを作らない。

代表的な object layout:

```text
/organizations/{organization_id}/documents/{document_id}/head.json
/organizations/{organization_id}/documents/{document_id}/revisions/{version}/source-manifest.json
/organizations/{organization_id}/documents/{document_id}/source/{sha256}.ldl.gz
/organizations/{organization_id}/documents/{document_id}/resolved/{sha256}.json
/organizations/{organization_id}/documents/{document_id}/dependencies/{sha256}
/organizations/{organization_id}/documents/{document_id}/ops/{version}-{operation_id}.json
/organizations/{organization_id}/documents/{document_id}/assets/{sha256}
/organizations/{organization_id}/documents/{document_id}/previews/{revision}/{view_address_hash}.svg
/organizations/{organization_id}/documents/{document_id}/exports/{revision}/{artifact_id}
```

`organization_id`と`document_id`はhost-owned identityである。object keyとindex queryの両方をOrganizationでscopeし、cross-organization deduplicationによってblobの存在やdigestを漏らしてはならない。`view_address_hash`はView StableAddressのSHA-256であり、preview metadataは元のView StableAddressを保持する。StableAddress文字列をstorage pathへ直接埋め込まない。

`source-manifest.json` は正規化された project-relative path、source blob digest、source byte digest、entry module、resolved dependency metadata digest、expanded dependency tree digest を記録する。同じ source blob と dependency blob は revision 間で共有し、変更分だけを追加保存する。過去 revision の復元、query、render、export は Registry へ接続せず、固定された resolved metadata と dependency tree だけで完結しなければならない。

`head.json` は current revision、latest source manifest、normalized definition hash、schema version、compaction marker、lease metadata を持つ。head 更新は ETag / generation token を使う conditional write とし、manifest と必要な source blob の保存完了前に公開してはならない。

### 5.3 External drive

Google Drive、OneDrive、SharePoint などは、ファイル本体と権限が保存先側にある。

LayerDraw は保存先 ACL を尊重する。外部保存先ではeffective accessを保存先ACLとLayerDraw Access decisionの積集合とし、どちらか一方のgrantで他方のdenyを昇格させない。

このモードでは、保存先の file id、etag / version token、modified time、owner、sharing state を adapter が保持する。

### 5.4 Repository storage

GitHub / GitLab などの repository storage では `.ldl` を primary file として扱う。

- branch / commit / pull request と相性がよい
- AI / MCP は `.ldl` diff を作れる
- `.layerdraw` package は release artifact または generated asset として扱える

## 6. Adapter contract

サーバー内部は、保存先固有 API を直接呼ばない。すべて port interface を通す。

概念的な contract:

```ts
interface DocumentStore {
  getCurrent(projectId: string): Promise<DocumentRevision>;
  getHead(projectId: string): Promise<DocumentHead>;
  tryUpdateHead(input: UpdateHeadInput): Promise<DocumentHead>;
  appendOperation(input: AppendOperationInput): Promise<OperationRecord>;
  putRevision(input: PutRevisionInput): Promise<DocumentRevision>;
  getRevision(projectId: string, revision: number): Promise<DocumentRevision>;
  listRevisions(projectId: string): Promise<DocumentRevisionSummary[]>;
}

interface StateBackend {
  getHead(projectId: string, stateKey: string): Promise<StateHead | null>;
  readState(input: ReadStateInput): Promise<LayerDrawState | null>;
  writeState(input: WriteStateInput): Promise<StateWriteResult>;
  acquireLease(input: AcquireLeaseInput): Promise<StateLease>;
  renewLease(input: RenewLeaseInput): Promise<StateLease>;
  releaseLease(input: ReleaseLeaseInput): Promise<void>;
  appendAuditEvent(input: AppendAuditEventInput): Promise<AuditEventRef>;
}

interface ArtifactStore {
  getPackage(projectId: string, revision: number): Promise<Blob | null>;
  putPackage(input: PutPackageInput): Promise<PackageArtifact>;
  putAsset(input: PutAssetInput): Promise<AssetRef>;
  getAsset(ref: AssetRef): Promise<Blob>;
}
```

Go 実装では同等の interface を package 内に定義し、既存の file store は local adapter として扱う。
State backend と backend binding の詳細は [state-backends.md](state-backends.md) に置く。

## 7. 整合性と競合

すべての write は base revision または保存先の version token を要求する。

競合検出:

- LayerDraw managed storage: numeric revision
- object storage: stored revision metadata + conditional write
- Google Drive / OneDrive / SharePoint: etag / drive item version
- GitHub / GitLab: blob sha / commit base

競合時はサーバーが自動上書きしない。クライアントまたは MCP は差分確認、merge、restore、retry のいずれかを選ぶ。

### 7.1 Runtime write protocol

複数 actor / agent の入力は選択した `RealtimeRoom` adapter で同期し、Committed Revision の公開は runtime が revision 順に直列化する。

1. client / agentがLDL詳細仕様11.1節の`OperationBatch`そのものを渡す。`base_revision`、各expected hash、`operations`、`idempotency_key`を外側の別envelopeへ重複させない。routeまたはpayloadのhost Document IDで全addressをscopeする
2. runtime が active lease、current document head、state head を確認する
3. semantic operation を Working Document に適用する
4. validator で syntax、symbol、type、row、endpoint、cardinality、reference integrity を確認する
5. `operation_id`付きpending operation recordとpending realtime outbox eventをdurableにconditional writeする
6. 変更 `.ldl` blob、必要な resolved / dependency blob、source tree manifest を expected revision / hash 付きで保存する
7. definition headをETag / generation / version token付きでconditional updateする。この成功をCommitted Revisionの公開点とする
8. state delta / snapshotを同じ`idempotency_key`、expected `state_version`、lease token付きでconditional writeする
9. audit eventをappendする
10. operation recordへ最終`OperationResult` statusを記録し、outbox eventをreadyへ進める
11. event publisherがready eventをrealtime subscriberへ通知する

object storage だけでは realtime notification と multi-writer coordination が弱いため、project / session 単位で single-writer lease を持つ。複数 compute が同じ project を触る場合は lease と conditional write で競合を検出し、reload / retry / reject のいずれかに落とす。

DocumentStoreとStateBackendが別保存先の場合でも、runtimeは同じ`idempotency_key`を両方へ記録する。
完全な分散 transaction は要求しない。
代わりに pending / committed operation record と recovery により、document revision と state snapshot の対応を検査できるようにする。
state writeが遅延または失敗したrevisionは`committed_state_stale`と`LDL1902`で開き、reconcile / repairを要求する。recoveryで公開状態を一意に決められない場合だけ`needs_review`と`LDL1903`を返す。

step 7より前の失敗はdefinitionを公開せず`rejected`を返す。pending outboxをdurableに保存できない場合もheadをpublishしない。step 7成功後のstep 8または9の失敗はdefinitionをrollbackせず`committed_state_stale`を返す。step 7のconditional write応答またはoutbox ready化が不明でも、head、pending record、pending outboxから公開成否を一意に復元できれば`committed`、`committed_state_stale`、`rejected`のいずれかへ収束させ、一意に判定できない場合だけ`needs_review`とする。API / MCP responseはRuntimeCommitResult内にLDL詳細仕様の`OperationResult`を変更せず保持する。

### 7.3 Query/View state snapshot

state依存Query/View requestでは、runtimeはdefinition revisionとstate headを評価開始前にそれぞれ1件へ固定する。Access packageで要求field pathを認可し、`move_closure`とown-subject hashを検査してから、LDL詳細仕様5.4節のStateQuerySnapshotを構築する。評価中にstate headを再読込したり、server clockでmissing時刻を補ってはならない。

responseのQueryResultは`state_input`と`state_reads`、ViewDataは`state_input`と各SourceRefs内のStateRefs、ExportPlanは`state_input`と各representation SourceRefs内のStateRefsを返す。cache keyはQuery/View hashと、snapshot利用時はsnapshot hashを含む。state非依存recipeはstate headが存在してもnone inputを使う。required欠落/stale、optional no-state/stale、Access拒否/redactionは規範diagnosticへ写像し、HTTP statusや空resultだけで意味を代替しない。

## 8. Time Machine

Time Machine は `.ldl` revision stream を基準にする。

必要な機能:

- revision list
- revision preview
- revision diff
- restore as new revision
- actor / operation / timestamp 表示
- package artifact の再生成

外部保存先が独自 version history を持つ場合でも、LayerDraw は最低限の revision metadata を持つ。保存先 history は補助情報として利用する。

## 9. MCP との関係

MCP endpointはLayerDraw ServerのEngine / Runtime操作面をMCPへ写すGo adapterである。

MCPは保存先固有APIを直接触らず、LayerDraw Runtime経由で`.ldl`とrevisionを操作する。局所取得、semantic operation、scoped LDL fragment、token budgetの規範は[ai-integration.md](ai-integration.md)に従う。

標準操作:

- document read
- validation
- subgraph query
- operation apply
- patch propose / apply
- revision list
- revision diff
- restore revision
- package export

## 10. 実装方針

対応するもの:

- 既存 `FileProjectStore` を local adapter として整理する
- `.ldl` source tree の Committed Revision と `.layerdraw` artifact の概念を分ける
- server API は storage 実装を直接仮定しない
- embedded runtime の interface を先に固定する
- S3 compatible adapter
- object-storage-backed head / operation log / source tree manifest
- Go Runtime libraryではSQL metadata repositoryをoptionalとする
- `layerdraw-server`ではOrganization / Workspace / membership / ACL / audit metadata repositoryを必須とし、SQLite / PostgreSQL等のadapterを構成する
- MCP server endpoint
- time machine API
- Go Engine / Runtime libraryをEcho handlerから分離する
- EchoをHTTP / WebSocket / middleware shellに限定する
- Engine ProtocolとMCP envelopeのconformance test

同じ adapter contract で扱う保存先:

- Azure Blob Storage adapter
- Google Cloud Storage adapter
- SharePoint / OneDrive adapter
- Google Drive adapter
- repository storage adapter
- hosted / self-host deployment templates
