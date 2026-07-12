# LayerDraw Blueprint

To-Be repository topology、public / private source境界、governance、CI、release、配布、SaaS CD、license enforcementは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)、licenseの法的適用範囲は[legal/README.md](legal/README.md)を規範とする。このBlueprintはそれらを重複定義しない。

## 1. Product Thesis

LayerDraw は自由作図ツールではない。

LayerDraw は、typed graph と 2D attribute table を正本として保持し、必要な切り口を View として生成するための構造管理プラットフォームである。

主な用途は 2 つある。

1. Multi-layer architecture source of truth
   - 業務、データ、アプリケーション、OS / VM、ハードウェア、ネットワークなどの多層構造を 1 つの正本グラフで管理する。
   - Excel sheet や個別図面で手動管理していた成果物を View として切り出す。
   - 正本を更新すると、関連 View と派生成果物が自動的に更新される。

2. Typed long-term memory for AI agents
   - Markdown note ではなく、Entity / Relation / Attribute / View で AI agent の長期記憶を構造化する。
- 複数端末、複数AI agent、IDE agent、CI botなどが同じtyped memory graphを共有できる。
   - Obsidian 的な個人知識ベースではなく、人間と AI agent が共同編集する typed semantic memory layer を目指す。

中心抽象は以下である。

```text
Typed graph
  EntityType
  RelationType
  Layer
  Entity
  Relation

2D attribute tables
  EntityType.columns
  Entity.attributeRows
  RelationType.columns
  Relation.attributeRows

Saved views
  QueryRecipe
  ViewRecipe.category / source / typed shape / export recipes

Agent guidance
  Reference
```

LayerDraw の価値は、図面を直接編集することではなく、図面にできる構造を管理することにある。

## 2. Core Principles

- entry moduleから選択されるproject-local `.ldl` source treeを編集正本にする。
- `.ldpack` は registry に登録する pack container とする。asset が無い pack も `.ldpack` に包む。
- `.layerdraw` は project / template / export 共有用の zip container とする。
- すべてのaddressable subjectはauthored local IDを持ち、Project/Pack origin、kind、owner chainからStableAddressを決定論的に構成する。
- committed IDのdeleteは`reserved`、renameは`moves`としてdefinitionへ残し、stateやGit履歴がなくても既知のidentity lineageを共有できる。
- Relation は線ではなく意味を持つ。
- View は静的コピーではなく、正本グラフに対する抽出・投影レシピである。
- ViewData / RenderDataはView実行結果であり、正本ではない。RelationType projection ruleはViewData変換規則として正本schemaに含む。
- `.ldl` は宣言 DSL であり、system fields / provenance / audit は state layer で扱う。
- createdAt / updatedAt / provenance は鮮度と信頼性のために必要だが、`.ldl` の canonical definition には混ぜない。
- ノードに Markdown 本文を詰め込まない。
- 知識の本体は typed graph と 2D attribute table に寄せる。
- AI agent は Markdown 追記ではなく、structured operation で更新する。
- 認証、保存、履歴、共同編集は host capability として扱う。
- Editor core は特定 server、DB、storage、auth provider を知らない。
- Registry は artifact の取得、更新、修復時にだけ参照し、通常の open、compile、render の実行依存にしない。

## 3. File Formats

### 3.1 `.ldl`

`.ldl` は LayerDraw Language のテキスト形式である。
字句、module、import / export、型、Query、View、Referenceの規範source syntaxは[ldl-language-specification.md](ldl-language-specification.md)、defaults、正規化、評価、hash、diagnostic、operation semanticsは[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)に従う。

用途:

- 編集正本
- Git diff
- VSCode extension
- MCP / AI 操作
- review / patch
- local-first workflow

`.ldl` に含めるもの:

- project metadata
- EntityType
- RelationType
- Layer
- Entity
- Entity attribute rows
- Relation
- Relation attribute rows
- Query
- View
- Reference
- identity reservations / moves
- asset references
- module / installed pack imports

`.ldl` に含めないもの:

- binary assets
- generated state snapshot
- system fields
- provenance
- revision history
- generated previews
- generated SVG / PNG / PDF
- server auth settings
- detailed audit log

`.ldl` 単体共有は definition-only である。
system fields / provenance が必要な共有は `.ldstate.json`、`.layerdraw`、または server backend を使う。
state backend 設定は `.ldl` に書かず、`project.ldbackend.json` または `layerdraw.backend.json` で扱う。
詳細は [state-backends.md](state-backends.md) に置く。

### 3.2 `.ldpack`

`.ldpack` は registry に登録する pack container である。

用途:

- EntityType / RelationType / View / schema pack 配布
- pack versioning
- dependency declaration
- checksum / signature verification
- optional assets / previews

構成:

```text
aws_core_1.0.0.ldpack
  manifest.json
  pack.ldl
  modules/
  assets/
  previews/
  checksums.json
  signature.json
```

原則:

- registry に登録する pack は、asset の有無に関係なく必ず `.ldpack` にする。
- `pack.ldl` は pack の構造定義である。
- `assets/` と `previews/` は optional である。
- `.ldl` を registry に直接登録しない。
- `.ldpack` は project や template ではない。

### 3.3 `.layerdraw`

`.layerdraw` は project / template / export 共有用の zip container である。

用途:

- 成果物共有
- template 配布
- offline portable container
- asset 込み再現
- preview / export artifact 同梱

構成:

```text
project.layerdraw
  manifest.json
  document.ldl
  schema/
  layers/
  views/
  references/
  document.json
  layerdraw.resolved.json
  layerdraw.index.json
  pack/
  state/
    current.json
    provenance.json
  assets/
  previews/
  exports/
```

原則:

- `document.ldl`はentry moduleであり、entryから選択されるproject-local LDL source treeが正本definitionである。
- project-local source は単一 `document.ldl` でもよいが、LayerDraw が生成する標準構成では `schema/`、`layers/`、`views/`、`references/` に分割する。
- `layers/<layer-id>/` にはその Layer の Entity と、その Entity を `from` とする Relation を置く。同一 Layer / Layer 間 Relation でディレクトリを分けない。
- Entity/Relationのrow groupはownerと同じmoduleに置き、ownerのexport/importがrow subtreeも選択する。ownerとrowを分離するmodule構成は作らない。
- ファイル配置は意味モデルに影響せず、任意の合法な module 分割を許可する。標準構成への移動は formatter ではなく明示的な workspace organization で行う。
- `document.json`はGo Engineがsource treeから生成するoptionalなNormalizedDocument snapshotであり、interoperabilityとcache warm-upに使える。validation、Query、View、render、source repairの権威入力ではなく、sourceから再生成した結果と一致しなければ破棄する。
- `layerdraw.index.json` はsource treeとresolved packから再生成できる非正本のsemantic indexである。
- `layerdraw.resolved.json` は導入済み pack の解決結果を固定する共有可能な metadata である。
- `pack/` には検証・展開済みの pack directory を置く。`.ldpack` archive 自体は runtime input として入れ子にしない。
- `state/` には current state / provenance snapshot を置ける。
- `assets/` にはローカル画像や pack 由来の同梱 asset を置く。
- `previews/` と `exports/` は派生成果物であり、正本ではない。
- registry に登録する template は `.layerdraw` にする。
- `.layerdraw` は履歴 DB ではない。

### 3.4 Resolved Dependency Metadata

`layerdraw.resolved.json` は pack の解決結果を記録する metadata である。
これは `.ldl` の一部ではなく、system fields / provenance を保持する state file でもない。

記録するもの:

- registry source identity
- pack identity
- exact resolved version
- content digest
- local path
- expanded file digests
- dependency install-name mappings

記録しないもの:

- registry credential
- auth token
- system fields / provenance
- revision history

`.ldl` を directory project として扱う場合は entry file と同じ project root に置き、`.layerdraw` では container 内に同梱する。
`layerdraw.resolved.json` と参照先の展開済み `pack/` tree を共有することで、Registry へ接続せず同じ依存を再現できる。

## 4. Asset Model

`.ldl` は画像バイナリを持たず、asset reference だけを持つ。

asset 解決は host capability である。

解決順序:

1. source module から解決した project-relative assets
2. source module から解決した展開済み installed pack assets
3. `.layerdraw` container 内の対応する `assets/` / `pack/` entry
4. fallback lucide icon
5. fallback shape

通常の asset 解決では remote registry を参照しない。
不足 asset の取得は Library から明示的に install または repair した時だけ行う。

asset 種別:

```text
installed pack asset
  pack/aws_complete/modules/compute.ldl
    image "../assets/aws-ec2.png"

project-relative asset
  image "assets/my-service.png"

packaged asset
  project.layerdraw/assets/my-service.png
```

ローカル独自画像を使う場合、assetを含まない`.ldl`単体では共有再現できない。共有時は`.layerdraw`にpackageするか、project-local LDL source treeと`assets/` directoryをセットで渡す。
pack asset は宣言元 `.ldl` module からの相対 path として、展開済み `pack/<install-name>/` 内で解決する。通常の open / compile / render では Registry や host cache への fallback fetch を行わない。

## 5. Domain Model

### 5.1 Project

Project は 1 つの LayerDraw document を表す。

Local modeでは、entry moduleを起点とする`.ldl` source tree、または`.layerdraw`ファイルがprojectに相当する。単一`.ldl`も合法なsource treeである。

Server mode では project metadata、current revision、access policy、artifact refs を持つ。

### 5.2 EntityType

EntityType はノードの class である。

持つもの:

- authored local ID / StableAddress
- display name
- description
- icon
- image
- color
- representation
- typed columns
- unique constraints
- tags / annotations

EntityType は schema であり、instance data は Entity の `attributeRows` に入る。

### 5.3 Entity

Entity は意味のある対象を表す。

例:

- business process
- service
- API
- database
- VM
- OS
- hardware
- subnet
- metric
- dashboard
- decision
- constraint
- task

Entityはauthored local IDからStableAddressを構成し、Layer / EntityType StableAddressを参照して、0行以上のrow StableAddress付き`attributeRows`を持てる。

### 5.4 RelationType

RelationTypeはRelation factのclass / schemaである。semantic kind、endpoint role / EntityType / Layer制約、cardinality、duplicate policy、属性表の列定義、traversal policy、View projection、render hints、export hintsを持つ。

RelationTypeはproject-local schemaまたは展開済みPack moduleから明示的に解決する。Coreにdomain固有RelationTypeをハードコードしない。

### 5.5 Relation

Relation は Entity 間の意味付き関係である。
Relation は線ラベルではなく、RelationType によって意味、endpoint 制約、cardinality、属性表、View 変換ルール、render hints、export schema を持つ。
詳細は [relation-type-system.md](relation-type-system.md) に置く。

例:

- calls
- writes
- runs_on
- connected_to
- depends_on
- owns
- implements
- decided_by
- derives_from
- joins_to
- used_in

Go View MaterializerはRelationType projectionとViewの目的からedge / nesting / overlay / badge / hiddenの意味を決める。TS RenderはそのViewDataとrender hintsから線style、routing、labelなどのvisual表現を決める。

Relationはauthored local IDから構成したStableAddress、RelationType StableAddress、`from` / `to` Entity StableAddress、optional display fields、0行以上のrow StableAddress付き属性行を持つ。Entityの包含も別fieldではなくtyped Relationで表現する。

### 5.6 Layer

Layer は関心領域または構造上の階層を表す。

インフラ刷新の標準例:

- business flow
- data flow
- application
- OS / VM
- hardware
- network

Domain-specific template では別の固定 Layer を持てる。

### 5.7 Query / ViewRecipe

QueryはMaster Graphから対象ID集合とRelation集合を選ぶ、保存可能なtyped predicate / traversal recipeである。raw Cypher / SQL文字列は正本に保存せず、backend queryへcompileする。

Project SearchはFTS / Vector / HybridによってQueryの探索起点候補を発見する派生operationであり、Query recipeとは分離する。SearchHitをView selectionとして保存せず、確認したStableAddressまたは一般化したtyped predicateをQueryへ明示する。Graph AnalysisはQueryResultまたは明示subgraphにPageRank、K-Core、Louvain、SCC、WCCを適用する派生operationであり、Master Graphを暗黙更新しない。厳密な契約は[search-query-and-analysis.md](search-query-and-analysis.md)に従う。

ViewRecipeは保存される切り口である。

ViewRecipeはExcel sheetや個別図面に相当するが、表示対象のコピーではない。QueryまたはDiff sourceとtyped View shapeを結合する再生成可能なrecipeである。

ViewRecipeが持つもの:

- id
- display name
- category (`topology`、`inventory`、`dependency`、`hierarchy`、`flow`、`impact`、`diff`、`context`)
- optional intent
- exactly one Query / Diff source
- exactly one typed shape (`diagram`、`table`、`matrix`、`tree`、`flow`、`context`、`diff`)
- optional RelationType projection overrides
- zero or more export recipes

QueryとViewRecipeはproject-local `.ldl` source treeに保存し、`.layerdraw`へそのsource treeを格納する。

### 5.8 Generated ViewData / RenderData

ViewDataはMaster GraphとViewRecipeから生成するrenderer非依存のsemantic中間表現である。RenderDataはViewDataから生成する。ExportではGo EngineがViewDataとExportRecipeからExportPlanを生成し、versioned serializerがExportArtifactとSource Manifestを生成する。

ViewData / RenderDataは原則として永続化しない。cacheは可能だが正本ではない。

```text
Master Graph + ViewRecipe
  -> ViewData
    -> RenderData
    -> ExportPlan -> ExportArtifact + Source Manifest
```

## 6. View / Query / Document Generation

LayerDraw の出発点は、多次元構造から必要な切り口を取り出すことである。

インフラ刷新の例:

```text
Master Graph
  business process
  data flow
  application
  database
  OS / VM
  hardware
  network

Views
  業務フロー
  データフロー
  アプリケーション構成
  OS / VM 対応
  ハードウェア構成
  ネットワーク構成
  本番環境
  DR環境
  責任分界
  影響範囲
  移行対象一覧
```

正本を変えれば、関連 View の diagram、table、matrix、tree、flow、context、CSV、XLSX、PDF、PowerPoint export が再生成される。

View System は UI から設計しない。
先に [view-conversion-contract.md](view-conversion-contract.md) の契約に従い、以下を定義する。

```text
Master Graph + ViewRecipe
  -> ViewData
    -> RenderData
    -> ExportPlan -> ExportArtifact + Source Manifest
```

各 ViewCategory / ViewShape は、正本グラフから ViewData へ変換できる条件、lossless / traceable-summary / lossy の分類、plain export capability を持つ。
ViewCategory / ViewShape を増やす時は、DSL recipe、ViewData type、renderer input、plain export artifact を同時に設計する。
PDF / PPTX / DOCX のビジネス文書化は、View System の本体ではなく Document Generation として別機能に分ける。

Query-backed View は以下の能力をすべて設計対象にする。
実装順序は依存関係で決める。

0. entry-point discovery
   - lexical search with FTS / BM25
   - semantic search with Vector / HNSW
   - Hybrid Search with versioned RRF profile
   - selected SearchHitからexplicit Query rootへの変換

1. explicit filters
   - layers
   - entityTypes
   - relationTypes
   - roots
   - depth

2. no-code query builder
   - attributes
   - relation hop
   - layer / type / name

3. saved graph query
   - typed predicate tree compiled to the graph backend
   - named query
   - parameterized query

4. scoped graph analysis
   - PageRank
   - K-Core
   - Louvain
   - SCC / WCC
   - QueryResultまたは明示address集合だけをscopeにする

browserではGo Engine WASMがparameterized Query / Search / Analysis ExecutionPlanを生成し、TS LadybugDB WASM adapterはplan実行とtyped raw rows返却だけを担当する。SearchResult、QueryResult、AnalysisResult、ViewDataの規範化はGo Engineへ戻す。Embedding生成はRuntimeがEmbedding Providerへ委譲し、TS adapterやMCP adapterが検索対象textを独自生成しない。

## 7. Host Capabilities

Editor は capability に応じて機能を出す。

```ts
interface LayerDrawHostCapabilities {
  auth?: boolean;
  storage?: boolean;
  history?: boolean;
  realtime?: boolean;
  packages?: boolean;
  registry?: boolean;
  mcp?: boolean;
}
```

機能は常に存在する前提にしない。

例:

- local-only desktop: history なし、realtime なし
- VSCode in Git repo: Git history を history provider として使える
- Web + LayerDraw Server: auth / history / realtime / registry / MCP あり
- Embedded SDK: host app が渡す capability に依存

## 8. Architecture

規範実装は、Go の意味論・runtime層、versioned protocol、TypeScriptのpresentation層、framework shellを分ける。

```text
Go LayerDraw Engine
  Compiler / Workbench
  Query planner and result normalizer
  ViewData materializer / ExportPlan
  package validator and builder

Go LayerDraw Runtime
  document sessions / revisions / autosave
  state / access / storage orchestration
  history / realtime / audit

Versioned Engine Protocol
  generated Go / TypeScript bindings
  in-process / WASM / stdio / HTTP / Wails transports

TypeScript Presentation
  Composer / Render / Viewer / React / Library UI
  Engine client / Server client / SDK facades

Framework shells
  Echo / Wails / React Web / VSCode / MCP Apps / Next.js host
```

実装・配布境界の詳細は[architecture.md](architecture.md)、Compiler / Workbenchの純粋入出力は[compiler-architecture.md](compiler-architecture.md)、Engine ProtocolからVersion / Releaseまでの境界契約は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

Applications / executables:

```text
apps/web
  Web app host

apps/desktop
  Wails desktop host

apps/mcp-app
  MCP Apps client host

extensions/vscode
  VSCode extension host

integrations/marketplace
  Provider integration shells

cmd/layerdraw-engine
  native stdio sidecar

cmd/layerdraw-host
  local Runtime / MCP sidecar

cmd/layerdraw-server
  LayerDraw Server executable

cmd/layerdraw-registry
  LayerDraw Registry executable
```

## 9. LayerDraw Server

LayerDraw Server はエディタコアではなく host server である。

責務:

- auth / Actor resolution
- Instance administration
- Organization / membership / Team / Service Account management
- Workspace management
- project metadata
- `.ldl` revision store
- `.layerdraw` artifact generation
- Time Machine
- realtime collaboration
- presence
- storage adapters
- registry access
- remote MCP endpoint
- access / share / audit / admin policy
- entitlement enforcement / usage event emission

Echo は server implementation の 1 つであり、LayerDraw core ではない。

### 9.1 Storage Separation

Server と storage は分離する。

```text
LayerDraw Server
  -> OrganizationRepository
  -> WorkspaceRepository
  -> IdentityRepository
  -> AccessRepository
  -> ProjectRepository
  -> DocumentStore
  -> StateBackend
  -> ArtifactStore
  -> HistoryStore
  -> RealtimeProvider
  -> ExternalFileStore
  -> EntitlementProvider
  -> UsageSink
```

対応候補:

- local filesystem
- S3 compatible storage
- Azure Blob Storage
- Google Cloud Storage
- Cloudflare R2
- MinIO
- SharePoint
- Google Drive
- OneDrive
- GitHub
- GitLab
- Nextcloud
- PostgreSQL
- SQLite
- Cloudflare D1

Object storage は `.ldl` snapshot、`.layerdraw` container、assets、previews、exports を持つ。

State backend は current state、provenance、audit summary、lease を持つ。
local では `<basename>.ldstate.json`、remote では object storage / external drive / managed backend を使う。
Backend binding は `.ldl` ではなく `project.ldbackend.json` または `layerdraw.backend.json` に置く。

SQL / metadata repository はOrganization、membership、Team、Service Account、Workspace、Project metadata、access、revision index、search metadata、audit indexを持つ。Go Runtime library単体はSQLを要求しないが、Organization / Workspace機能を提供する`layerdraw-server`はdurable metadata repositoryを必須とし、SQLiteまたはPostgreSQL等のadapterを構成する。

External drive は provider ACL と version token を尊重する。

### 9.2 Embedded Runtime

LayerDraw Server は standalone process としてだけ存在するものではない。

中核はGo製LayerDraw Runtime libraryとして切り出し、host application、self-host server、managed cloud、Desktop、local MCP bridgeから同じruntime semanticsを使えるようにする。

```text
Host application / LayerDraw Server
  -> Go LayerDraw Runtime
    -> Go LayerDraw Engine
    -> Go Access component
    -> immutable StateQuerySnapshot
    -> StorageAdapter
    -> EventPublisher
    -> LeaseManager
```

runtime の責務:

- document session の作成と復元
- actor / agentごとのHost Document IDでscopeされたStableAddressを対象にしたsemantic operation受付
- transient Working Document と immutable Committed Revision の分離
- `base_revision`とcommit後revisionの管理
- operation batch の原子的な validation / conflict detection / commit
- operation log への append
- canonical `.ldl` checkpoint の autosave
- snapshot compaction
- Query/View開始時にstate headを1つのimmutable StateQuerySnapshotへ固定し、definition/subject hashとの互換性を検査する
- transient collaboration event の publish
- storage adapter への conditional write

runtime は永続 DB を必須にしない。短命な compute 上では in-memory cache、ephemeral query index、temporary layout cache を持ってよいが、durable source of truth は storage adapter に置く。

### 9.3 Object-storage-backed Runtime

低コスト self-host、embedded SDK、serverless deployment のため、object storage を直接 document backend にできる構成を第一級に扱う。

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

`organization_id`と`document_id`はhost-owned identityであり、LDL Project StableAddressとは分離する。Documentは1つのOrganization / Workspaceに所属し、object key、metadata query、cache、search、history、asset、realtime、auditをOrganizationでscopeする。`source-manifest.json`はdefinition Project StableAddress、canonical project-relative path、source blob digest、source byte digest、entry module、resolved dependency metadata digest、expanded dependency tree digestを記録する。同じOrganization内では同じsource blobとdependency blobをrevision間で共有できるが、cross-organization deduplicationで存在やdigestを漏らさない。過去revisionはRegistryへ接続せず、この固定済みdependency closureから再現する。`view_address_hash`はView StableAddressのSHA-256で、metadataに元addressを保持する。

`head.json` は current version、latest source manifest、normalized definition hash、schema version、compaction marker、lease metadata を持つ。

write protocol:

1. client / agentが`base_revision`、`expected_subject_hashes`、owner削除用の`expected_subtree_hashes`、`expected_child_sets`、`idempotency_key`を含むoperation batchを渡す
2. runtime が current head と lease を確認する
3. operationをGo Workbenchへ適用し、Go Engineで検証する
4. pending operation recordとpending realtime outbox eventを`idempotency_key`付きでdurableに保存する
5. 変更 `.ldl` blob、必要な resolved / dependency blob、revision の source manifest を未公開artifactとして保存する
6. head を ETag / generation / version token 付きで conditional update し、definitionのCommitted Revisionを公開する
7. configured StateBackendへstate delta / snapshotを同じ`idempotency_key`とexpected state versionでwriteする
8. audit eventをappendし、operation recordへ最終`OperationResult` statusを記録してoutbox eventをreadyへ進める
9. event publisherがready eventをrealtime subscriberへdefinition revisionとstate statusを通知する

step 6前の失敗は`rejected`とし、durable pending outboxなしでheadを公開しない。step 6成功後のstate/audit失敗はdefinitionを保持して`committed_state_stale`、step 6の公開成否をrecoveryでも一意に判定できない場合だけ`needs_review`とする。state write、audit、outbox ready化はpending record、`idempotency_key`、公開済みrevisionから再開する。client / agentへはRuntimeCommitResult内にLDL詳細仕様11.3節の`OperationResult`を変更せず保持して返す。

object storage だけでは realtime notification と multi-writer coordination が弱いため、runtime は project / session 単位の single-writer lease を持つ。複数 AI / 複数ユーザーの同時編集は active runtime 内で直列化し、複数 compute が同じ project を触る場合は lease と storage の conditional write で競合を検出する。

この構成ではGo Runtime libraryのdocument durabilityに常駐DBを要求しない。一方、Public Server ApplicationはOrganization / Workspace / membership / ACL / audit / search metadataのdurabilityにSQL / KV repositoryを要求する。definition blobとrevisionの正本をobject storageへ置くことと、Server metadata repositoryを省略することを混同しない。

### 9.4 SDK Split

SDK は用途ごとに分ける。

```text
@layerdraw/viewer
  read-only visualization
  ViewData / stream -> interactive view
  no Engine / write runtime dependency

@layerdraw/client-sdk
  browser editor facade
  -> @layerdraw/engine-client
  -> Go Engine WASM or remote host

@layerdraw/server-sdk
  Node / Next.js / host backend adapter
  mode A: remote wrapper for layerdraw-server
  mode B: bundled layerdraw-host sidecar + host-integrated routes
  mode C: bundled layerdraw-engine sidecar for portable compile / preview only

Go LayerDraw Runtime
  storage-backed multi-actor sessions
  autosave / history / event stream / lease

@layerdraw/server-client
  remote LayerDraw Server API client
```

単に描画したいhostはViewDataを渡して`@layerdraw/viewer`だけを使える。ViewerはLDLをparseしない。
client側にLDL編集を組み込みたいhostは`@layerdraw/client-sdk`とGo Engine WASMまたはremote hostを使う。
host backend に server 型の LayerDraw API / MCP / storage / realtime entrypoint を生やしたい場合は `@layerdraw/server-sdk` を使う。
本格的な hosted persistence、Time Machine、realtime hub、marketplace backend が必要な場合は `layerdraw-server` を使う。

## 10. Time Machine

Time Machine は file format の機能ではなく host capability である。

runtime / server-backed projectでは、entryと全project-local `.ldl` module、resolved dependency digestを指す論理的なfull source-tree snapshot manifestをrevisionごとに保存する。blobのcontent-addressed共有はstorage最適化であり、revisionの意味を差分列へ変えない。

```text
Project
  document_id
  definition_project_address
  current_revision

Revision
  document_id
  revision
  source_tree_manifest
  resolved_metadata_digest
  dependency_tree_digest
  normalized_definition_hash
  actor_id
  operation_ids
  parent_revision
  created_at

Artifact
  document_id
  revision
  source_view_address
  kind
  blob_ref
```

差分保存は domain requirement にしない。必要なら storage optimization として後から導入する。

`.layerdraw` に全履歴を入れない。

理由:

- package が肥大化する
- latest-only prune が難しい
- Drive / Git / SharePoint の versioning と衝突する
- 共有用 package に過去履歴が漏れる
- 大きい zip の再生成が必要になる

提供形態別:

- Web + Server: Time Machine あり
- self-host: Time Machine あり
- Desktop local: 原則なし、sidecar history があれば可能
- VSCode: Git history があれば可能
- Embedded SDK: host が HistoryProvider を渡せば可能
- Drive / SharePoint: provider version history + LayerDraw revision index
- GitHub / GitLab: commit history

## 11. Realtime Collaboration

Realtime は runtime-backed capability である。

扱うもの:

- room
- participants
- cursor
- selection
- active view
- transient presence
- revision-protected document update

永続化するもの:

- canonical Committed Revision source
- revisionとnormalized definition hash / own-subject / subtree semantic hash
- actor
- semantic operation log
- audit metadata

永続化しないもの:

- cursor
- current selection
- focus
- typing state

Realtime transportはWebSocket、競合検出はrevision protectionを共通境界とする。command log、CRDT、OTは`RealtimeProvider` / `RealtimeRoom` adapterの実装選択であり、Go Engineの意味モデルには埋め込まない。

共同編集では transient な Working Document と、parse、name resolution、typing、reference integrity、row、endpoint、cardinality の検証を通過した immutable な Committed Revision を分離する。通常の query、View、render、export、read-only MCP operation は最新の Committed Revision を対象にする。

GUI / MCP / SDKはHost Document IDでscopeされたStableAddressを対象とするsemantic operationをruntimeへ渡す。operation batchは`base_revision`、own-subject hash、child-set hashを検査してdefinition revisionを原子的にcommitし、stale revision、same-field concurrent update、delete-versus-update、duplicate IDを黙って上書きしない。外部state writeが同時完了しない場合はdefinitionを`committed_state_stale`として開きreconcileする。

同期方式は revision 付き command log、CRDT、OT のいずれでもよいが、`RealtimeProvider` / `RealtimeRoom` adapter の下で同じ validation、structured conflict、commit、checkpoint 契約を実装する。raw text 編集ではキー入力ごとの全体 format を行わず、完全な変更ノード、明示 format、または checkpoint の境界で canonical source を生成する。

## 12. Delivery Channels

提供形態は、ユーザーが LayerDraw をどこからどう使うかで定義する。
ここでは実装コンポーネント、内部 package、backend mode、storage mode、機能一覧を混ぜない。
LayerDraw Library は独立した提供形態ではなく、すべての提供形態から利用できる共通の pack / template 利用画面である。

### 12.1 LayerDraw SaaS

LayerDraw の Web サービスにログインして、ブラウザ上でプロジェクトを作成、編集、共有する。

### 12.2 LayerDraw Self-host

自社または自組織で用意した LayerDraw 環境にアクセスして、組織内のプロジェクトを編集、共有する。

### 12.3 LayerDraw Desktop App

PC に LayerDraw アプリをインストールして、ローカルまたは接続先の LayerDraw document を開いて編集する。

### 12.4 LayerDraw VSCode Extension

VSCode 上で `.ldl` ファイルを編集しながら、LayerDraw project を確認、検証、更新する。

### 12.5 LayerDraw SDK

自分たちの Web app、業務 system、agent runtime に LayerDraw の表示、編集、実行体験を組み込んで使う。

### 12.6 LayerDraw MCP Apps

AI client 上で、AI が生成または更新した LayerDraw project をリアルタイムに見ながら確認、操作、承認する。

### 12.7 LayerDraw Marketplace Integrations

Google Workspace、Microsoft、GitHub、Atlassian など、普段使っている service から LayerDraw を開く、連携する、または生成物として扱う。

## 13. Feature Definitions

機能は、ユーザー、人間の開発者、または AI agent が LayerDraw を使って完了できる結果で定義する。
ここでは提供形態、実装コンポーネント、内部 package、backend mode、storage mode を混ぜない。

後続の feature matrix では、この定義を行として使う。
行にしてよいのは observable capability だけであり、`Web UI`、`LayerDraw Server`、`Runtime`、`HostAdapter`、`StorageAdapter`、`MCP Server` のような実装部品は機能行にしない。

### 13.1 Project / Document

| ID | Feature | Definition |
| --- | --- | --- |
| F01 | Project management | 複数のLayerDraw projectを一覧、作成、検索、display name変更、削除、archive、整理できる。通常の名前変更はProject ID / StableAddressを変えない。 |
| F02 | Workspace management | 1つのOrganization内でWorkspaceを作成、更新、archiveし、Member / TeamとProjectを分類、管理できる。 |
| F03 | Project open | 既存の 1 つの project または document を開き、graph、layers、types、views を参照可能にする。 |
| F04 | Project save | 開いている 1 つの project または document の変更を保存する。 |
| F05 | Project settings | project 名、説明、default view、schema version、package policy など project 単位設定を変更できる。 |
| F06 | Recent / pinned projects | 最近開いた project や固定 project へ再アクセスできる。 |

### 13.2 File / Artifact

| ID | Feature | Definition |
| --- | --- | --- |
| F07 | `.ldl` read | `.ldl` を読み、LayerDraw definition として parse / validate できる。 |
| F08 | `.ldl` edit / apply | `.ldl`の正本定義を変更できる。標準はsemantic operation、大量作成はscoped LDL fragment、低レベルfallbackはrevision-protected source patchとする。 |
| F09 | `.ldl` export | 現在の正本定義を `.ldl` として取り出せる。 |
| F10 | `.layerdraw` import | `.layerdraw` container を読み、含まれる definition、state、assets、previews、exports を利用できる。 |
| F11 | `.layerdraw` export | project を `.layerdraw` container として取り出せる。 |
| F12 | External format import | 外部形式から LayerDraw project または View を生成できる。 |
| F13 | Asset management | project 内 asset を追加、置換、削除、container 同梱対象にできる。 |
| F14 | Asset resolution | asset reference を解決し、表示、export、container 化に使える。 |

### 13.3 Authoring

| ID | Feature | Definition |
| --- | --- | --- |
| F15 | Graph authoring | Entity、Relation、Layer、attribute rows、relation rows を作成、更新、削除できる。 |
| F16 | Schema authoring | EntityType、RelationType、columns、stable row ID、unique constraint、projection、render hints を定義、変更できる。 |
| F17 | View authoring | ViewRecipe を作成、更新、削除できる。 |
| F18 | View composing | 目的に応じて ViewRecipe を組み立てられる。 |
| F19 | Layout / presentation editing | typed shapeが許可するlayout、direction、表示密度、label、composed設定など表示意図を編集できる。transientな展開状態は正本へ混ぜない。 |
| F20 | Graph query / analysis | graph、relation、attribute row、および明示的に固定したcurrent state / provenance snapshotに対して、型付き条件検索、directed hop探索、saved queryを実行できる。QueryResultまたはrevision固定済み明示subgraphへPageRank、K-Core、Louvain、SCC、WCCを適用できる。state依存Queryはsnapshotの必須/任意を宣言し、暗黙の現在時刻やbackend固有queryを使わない。 |
| F21 | Project search | project内のLayer、Entity / Relationとrow、Type、Query、View、Reference、attribute値をFTS / BM25、Vector / HNSW、versioned Hybrid rankingで横断検索し、StableAddress、graph entry、score理由を取得できる。検索結果は派生候補でありView selectionへ暗黙保存しない。 |
| F22 | Validation | syntax、参照、schema、relation 制約、projection、export 可否を検証し diagnostics を返せる。 |
| F23 | Conflict resolution | 競合を検出し、解決、再適用、破棄を選べる。 |

### 13.4 Query / View / ViewData

| ID | Feature | Definition |
| --- | --- | --- |
| F24 | View materialization | Master Graph + ViewRecipeと、recipeが要求する場合は1つのimmutable StateQuerySnapshotから、ViewDataを決定論的に生成できる。stateなし、stale、redactedの扱いはrecipeと規範診断で一意に決まる。 |
| F25 | View rendering | ViewData を diagram、table、matrix、tree、flow、context、diff として表示できる。 |
| F26 | View selection | project 内の View 一覧から対象 View を選択、切替できる。 |
| F27 | View coverage analysis | Entity / Relation がどの View に含まれるか、未カバー要素が何かを説明できる。 |
| F28 | Projection explanation | 表示要素がどのdefinition SourceRefs、StateRefs、snapshot、projection ruleから生成されたか説明できる。 |
| F29 | Streaming render | 更新中の project を、中間状態も含めて継続的に表示できる。 |

### 13.5 Export / Review

| ID | Feature | Definition |
| --- | --- | --- |
| F30 | Plain export | ViewDataから外部artifactを生成し、state依存ViewDataでは使用したsnapshot hashまたはoptional no-state入力とStateRefsをSource Manifestへ必ず保持する。 |
| F31 | Diff / review | 変更前後、revision 間、proposal と current の差分を表示、確認できる。 |
| F32 | Approve / apply | 提案された変更を確認し、承認後に正本へ適用できる。 |
| F33 | Redacted export | Access/redaction policyをQuery/View materializationより前に適用し、秘匿対象をmissingへ偽装せず削除した共有用`.layerdraw`と派生成果物を作れる。package manifestへredactionを明示し、snapshotと派生成果物を再ハッシュする。 |
| F34 | Artifact preview | container や export artifact に含まれる preview を表示できる。 |

### 13.6 Storage / State

| ID | Feature | Definition |
| --- | --- | --- |
| F35 | Local file workflow | local filesystem 上の `.ldl` / `.layerdraw` を開き保存できる。 |
| F36 | External storage workflow | 外部保存先にある project を開き保存できる。 |
| F37 | Storage connection management | 外部保存先との接続、認可、接続解除、接続状態確認を行える。 |
| F38 | Autosave / recovery | 変更を自動保存し、中断後に復元できる。 |
| F39 | State / provenance | system fields、source、verifiedAt、confidence、row provenanceを`.ldl`の値から分離して保持し、credential scopeとredaction policyを適用した型付きStateQuerySnapshotとしてQuery/Viewへ固定入力できる。`.ldl`にはstate値ではなく参照するrecipeだけを保持する。共有Project ACLの管理は含まない。 |
| F40 | Backend binding | `.ldl` と対応する state / backend 設定を解決し、stale や mismatch を検出できる。 |
| F41 | Time Machine | revision list、preview、diff、restore as new revision を実行できる。 |
| F42 | Sync / reconcile | 正本、state、remote head、provider version のズレを検出し、再同期できる。 |

### 13.7 Auth / Sharing / Collaboration

| ID | Feature | Definition |
| --- | --- | --- |
| F43 | Actor resolution | 操作主体を Actor として識別できる。 |
| F44 | Organization membership management | Organizationにscopeしたuser membership、team、service accountを管理できる。Userは複数Organizationへ所属できるが、Team / Service AccountはOrganizationをまたがない。 |
| F45 | Resource access control | Instance、Organization、Workspace、Projectのrole / ACLを設定・永続化し、open、share、Query、Export、管理操作とAuthoring grantの基礎権限を判定できる。書込内容の意味分類と全経路強制はF62、保存先ACLがある場合は両decisionの積集合を使う。ローカルcredential scopeによるstate field filteringはF39に含む。 |
| F46 | Sharing workflow | project 共有、invite、link 共有、権限変更、共有解除ができる。 |
| F47 | Realtime collaboration | 複数主体が presence や変更を同期しながら同じ project を扱える。 |
| F48 | Comments / annotations | project、View、Entity、Relation、差分に対してコメントや注釈を残せる。 |

### 13.8 Registry / Pack / Template

| ID | Feature | Definition |
| --- | --- | --- |
| F49 | Public registry | 公開 `.ldpack` と `.layerdraw` template を検索し、詳細、version、依存、検証状態を確認して取得できる。 |
| F50 | Private / custom registry source | Organization private、self-host registry、local registryなどの非公開または独自registry sourceを選択し、認可された`.ldpack`と`.layerdraw` templateを検索、確認、取得できる。 |
| F51 | Pack / template authoring | pack を `.ldpack` として、template を `.layerdraw` として作成できる。 |
| F52 | Pack install / update | `.ldpack` を検証してローカル実体化し、解決結果を固定した上で、pin、upgrade、repair、remove と migration diagnostics の確認ができる。 |
| F53 | Create from template | registry 上の `.layerdraw` template と required pack を検証、実体化して新規 project を作成できる。 |

F49-F53 の標準ユーザー導線は GUI の LayerDraw Library とする。
専用 CLI や独自 package manager は、これらの機能を利用するための前提にしない。

### 13.9 AI / MCP

| ID | Feature | Definition |
| --- | --- | --- |
| F54 | MCP operation | MCP経由でsymbol lookup、FTS / Vector / Hybrid Search、bounded subgraph inspection、Structural Query、Graph Analysis、validate、apply、preview、View / exportを実行できる。 |
| F55 | MCP connection management | MCP 操作面へ接続、認可、切断できる。 |
| F56 | Agent scope management | AI agentに許可するread、export、propose、applyとAuthoring Capabilityの委任subsetを制御できる。委任元ActorまたはProject Policyのgrantを拡張しない。 |
| F57 | AI proposal workflow | AI が変更案を作り、diff、diagnostics、preview 付きで提示できる。 |

### 13.10 Administration

| ID | Feature | Definition |
| --- | --- | --- |
| F58 | Admin policy | sharing、registry、MCP、authoring、export、redaction、retention の policy を設定できる。 |
| F59 | Audit | Organization scopeを保持して、誰が、いつ、何を、どの経路で変更・管理したか追跡できる。 |
| F60 | Entitlement / quota / usage | 外部または静的EntitlementSnapshotとquotaに応じて機能利用可否を判定し、課金処理を持たないUsageEventをUsageSinkへ発行できる。 |
| F61 | Organization management | 1つのServer Instance内で複数Organizationを作成、更新、archiveし、membership、policy、storage / registry binding、audit scopeを分離できる。 |
| F62 | Authoring policy enforcement | Schema、Graph instance、Query、View、Reference、Asset、source保守、Project設定、Package操作を別capabilityとしてgrant / deny / constrainする。Definition差分のAuthoringImpactとLDL外操作のHostOperationImpactを合成し、semantic operation、source patch、import、Registry、restore、reconcile、realtime、SDK、MCPの全書込経路で原子的に強制できる。 |

### 13.11 Feature x Delivery Matrix

`✓` は、その提供形態でユーザーに機能として提供することを示す。
`-` は、その提供形態では LayerDraw の機能として提供しないことを示す。
この表は実装 package や backend mode を表さない。

| ID | Feature | SaaS | Self | Desktop | VSCode | SDK | MCP Apps | Marketplace |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| F01 | Project management | ✓ | ✓ | - | - | ✓ | - | ✓ |
| F02 | Workspace management | ✓ | ✓ | - | - | ✓ | - | ✓ |
| F03 | Project open | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F04 | Project save | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F05 | Project settings | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F06 | Recent / pinned projects | ✓ | ✓ | ✓ | - | ✓ | - | ✓ |
| F07 | `.ldl` read | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F08 | `.ldl` edit / apply | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F09 | `.ldl` export | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F10 | `.layerdraw` import | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F11 | `.layerdraw` export | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F12 | External format import | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F13 | Asset management | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F14 | Asset resolution | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F15 | Graph authoring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F16 | Schema authoring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F17 | View authoring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F18 | View composing | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F19 | Layout / presentation editing | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F20 | Graph query / analysis | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F21 | Project search | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F22 | Validation | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F23 | Conflict resolution | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F24 | View materialization | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F25 | View rendering | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F26 | View selection | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F27 | View coverage analysis | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F28 | Projection explanation | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F29 | Streaming render | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F30 | Plain export | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F31 | Diff / review | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F32 | Approve / apply | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F33 | Redacted export | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F34 | Artifact preview | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F35 | Local file workflow | - | - | ✓ | ✓ | ✓ | - | - |
| F36 | External storage workflow | ✓ | ✓ | ✓ | - | ✓ | ✓ | ✓ |
| F37 | Storage connection management | ✓ | ✓ | ✓ | - | ✓ | - | ✓ |
| F38 | Autosave / recovery | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F39 | State / provenance | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F40 | Backend binding | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F41 | Time Machine | ✓ | ✓ | - | - | ✓ | ✓ | ✓ |
| F42 | Sync / reconcile | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F43 | Actor resolution | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F44 | Organization membership management | ✓ | ✓ | - | - | ✓ | - | - |
| F45 | Resource access control | ✓ | ✓ | - | - | ✓ | ✓ | ✓ |
| F46 | Sharing workflow | ✓ | ✓ | - | - | ✓ | - | ✓ |
| F47 | Realtime collaboration | ✓ | ✓ | - | - | ✓ | ✓ | ✓ |
| F48 | Comments / annotations | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F49 | Public registry | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F50 | Private / custom registry source | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F51 | Pack / template authoring | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F52 | Pack install / update | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F53 | Create from template | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F54 | MCP operation | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F55 | MCP connection management | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F56 | Agent scope management | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F57 | AI proposal workflow | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| F58 | Admin policy | ✓ | ✓ | - | - | - | - | - |
| F59 | Audit | ✓ | ✓ | - | - | - | - | ✓ |
| F60 | Entitlement / quota / usage | ✓ | ✓ | - | - | ✓ | - | ✓ |
| F61 | Organization management | ✓ | ✓ | - | - | ✓ | - | ✓ |
| F62 | Authoring policy enforcement | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

Matrix notes:

- Desktop は remote file picker / connector 経由で external storage workflow を提供できる。
- VSCode は workspace / Git 前提なので、LayerDraw Extension 自体は external storage connection や Time Machine state を持たない。
- Marketplace は provider storage / provider ACL を使う場合でも、LayerDraw 側で open、share、access decision、audit surface を提供する。
- SDK の `✓` は、host application に組み込める LayerDraw capability を提供するという意味であり、host application そのものを提供形態に追加しない。
- MCP Apps の tool availability は、接続先 host が提供する機能に応じて MCP 詳細設計で制御する。
- F49-F53 は全提供形態で LayerDraw Library から利用できる。SDK は Library UI を埋め込み可能な component として提供する。
- SaaS / Self-hostは同じOrganization / Workspace / Projectモデルを使う。Self-hostで複数Organizationを使うためにServerを複製する必要はない。
- F62のDesktop / VSCode local fileではOS所有者の直接編集を防ぐsecurity guaranteeを意味しない。`layerdraw-host`、server project、またはtrusted host persistenceを通るwriteでAuthoring Policyを強制し、local ownerの既定grantは`full_authoring`とする。

## 14. Package Responsibilities

ここでいう component は機能の意味と不変条件を所有する論理責務である。npm package、Go package、binary、framework shell、提供形態とは同じ分類ではない。Registry に登録される `.ldpack` / `.layerdraw` も software package ではなく artifact である。

Rules:

- 一つの意味規則は一つの primary component だけが所有する。
- LDL の意味論は Go Engine だけが実装する。
- UI、SDK、transport、framework shell は意味論を再定義しない。
- host adapter は外部能力を供給するが、Query、View、Access、container の規範を変えない。
- 機能マップは capability component に、提供形態マップは implementation artifact に対応させる。
- 詳細な実装境界は [architecture.md](architecture.md) を規範とする。
- package path、依存DAG、binary composition、delivery bundle closureは[component-package-boundary-specification.md](component-package-boundary-specification.md)を規範とする。
- component間のoperation、port、状態遷移、failure、capability、version交渉は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

### 14.1 Capability Components

| Component | Primary implementation | Responsibility |
| --- | --- | --- |
| Engine | Go | Compiler、Workbench、LDL operations、validation、StableAddress / hash、AuthoringImpact、SearchDocument / SearchResult、QueryResult、AnalysisResult、ViewData、ExportPlan、`.layerdraw` / `.ldpack` 規範 |
| Runtime | Go | document session、open / save、revision、autosave、BackendBinding、StateQuerySnapshot、AuthoringImpact / HostOperationImpactの合成とAuthoringGrant / Decision強制、Access適用済みSearch Index / Embedding Provider、conflict、realtime、history orchestration |
| Access | Go | Actor、role、shared ACL、credential / agent scope、AuthoringPolicy、AuthoringImpact / HostOperationImpact、state-field redaction、entitlement inputを統合したdecision |
| Organization | Go application component | Server Instance内のOrganization、membership、Team、Service Account、Organization / Authoring policy、storage / registry binding、tenant isolation |
| Workspace | Go application component | Organization配下のWorkspace、multi-project metadata、settings、recent / pinned index。local file open historyはhost shellが提供する |
| Review | Go application component + TS presentation | semantic diff / AuthoringImpact input、Required Capability、comments、annotations、authorized approve / apply workflow。definition diffとimpactはEngineが生成する |
| Share | Go application component | share intent、target、invite / link / provider handoff、共有診断。provider ACL永続化はadapterへ委譲する |
| Registry | Go library / service | registry protocol、source / publisher / signature policy、dependency resolution、install / update / pin / remove transaction。container / LDL検証はEngineへ委譲する |
| Entitlement | Go application component | external / static EntitlementSnapshot取得、quota state、Accessへ渡すneutral input。商用契約や請求の意味論は持たない |
| Usage Meter | Go application component | Organization / Workspace / Project scope付きUsageEvent生成とUsageSinkへの配送。請求計算は持たない |
| MCP Host | Go adapter | Engine / Runtime capabilityをMCP tools / resourcesとして公開する。意味論を持たない |
| Presentation | TypeScript | Composer、Render、Viewer、React UI、Library UI。Engine protocolを利用しsource-of-truth logicを持たない |
| Export Serialization | GoまたはTypeScript | Go EngineのExportPlanをversioned format profileに従ってartifact / Source Manifestへserializeする。semantic mappingを変更しない |
| Host Adapters | GoまたはTypeScript | storage、credential、auth、registry、history、realtime、file picker、provider SDKを実行環境へ接続する |

### 14.2 Implementation Artifacts

| Artifact | Kind | Responsibility |
| --- | --- | --- |
| LayerDraw Engine library | Go package | Engine componentとAuthoringImpact classifierの唯一の規範実装 |
| LayerDraw Runtime library | Go package | Runtime、Authoring Access enforcement、host port orchestration |
| LayerDraw Access library | Go package | Actor / ACL / credential / agent scope / AuthoringPolicy / entitlementを統合したdecision |
| LayerDraw Registry client library | Go package | Registry source、signature / publisher policy、dependency、install transaction |
| LayerDraw native exporters | Go package | ExportPlanをnative format profileでserializeする。semantic mappingは変更しない |
| LayerDraw MCP Host adapter | Go package | Engine / Runtime ProtocolをMCP tools / resourcesへ写す |
| LayerDraw Engine WASM | Go WASM | browser内のEngine Protocol endpoint |
| `layerdraw-engine` | native Go binary | pureなstdio Engine Protocol sidecar。portable compile / preview用 |
| `layerdraw-host` | native Go binary | Engine、Runtime、Access、Registry client、Review、native exporters、MCP Hostをlinkしたlocal host sidecar |
| `layerdraw-server` | Go service binary | Engine、Runtime、Access、Registry、export、MCP、Organization、Workspace、Review、Share、Entitlement、Usage Meterをlinkしたmulti-organization HTTP / WebSocket host |
| `layerdraw-registry` | Go service binary | official / organization private / self-host Registry service |
| `@layerdraw/protocol` | generated TS | Engine / Runtime protocol型とcodec。手書き意味論を含めない |
| `@layerdraw/engine-client` | TS | WASM / stdio / HTTP / Wails transport facade |
| `@layerdraw/query-adapter-ladybug-wasm` | TS + WASM | Go EngineのQuery / Search / Analysis ExecutionPlanとSearchIndexPlanをLadybug WASMで実行しtyped raw rowsだけを返す |
| `@layerdraw/embedding-provider-wasm` | TS + model WASM | version固定Embedding Profileのvector生成だけを行う。SearchDocumentとranking semanticsは持たない |
| `@layerdraw/composer` | TS | GUI intentをsemantic operation / ViewRecipe operationへ変換する |
| `@layerdraw/render` | TS | ViewDataからlayout / RenderData / visual outputを生成する |
| `@layerdraw/export` | TS | ExportPlanとViewDataをbrowser / Node format profileでserializeする |
| `@layerdraw/viewer` | TS | framework-neutral readonly / streaming viewer |
| `@layerdraw/react` | TS | editor、Authoring Policy / grant-aware controls、Search / Analysis Workbench、Query Editor、viewer、inspector、workspace、review UI |
| `@layerdraw/library` | TS | Registry contentのbrowse / inspect / install UI |
| `@layerdraw/registry-client` | TS | Registry / local host / MCP transport facade。Registry semanticsは持たない |
| `@layerdraw/review` | TS | diff / Required Capability / review / comment / approve UI model |
| `@layerdraw/server-client` | TS | LayerDraw ServerのOrganization / Workspace / Project / Resource Access / AuthoringPolicy API client |
| `@layerdraw/client-sdk` | TS facade | Viewer SDK / Browser Editor SDK の公開surface |
| `@layerdraw/server-sdk` | TS facade | Node / Next.js / Mastraからremote serverまたはnative sidecarを利用するsurface |
| `@layerdraw/mcp-client` | TS | MCP Apps clientとhost clientのadapter |
| `@layerdraw/engine-wasm` | generated distribution | version固定済みEngine WASMとWorker bootstrap |
| `@layerdraw/native` | TS + platform artifacts | Server SDK用platform binary resolver |

Echo、Wails、React、VSCode API、Next.js / Mastra は上表のartifactを環境へ接続するframework shellであり、capability componentではない。

### 14.3 Feature Owner Mapping

| Feature IDs | Primary owner |
| --- | --- |
| F01-F02, F05-F06 | Workspace |
| F03-F04 | Runtime |
| F07-F09 | Engine / Workbench |
| F10-F11 | Engine package component + Host Adapters |
| F12 | External Format Adapter + Engine Workbench |
| F13 | Runtime + Host Adapters |
| F14 | Engine asset validation + Host Adapters + Presentation |
| F15-F17 | Engine / Workbench |
| F18-F19 | Presentation Composer + Engine validation |
| F20 | Engine Query / Analysis + Ladybug execution adapter。host-backed実行はRuntimeがrevision / Accessを固定 |
| F21 | Engine Search + Runtime / Access / Embedding Provider / Search Index + Ladybug execution adapter |
| F22 | Engine validation |
| F23 | Runtime |
| F24 | Engine Query / View Materializer |
| F25 | Presentation Render |
| F26 | Presentation Viewer |
| F27-F28 | Engine Query / View Materializer |
| F29 | Presentation Viewer |
| F30 | Engine Export Planner + Export Serialization |
| F31-F32 | Review |
| F33 | Access + Runtime + Engine package component |
| F34 | Presentation Viewer |
| F35-F38, F40-F42 | Runtime |
| F39 | Runtime + Access |
| F43, F45 | Access |
| F44 | Organization |
| F46 | Share |
| F47 | Runtime |
| F48 | Review |
| F49-F53 | Registry |
| F54-F55, F57 | MCP Host + Engine / Runtime |
| F56 | Access |
| F58-F59 | Organization / Workspace / Server application |
| F60 | Entitlement + Usage Meter + Access |
| F61 | Organization |
| F62 | Engine + Access + Runtime + Server Application + generated access / runtime / registry / server protocol contracts。各resourceのpublication ownerが強制し、Presentation / MCP / SDKはeffective grantを表示する |

AccessをServer applicationから論理分離する理由は、Desktop、VSCode、SDK、local MCP bridgeでもcredential scope、state field filtering、redaction、agent scopeが必要だからである。共有Project ACLを提供しない提供形態でもAccess component自体は利用できる。

Authoring Accessでは、Engineがbefore / afterからAuthoringImpactを生成し、Runtime / Registry / Server Applicationのversioned protocolがLDL外writeのHostOperationImpactを宣言し、AccessがActorとAuthoringPolicyから両impactに対するdecisionを作る。document / asset / package適用はRuntime、Host metadataはServer Applicationまたはlocal Host Applicationがpreview / commitで原子的に強制する。PolicyとgrantはHost metadataであり、`.ldl`、`.layerdraw`、`.ldpack`へ入れない。詳細は[authoring-access-control.md](authoring-access-control.md)に従う。

State-aware Queryでは、RuntimeがbackendとAccessを使って固定済み`StateQuerySnapshot`を構築し、Engineがpure inputとしてQuery / Viewを評価する。Engineはbackend locator、credential、provider SDKを受け取らない。

Project Searchでは、EngineがSearchDocument、filter、Hybrid fusion、SearchResultを所有し、Runtimeがrevision、Access projection、Search Index lifecycle、Embedding Providerをorchestrateする。Ladybug adapterはFTS / Vector / Algoを物理実行するだけであり、MCP HostとTS UIはrankingやanalysis semanticsを持たない。

Registry sourceの取得、signature / publisher policy、dependency resolution、install transaction planはGo Registry componentが所有する。archive safety、artifact manifest、LDL、resolved treeの規範検証はGo Engine package componentを呼ぶ。TS Library UIとclientはこれらを再実装しない。`layerdraw-registry`はheadless serviceであり、公式Registryとself-host Registryの両方に使う。

Entitlementは商用契約や請求の意味論を所有しない。external / static providerからEntitlementSnapshotを取得し、Accessがpermission / scopeと統合して最終可否を返す。Usage Meterは請求額を計算せず、scope付きUsageEventをUsageSinkへ配送する。

### 14.4 Delivery Package Mapping

この対応は次から機械的に導出する。

1. Feature x Delivery Matrix
2. Feature Owner Mapping
3. Shell / facade / adapter / service components required by each delivery channel

| Delivery channel | Go artifacts | TypeScript artifacts | Framework shell / service |
| --- | --- | --- | --- |
| LayerDraw SaaS | Engine + Runtime + native Ladybug / Search Index / Embedding adapters + Organization / Workspace application + native exporters linked in `layerdraw-server`、optional `layerdraw-registry` | `protocol`、`server-client`、`registry-client`、`composer`、`render`、`export`、`viewer`、`review`、`react`、`library` | React Web + Echo / WebSocket / MCP service |
| LayerDraw Self-host | SaaSと同じmulti-organization `layerdraw-server` closureと既定local Embedding Profile、optional bundled `layerdraw-registry` | SaaSと同じclient artifacts | React Web + Echo、container / native service |
| LayerDraw Desktop App | Engine + Runtime + native Ladybug / Search Index / local Embedding adapters + native exporters linked in Wails Go backend | `protocol`、`engine-client`、`registry-client`、`composer`、`render`、`export`、`viewer`、`review`、`react`、`library` | Wails native shell |
| LayerDraw VSCode Extension | bundled `layerdraw-host` sidecar + native Ladybug / Search Index / local Embedding adapters + native exporters | `protocol`、`engine-client`、`registry-client`、`composer`、`render`、`export`、`viewer`、`review`、`react`、`library` | platform-specific VSIX + VSCode API |
| LayerDraw SDK | variant別。ViewerはGo artifactなし、Browser EditorはEngine WASM、Serverはremote `layerdraw-server` / `layerdraw-host` / preview限定`layerdraw-engine` | Viewer: `protocol` / `render` / `export` / `viewer`。Browser Editor: それら + `engine-client` / `engine-wasm` / `client-sdk` / `composer`、local Search / Query / Analysis時は`query-adapter-ladybug-wasm`、local semantic Search時は`embedding-provider-wasm`。Server: `protocol` / `server-sdk` / `server-client` / optional `native`。Library capability add-on: `registry-client` / `library` | host application、browser Worker、またはNode / Next.js / Mastra host |
| LayerDraw MCP Apps | 接続先hostのEngine / Runtime / exporters | `protocol`、`mcp-client`、`registry-client`、`render`、`export`、`viewer`、`library`、optional `review` / `react` | MCP Apps client shell |
| LayerDraw Marketplace Integrations | Engine + Runtime + native Ladybug / Search Index / Embedding adapters + native exporters linked in marketplace backend | Web client artifacts + `server-client` | provider Web shell + LayerDraw Server backend |

Notes:

- MarketplaceはOAuth callback、token refresh、provider API proxy、entitlement、provider ACL mapping、auditのためserver-backedである。
- SDKは一つの提供形態であり、Viewer / Browser Editor / Serverは内部bundle variantである。提供形態一覧やFeature x Delivery Matrixへ別列として増やさない。
- Viewer variantはViewDataとoptional ExportPlanを入力とし、LDLを解釈しない。LDL入力と編集が必要ならBrowser Editor variantを使う。
- Server variantはTS製facadeである。portable compile / previewは`layerdraw-engine`、host-integrated storage / state / MCPは`layerdraw-host`、remote運用は`layerdraw-server`へ委譲する。
- MCP Appsは接続先hostのcapabilityに従い、独自Runtimeを持たない。
- Desktop、VSCode、SDK、MCP AppsはRegistry serviceを運営しない限り`layerdraw-registry`をbundleしない。Registry client経由でremote / local sourceを利用する。
- Library UIは全提供形態へ埋め込めるが、Registry / Engineが返すverification結果とinstall transactionを表示・実行するだけである。
- SDK package、native sidecar、WASM、Self-host artifactのlicense適用範囲は[legal/README.md](legal/README.md)、artifactへのlicense同梱と検証は[repository-governance-and-delivery.md](repository-governance-and-delivery.md) 10章を規範とする。managed serviceの商用条件はこのBlueprintで定義しない。

## 15. MCP

MCP Server は提供形態ではなく、各 host を AI agent から操作する protocol adapter である。

MCP transportは`layerdraw-host`、`layerdraw-server`、Desktop backendなどのhost composition rootへ組み込む。独立した`layerdraw-mcp` executableは規範構成に含めない。

分類:

```text
MCP Server
  protocol adapter
  tool/resource provider
  cross-cutting interface

MCP Apps
  client UI
  preview / interaction / approve surface
  delivery channel
```

MCP HostはGo Engine / Runtime protocolをtools / resourcesへ写すadapterであり、LDL parser、source rewrite、Query / View semanticsを持たない。MCP Appsはclient UIの提供形態であり、MCP Serverとは別分類である。

MCP toolsはhost capability awareにする。FTS / Vector / Hybrid Search、bounded subgraph inspection、Graph Analysis、局所取得、token budget、cursor、semantic operation、scoped LDL fragmentの規範は[search-query-and-analysis.md](search-query-and-analysis.md)と[ai-integration.md](ai-integration.md)に従う。

hostなしでclosed source treeを入力すれば可能:

- parse
- validate
- list / find / scoped read
- closed sourceと完全なlocal indexがある場合のProject Searchと明示subgraph analysis
- preview provided LDL source tree
- preview operations / scoped LDL fragment
- materialize ViewData from provided complete input
- 完全なViewData / ExportRecipe入力からExportPlanを生成する
- serializer、asset、fontがすべて揃う場合にnon-persistentなPlain View artifactをserializeする

host がある時だけ可能:

- open project
- save project
- list projects
- list views
- preview view
- apply operations and commit a revision
- list revisions
- restore revision
- import / export `.ldl` source tree or `.layerdraw` as Document I/O
- persist preview / export artifact to ArtifactStore

MCPは保存先を直接触らない。Go RuntimeのHost AdapterまたはLayerDraw Server API経由で操作する。

hostなしのoperation previewは永続`OperationBatch`ではない。provided source tree、source digest、semantic operationsを受け取り、Working Document、diagnostics、conflicts、preview definition hashだけを返すportable transformである。Host Document ID、revision、idempotency、audit、state updateを生成しない。`layerdraw.apply_operations`はHost Document IDでscopeされたruntimeがある場合だけ利用でき、LDL詳細仕様の`OperationBatch`をcommitする。

## 16. Domain Semantic Templates

LayerDraw は domain-specific semantic layer に転用できる。

業務領域ごとに Layer と EntityType / RelationType を固定した template として使う。

例:

```text
Layers:
  business
  metric
  semantic_model
  warehouse
  pipeline
  dashboard
  governance

EntityTypes:
  business_term
  metric
  dimension
  measure
  table
  column
  dbt_model
  pipeline
  dashboard
  owner
  policy

Relations:
  defines
  derives_from
  joins_to
  aggregates
  feeds
  used_in
  owned_by
  governed_by
  depends_on
```

AI は固定 schema の中でだけ操作する。

Domain-specific tools:

- addMetric
- updateMetricFormula
- linkMetricToColumn
- addLineage
- markColumnAsPII
- findJoinPath
- findDownstreamDashboards
- previewLineageView

裏では LayerDraw operations に変換する。

Host application integration では:

```text
Host application
  domain agents
  host DB / auth / tenancy

  layerdraw-host sidecar or layerdraw-server
  @layerdraw/protocol
  @layerdraw/server-sdk
  @layerdraw/react
  @layerdraw/mcp-client

  optional MCP endpoint
    -> host capability adapter
```

内部 agent には local tools として渡し、外部 agent client には MCP endpoint として公開する。

## 17. Registry / Packs / Templates

LayerDraw は全提供形態共有の registry system を持つ。
Registry systemはGo製Registry component / service、TS client、共有Library UIに分かれる。Registry componentはsource / signature / dependency / install transactionを所有し、archive / manifest / LDL / resolved treeの規範検証はGo Engine package componentを呼ぶ。
公式 registry と self-host registry は同じ `layerdraw-registry` service semantics から構成する。

Registry に登録する artifact は 2 種類に固定する。

| Registry item | Artifact | Description |
| --- | --- | --- |
| Pack | `.ldpack` | EntityType、RelationType、View、schema、manifest、dependency、checksum、optional assets / previews を含む pack container。asset が無い pack も `.ldpack` に包む。 |
| Template | `.layerdraw` | project skeleton、required packs、starter graph、views、state、assets、previews を含む template / project container。 |

`.ldl` は編集・Git管理・AI操作のための DSL であり、registry へ直接登録しない。

### 17.1 Registry Deployment Forms

| Form | Description |
| --- | --- |
| Official public registry | LayerDraw が運営する公開 `.ldpack` / `.layerdraw` registry。 |
| Organization private registry | 公式またはself-host registry上で、Organization membership / policyにより公開範囲を制御するprivate registry。 |
| Self-host registry | 利用者が自環境に立てる registry service。auth、storage、network、availability は利用者側 adapter / deployment が持つ。 |
| Local / development registry | pack / template 開発、検証、offline workflow 用の local registry source。 |

これらは F50 `Private / custom registry source` の source 種別であり、別機能として増やさない。

### 17.2 Registry System Boundary

Go Registry component owns:

- registry API、registry index / publishing metadata、artifact manifestのwire shapeは`schemas/`が所有する。Go Registry componentはRegistry behavior、Go Engineはartifact semantic validation / build behaviorを所有する
- `.ldpack` / `.layerdraw`のsource、identity、version、dependency resolver
- pack / template publishing workflow
- public / private / custom registry source resolution
- install / update / pin / remove semantics
- dependency / version migration diagnostics。LDL semantic migration diagnosticsはGo Engineから受け取る

`@layerdraw/library` owns:

- pack / template browse and search UI
- detail, version, source, and verification status UI
- install / update / repair / remove actions
- custom registry source selection UI
- create-from-template UI

`@layerdraw/library`は`@layerdraw/registry-client`をinjectionされ、Go Registry component / serviceを呼ぶ。artifact resolution、verification、installation semanticsを再定義しない。

`layerdraw-registry` owns:

- deployable registry service
- official registry API implementation
- self-host registry API implementation
- registry storage integration
- publishing API
- registry auth integration
- registry search / listing API

`layerdraw-registry` is headless. It provides the same service semantics for the official Registry and self-host Registry without embedding a product UI.

Registry component does not own:

- user / team management
- project access control
- entitlement / quota decision semantics
- storage provider implementation
- arbitrary code execution
- user-facing Library UI

`layerdraw-server` integrates the Registry client/component and can connect to `layerdraw-registry` in SaaS and self-host deployments.
Access decides whether a caller can access a registry source using Organization membership、registry policy、optional EntitlementSnapshot。
Host adapters supply storage and provider connections.

### 17.3 Library and Installation Workflow

`LayerDraw Library` is the user-facing surface for Registry content.
Its reusable implementation is `@layerdraw/library` and it is available from SaaS, Self-host, Desktop, VSCode, SDK, MCP Apps, and Marketplace Integrations.

Primary workflow:

1. Browse or search a Registry source in the Library.
2. Inspect the artifact identity, version, source, digest, signature status, dependencies, and compatibility diagnostics.
3. Explicitly install or update the artifact.
4. Registry clientが`.ldpack`または`.layerdraw` bytesを取得する。
5. Go Registry componentがsource identity、publisher / signature policy、dependencyを検証し、Go Engine package componentがchecksum、ZIP safety、artifact manifest、LDL、resolved treeを検証する。
6. Go Registry componentがrequired Packの導入transactionを生成し、Go Engine package componentが安全な展開planを返し、host adapterがprojectの`pack/<install-name>/` treeへ書く。
7. Go Engineが`layerdraw.resolved.json`をexact version、digest、source identity、local pathで生成する。
8. Go Engineでprojectを再検証し、migration / compatibility diagnosticsを返す。

Runtime rules:

- open、compile、query、render、export は remote Registry に接続せず、展開済み `pack/` tree と `layerdraw.resolved.json` から再現する。
- missing artifact、digest mismatch、破損を検出した場合は暗黙取得せず、diagnostic と明示的な repair action を提示する。
- update は暗黙実行しない。利用者または権限を持つ agent が Library workflow から明示的に開始する。
- Registry credential や token は `layerdraw.resolved.json`、`.ldl`、`.ldpack`、`.layerdraw` に保存しない。
- template から project を作る場合も、required pack を同じ手順で検証、実体化、固定する。

Pack / Template の通常導入に専用 CLI、`ldpm`、`ldp`、または独自 package manager を要求しない。
GUI を標準導線とし、将来 CI / administration 用の薄い補助 CLI を追加する場合も、Library と同じ Registry API と installation semantics を利用する。

### 17.4 Pack Registry

Pack は `.ldpack` artifact として registry に登録する。
EntityType、RelationType、projection / render hints、schema、dependency、optional asset の詰め合わせである。

例:

- AWS infrastructure pack
- Azure infrastructure pack
- Google Cloud pack
- Kubernetes pack
- Data platform pack
- AI agent memory pack
- Enterprise architecture pack

含めるもの:

- manifest
- pack.ldl
- EntityType
- recommended RelationType with projection / render hints
- columns
- optional icon / square image assets
- colors
- example snippets
- dependency constraints
- checksums
- optional signature

### 17.5 Template Registry

Template は `.layerdraw` artifact として registry に登録する作業開始点である。

含めるもの:

- manifest
- entry `document.ldl`
- project-local `schema/`、`layers/`、`views/`、`references/` source tree
- required packs
- project skeleton
- layers
- views
- starter entities
- starter relations
- optional state snapshot
- assets
- previews
- checksums
- optional signature
- validation rules
- Reference declarations for MCP and human guidance

例:

- coding agent memory
- AI agent shared memory
- microservice architecture
- AWS landing zone
- incident impact analysis
- data lineage
- analytics semantic layer

### 17.6 Registry Safety

Pack / Template は code execution を含めない。

許可:

- schema
- `.ldpack`
- `.layerdraw`
- `pack.ldl`
- entry `document.ldl`とproject-local `.ldl` source modules
- static assets
- metadata
- validation metadata

禁止:

- arbitrary JavaScript
- credential embedding
- hidden external callback

## 18. AI Agent Memory

LayerDraw は AI agent の長期記憶レイヤとして使える。

例:

```text
Entity:
  repository
  package
  module
  service
  api
  database
  decision
  constraint
  task
  owner
  incident

Relation:
  contains
  depends_on
  calls
  writes
  implements
  decided_by
  blocked_by
  affects
  owned_by
```

Views:

- repository runtime architecture
- unresolved tasks and blockers
- auth design decisions
- database migration impact
- next context for agent

AI agent は same memory graph を共有する。

```text
coding agent
desktop agent
IDE agent
CI bot
local MCP script
```

server-backed AI memoryで利用できるcapability:

- cross-device memory sync
- remote MCP endpoint
- private typed graphs
- revision history
- agent access tokens
- MCP Apps preview

## 19. Authentication and Authorization

Local modeは独自ユーザー管理を要求しない。LayerDraw Serverは外部IdP等からActorを解決し、Public Server metadataとしてOrganization membership、Team、Service Account、Workspace / Project ACLを管理する。

基本:

- local mode は sign-in 不要
- server-backed mode は Actor を解決する
- auth provider は差し替える
- storage provider の ACL を尊重する
- 1つのServer Instanceで複数Organizationを分離する

Actor:

- user
- agent
- service_account
- anonymous

Roles:

- instance_admin
- organization_owner / organization_admin / organization_member
- workspace_admin / workspace_member
- project owner / editor / viewer

Project access は server / host capability である。保存先ACLがある場合は保存先ACLとLayerDraw Access decisionの積集合を使う。Organization / Workspace所属はHost metadataであり、LDL definition identityへ含めない。

## 20. Implementation Dependency Map

### Go Engine Reorientation

- `.ldl` を正式正本にする
- registry pack は `.ldpack` に統一する
- registry template は `.layerdraw` に統一する
- `.layerdraw`内の`document.ldl`をentry moduleに統一し、entryから選択されるproject-local LDL source treeを正本definitionとする
- `.layerdraw` は project / template / export container として整理する
- View 定義を `.ldl` 正本として維持する
- LDL parser / validator / formatter / hash / Query / ViewData / package semanticsをGo Engineへ統合する
- 同一Go sourceからnative library、WASM、stdio sidecar、server-linked artifactをbuildする
- protocol schemaからGo / TypeScript bindingを生成する

### Editor / Viewer Split

- `packages/react`を`@layerdraw/react`として公開する
- readonly viewer を先に安定化する
- Web app から host-specific project hub を分離する

### Host Interfaces

- HostAdapter
- AssetResolver
- HistoryProvider
- RealtimeProvider
- RegistryProvider
- ServerHostAdapter
- LocalFileHostAdapter

### View System

- RelationType System を ViewData 実装の前提ゲートにする
- 規範済みView Conversion Contractを実装ゲートへ反映する
- 規範済み`Master Graph -> ViewData`のGo Engine typeとconformance testsを実装する
- 規範済み`ViewData -> RenderData`と`ViewData + ExportRecipe -> ExportPlan -> ExportArtifact`のtypeとtestsを実装する
- LDL詳細仕様7.9節のplain export capability matrixを実装する
- View Composer 強化
- query-backed View
- no-code query builder の保存
- table projection
- matrix / tree / flow / context projection
- CSV / XLSX export
- JSON / Markdown / HTML / PDF plain export contract
- production / disaster-recovery environment template

### Document Generation

- ViewData から design document / approval report / review pack を生成する
- PDF / PPTX / DOCX templates
- multiple ViewData inputs
- narrative section templates
- approval / review metadata
- source refs appendix

### Server Foundation

- multi-organization Instance administration
- Organization membership / Team / Service Account
- Workspace / Project directory
- Organization-scoped metadata / ACL / audit / search
- EntitlementProvider / UsageSink ports
- `.ldl` revision store
- artifact store
- storage adapter interface
- Time Machine API
- realtime API
- remote MCP endpoint
- `layerdraw-server`
- `layerdraw-registry`
- `@layerdraw/server-client`
- `@layerdraw/server-sdk`

### MCP and MCP Apps

- Go MCP Host adapter
- `@layerdraw/mcp-client`
- scoped LDL read、SemanticIndex、token budget、cursor
- semantic operation、scoped LDL fragment、source patch fallback
- MCP Apps readonly preview
- desktop agent extension packaging
- host product integration

### Delivery Channels

- VSCode extension
- Open VSX
- Wails desktop
- Viewer SDK / Browser Editor SDK / Server SDK
- Google Drive integration
- Microsoft / SharePoint integration

### Registry / Marketplace

- official packs
- template registry
- private registry
- AWS / Azure / GCP packs
- AI agent memory templates
- analytics semantic layer template

## 21. Non-goals

- draw.io XML compatibility as a core requirement
- free-form whiteboard
- Markdown knowledge base replacement
- pixel-perfect manual layout as source of truth
- storing full Time Machine history inside `.layerdraw`
- making Echo or any server framework part of the core
- forcing all delivery channels to support realtime/history
- executing arbitrary code from packs/templates
- treating valid-time / transaction-time、point-in-time query、time-window traversal、temporal relation / cardinality、or bitemporal history as Master Graph semantics
- implementing managed-service commercial or distribution features in the Public Core

## 22. One-line Positioning

```text
LayerDraw is a typed graph workspace for architecture documents and AI agent memory.
```

Alternative:

```text
Architecture diagrams for humans. Structured memory for AI agents.
```

Developer-facing:

```text
Git and AI friendly architecture diagrams powered by .ldl.
```
