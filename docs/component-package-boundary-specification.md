# Component / Package Boundary 詳細仕様

## 1. 目的と規範範囲

この文書は、LayerDrawを構成するcapability componentを、Go package、TypeScript package、generated artifact、binary、framework shell、delivery bundleへ割り当てる規範詳細仕様である。

この文書が固定するもの:

- packageの正式な責務と非責務
- package間の許可依存と禁止依存
- Go / TypeScript / generated codeの境界
- libraryとdeployable binaryの境界
- binaryがlinkするpackage closure
- delivery bundleが同梱するartifact closure
- packageとprotocolのversioning規則
- 依存規則を機械検証するための条件

この文書はLDLの構文・意味、Engine Protocolの個別field、Runtime port、状態遷移、storage provider固有実装、repository governance / CI / distributionを再定義しない。それぞれ[ldl-language-specification.md](ldl-language-specification.md)、[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)、[architecture.md](architecture.md)、[compiler-architecture.md](compiler-architecture.md)、[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)、[repository-governance-and-delivery.md](repository-governance-and-delivery.md)を参照する。

## 2. 用語

| 用語 | 定義 |
| --- | --- |
| Capability component | 機能の意味、不変条件、decisionを所有する論理責務 |
| Go package | 単一Go module内のcompile単位 |
| TS package | pnpm workspaceおよびnpm distribution単位 |
| Generated package | machine-readable schemaから生成され、手編集を禁止するpackage |
| Distribution artifact | `.wasm`、native binary、platform packageなど、source packageから生成する配布物 |
| Executable | 複数packageをcomposition rootでlinkした実行可能artifact |
| Framework shell | Echo、Wails、React、VSCode、Next.jsなど、artifactを実行環境へ接続する薄い層 |
| Delivery bundle | ユーザーへ渡すSaaS、Self-host、Desktop、VSCode、SDK、MCP Apps、Marketplaceの完成物 |
| Registry artifact | `.ldpack`または`.layerdraw`。software packageとは別物 |

本文で`A -> B`は「AがBをimportする」を意味する。

## 3. 全体決定

### 3.1 Monorepo

Go source、schema、generated binding、TS package、application shellは同じpublic product monorepoで管理する。repository分割をpackage境界の代わりにしない。private SaaS operations、Registry content、distribution-only repositoryとの境界は[repository-governance-and-delivery.md](repository-governance-and-delivery.md)を規範とする。

### 3.2 Goは単一module

Goはrepository rootの単一module `github.com/layerdraw/layerdraw`として管理する。

理由:

- Engine、Runtime、Access、Registry client、binaryを同じrelease sourceへ固定する
- `replace`、cross-module pseudo-version、nested `go.mod`によるversion driftを発生させない
- Goの`internal`規則とimport linterで境界を強制する
- Wails、Server、sidecarが同じpackageを直接linkできる

Engine、Runtime、Accessは別Go moduleではなく、同一module内の独立したinternal packageである。外部利用者向けGo SDKは現要件に含めない。外部hostとの安定境界はEngine / Runtime ProtocolおよびTS SDKである。

### 3.3 TypeScriptは公開package単位

TypeScriptは用途ごとにworkspace packageを分ける。各packageは明示したpublic entrypointだけを公開し、他packageの`src/`やinternal subpathへのdeep importを禁止する。

### 3.4 Schemaは言語中立の正本

Go structまたはTypeScript interfaceをprotocol schemaの正本にしない。`schemas/`のmachine-readable schemaとconformance fixtureを正本とし、Go / TypeScript bindingを生成する。

### 3.5 Binaryはcomposition root

binaryとframework shellはpackageをlink、configure、exposeするだけである。Compiler、Query、View、Access decision、Registry resolutionをbinaryのhandlerへ再実装しない。

## 4. Target Repository Layout

repository全体の正準topologyは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)を規範とする。以下はその中でcomponent / package boundaryに関係するdirectoryを詳細化したものである。

```text
/
  .changeset/
  .github/
  docs/
  rfcs/

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
  fixtures/

gen/
  go/
    semantic/
    protocolcommon/
    accessprotocol/
    engineprotocol/
    runtimeprotocol/
    serverapplicationprotocol/
    realtimeprotocol/
    queryadapterprotocol/
    registryprotocol/
    container/
    release/

internal/
  engine/
    engine.go
    internal/
      compiler/
      workbench/
      query/
      view/
      exportplan/
      container/
      index/
  runtime/
    runtime.go
    port/
  access/
  registry/
  exporter/
  mcphost/
  application/
    organization/
    workspace/
    review/
    share/
    entitlement/
    usage/
    registryservice/
    server/
  adapter/
    query/
      ladybug/
    filesystem/
    objectstorage/
    database/
    externaldrive/
    repository/
    auth/
    realtime/
  transport/
    stdio/
    http/
    websocket/
    wasm/
    wails/

cmd/
  layerdraw-engine/
  layerdraw-host/
  layerdraw-server/
  layerdraw-registry/

apps/
  web/
  desktop/
  mcp-app/

integrations/
  marketplace/

extensions/
  vscode/

packages/
  protocol/
  engine-client/
  query-adapter-ladybug-wasm/
  embedding-provider-wasm/
  render/
  export/
  viewer/
  composer/
  library/
  registry-client/
  react/
  review/
  server-client/
  client-sdk/
  server-sdk/
  mcp-client/
  engine-wasm/
  native/

deploy/
  self-host/

tests/
  conformance/
  integration/
  e2e/
  packaged/

tools/

go.mod
package.json
pnpm-workspace.yaml
turbo.json
Makefile
README.md
LICENSE
NOTICE
CONTRIBUTING.md
CODE_OF_CONDUCT.md
SECURITY.md
SUPPORT.md
docs/
  legal/
    README.md
    use-cases.md
    trademarks.md
    contributor-license-agreement.md
    contributor-privacy-notice.md
    licenses/
      Apache-2.0.txt
OWNERS.yaml
```

`internal/engine/internal/*`は`internal/engine` facadeの実装詳細であり、Runtime、MCP、binaryから直接importしない。

## 5. Generated Schema Packages

### 5.1 Schema groups

| Schema group | Owns | Must not own |
| --- | --- | --- |
| `semantic` | Diagnostic、StableAddress、NormalizedDocument、AuthoringImpact、SearchResult、QueryResult、AnalysisResult、ViewData、ExportPlan、StateQuerySnapshotのcanonical wire型 | transport、Actor / policy、storage locator、framework型 |
| `protocol-common` | request / response envelope、ProtocolFailure、PageInfo、BlobRef、handshake、CapabilityManifestの共通primitive | Engine / Runtime / Registry固有operation |
| `access-protocol` | AuthoringCapability、HostOperationImpact、AuthoringPolicy、GrantSnapshot / Summary、AuthoringDecisionのtransport非依存wire型 | LDL意味分類、Policy永続化、Actor解決、decision実装 |
| `engine-protocol` | Compiler、Workbench、Search、Query、Analysis、View、Export、Package operation payload | Runtime commit、Actor session、共通envelope |
| `runtime-protocol` | Document session、revision、operation commit、AuthoringProofとAccess decisionの適用binding、state、search index / embedding orchestration、history、Runtime固有capability | LDL構文・意味、AuthoringImpact分類、Policy判定、Realtime event wire |
| `server-application-protocol` | Instance、Organization、membership、Team、Service Account、Workspace、Project directory、ACL、AuthoringPolicyのbinding / lifecycle、Share、Audit、Entitlement / UsageのServer API | LDL semantics、AuthoringPolicy wire型、managed-service commercial / distribution semantics |
| `realtime-protocol` | room、participant、presence、working change、commit event、resume cursor | Engine semantics、durable revision storage |
| `query-adapter-protocol` | Query / Search / Analysis ExecutionPlan、SearchIndexPlan、TypedRawRows、token binding、physical primitive capabilityを公式 / third-party adapterへ提供する低レベルSPI | recipe / search / analysis semantics、embedding生成、manual plan authoring、domain result normalization |
| `registry-protocol` | Registry source、artifact identity、publisher、signature、dependency、install transaction | artifact内部LDL semantics |
| `container` | `.layerdraw` / `.ldpack` manifest、digest、Source Manifest | Registry listing、managed-service commerce |
| `release` | release manifest、artifact digest、protocol range、schema digest、profile metadata | domain semantics、managed-service commerce |

### 5.2 Generated Go packages

`gen/go/*`はschemaから生成し、手編集を禁止する。Generated packageはhandwritten application packageをimportしてはならない。

### 5.3 Generated TypeScript package

`@layerdraw/protocol`は以下のsubpathを公開する。

```text
@layerdraw/protocol/semantic
@layerdraw/protocol/common
@layerdraw/protocol/access
@layerdraw/protocol/engine
@layerdraw/protocol/runtime
@layerdraw/protocol/server-application
@layerdraw/protocol/realtime
@layerdraw/protocol/query-adapter
@layerdraw/protocol/registry
@layerdraw/protocol/container
@layerdraw/protocol/release
```

root entrypointから全型を一括exportしない。利用packageは必要なschema groupだけをimportする。

## 6. Go Package Boundaries

### 6.1 `internal/engine`

Owns:

- Compiler facade
- Workbench facade
- Query prepare / complete
- SearchDocument、Search prepare / complete、Hybrid fusion
- Graph Analysis prepare / complete
- AuthoringImpact classification
- ViewData materialization
- ExportPlan generation
- package validation / build
- SemanticIndex、StableAddress、hash、diagnostics

Inputs:

- closed source tree
- verified installed Pack tree
- typed arguments
- optional Access適用済みStateQuerySnapshot
- Runtimeが固定したSearch Index identityとEmbedding Provider出力

Outputs:

- generated semantic型
- canonical source edits
- diagnostics
- opaque process-local handle

May import:

- `gen/go/semantic`
- `gen/go/container`
- `internal/engine/internal/*`
- Ladybug execution portの抽象型
- 標準libraryと明示許可されたpure dependency

Must not import:

- `internal/runtime`
- `internal/access`
- `internal/registry`
- `internal/application/*`
- `internal/adapter/*`
- Echo、Wails、MCP SDK、provider SDK
- filesystem / network credential resolver
- concrete embedding model / provider SDK

Ladybug execution portはEngine root facadeがconsumer-owned interfaceとして定義する。native execution adapterはこのinterfaceを実装し、Engineからadapter packageをimportしない。browser executionでは同じport operationをEngine Protocolのprepare / completeへmapする。

### 6.2 `internal/runtime`

Owns:

- Host Document session
- Working Document / Committed Revision
- open / save / autosave / recovery
- operation commit、lease、idempotency、conflict
- BackendBinding resolution
- StateQuerySnapshot construction
- Access適用済みSearch Index lifecycle、Embedding Provider orchestration
- AuthoringGrantSnapshot固定、AuthoringDecision適用、preview / commit再評価
- owner protocolが宣言したHostOperationImpactとEngine AuthoringImpactの原子的な合成・強制
- history、audit、reconcile、realtime orchestration

May import:

- `internal/engine`
- `internal/access`
- `internal/runtime/port`
- `gen/go/semantic`
- `gen/go/accessprotocol`
- `gen/go/runtimeprotocol`

Must not import:

- concrete storage / auth / provider adapter
- Echo、Wails、VSCode、React
- Engine internal subpackage
- Registry service implementation
- concrete embedding / search index adapter

`internal/runtime/port`はRuntimeが必要とするconsumer-owned interfaceを定義する。Runtime本体はadapter packageをimportしない。

### 6.3 `internal/access`

Owns:

- Actor、role、shared ACL、credential scope、agent scopeの評価
- Organization / Workspace / Project role、entitlement / quota inputを含むallow / deny / redact decision
- state field access decision
- AuthoringPolicy階層merge、AuthoringGrantSnapshot、AuthoringImpactとHostOperationImpactに対するallow / deny / approval decision

May import:

- `gen/go/semantic`のidentity / field path / AuthoringImpact型
- `gen/go/accessprotocol`のAuthoringPolicy / Grant / HostOperationImpact / Decision型
- 標準library

Must not import:

- Runtime
- managed-service commercial implementation
- storage / credential provider
- state value
- framework
- LDL parser / Engine internal。subject分類はAuthoringImpact、非Definition操作はowner protocolのHostOperationImpactを使う

Entitlement applicationがproviderから取得したimmutable EntitlementSnapshotをneutral inputとして受け取る。AccessからEntitlementProviderや商用契約システムを呼ばない。

### 6.4 `internal/registry`

Owns:

- Registry source resolution
- publisher / signature policy
- dependency graph resolution
- install / update / pin / remove transaction
- repair plan

May import:

- `internal/engine`のpackage validation facade
- `gen/go/registryprotocol`
- `gen/go/container`
- Registry source port

Must not import:

- Runtime session
- user / team management
- managed-service commercial implementation
- concrete Registry storage adapter
- Library UI

Registryはartifact bytesを取得・解決するが、ZIP safety、artifact manifest、LDL、StableAddress、resolved treeの意味検証を再実装しない。Engine package facadeへ委譲する。

### 6.5 `internal/exporter`

Owns:

- ExportPlanのnative serializer profile
- artifact bytes生成
- Source Manifestへのcompleted digest binding
- serializer capability reporting

May import:

- `gen/go/semantic`のViewData / ExportPlan
- format固有library

Must not import:

- Compiler / Workbench internal
- Runtime / storage
- Query / View semantic rules

ExporterはExportPlanに存在しないsheet、page、section、source mappingを推測しない。

### 6.6 `internal/mcphost`

Owns:

- Engine / Runtime capabilityのMCP tools / resourcesへのmapping
- capability discovery
- MCP pagination / attachment / streaming adapter
- Search / inspect / Graph Analysis tool mapping
- AuthoringGrantSummary、Required Capability、proposal / approval outcomeのtool mapping

May import:

- Engine facade
- Runtime facade
- Access decision facade
- generated protocol型
- MCP transport SDK

Must not import:

- Engine internal subpackage
- concrete storage provider
- 独自LDL parser / source writer
- 独自Query / View normalization
- 独自Search ranking / Hybrid fusion / Analysis normalization
- 独自AuthoringImpact分類またはAccess decision

### 6.7 Application components

| Package | Owns | Depends on |
| --- | --- | --- |
| `internal/application/organization` | Instance内Organization、membership、Team、Service Account、Organization / Authoring policy、storage / registry binding | Access、organization / identity / policy ports |
| `internal/application/workspace` | Organization-scoped workspace / project metadata、recent / pinned、settings、HostOperationImpact付きconditional publication | Runtime facade、Access、workspace ports、generated access / server protocol |
| `internal/application/review` | review session、comment / annotation、AuthoringImpact付きproposal、approve / apply orchestration | Engine semantic diff / AuthoringImpact、Access approver decision、Runtime commit、review ports |
| `internal/application/share` | share intent、invite / link / provider handoff | Access decision、share provider port |
| `internal/application/entitlement` | external / static EntitlementSnapshot取得、quota state | entitlement provider port。商用契約や請求の意味論は持たない |
| `internal/application/usage` | Organization / Workspace / Project scope付きUsageEvent生成と配送 | usage sink port。請求額計算は持たない |
| `internal/application/registryservice` | publish、listing、search、registry auth / storage orchestration | Registry、Engine package facade、registry service ports |
| `internal/application/server` | server-backed capabilityのcomposition facade |上記application component、Runtime、Registry、MCP Host |

Application component同士を直接循環参照しない。`server` composition facadeがdecisionと結果を受け渡す。

### 6.8 Adapters

`internal/adapter/*`はconsumer-owned portを実装する。

Rules:

- adapterは必要なport packageとprovider SDKだけをimportする
- adapterからEngine internalをimportしない
- provider objectをcanonical semantic型として外へ漏らさない
- provider ACLをAccess decisionへ変換する前のinputとして返す
- provider固有errorを共通error categoryとdiagnostic attachmentへmapする
- dynamic Go plugin ABIを採用しない。binaryが必要なadapterをcompile-time compositionする

### 6.9 Transports

`internal/transport/*`はgenerated protocolとfacadeを接続する。

| Transport | May call | Must not do |
| --- | --- | --- |
| stdio | Engine / Runtime / local host facade | semantic normalization、storage実装 |
| HTTP | Server application facade | handler内validation / Query生成 |
| WebSocket | Runtime realtime facade | commit規則の変更 |
| WASM | Engine facade、browser callback port | TS向け意味論fork |
| Wails | Desktop composition facade | Wails method内source rewrite |

## 7. Go Dependency DAG

`A -> B`をimportとして、許可する上位DAGは次である。

```text
cmd/*
  -> framework shells / transport
  -> application composition
  -> adapters

framework shells / transport
  -> application facades | Runtime facade | Engine facade
  -> generated protocol

application/*
  -> Runtime facade
  -> Access
  -> Registry
  -> Exporter
  -> Engine facade
  -> consumer-owned ports

MCP Host
  -> Runtime facade | Engine facade | Access

Runtime
  -> Engine facade | Access | Runtime ports | generated semantic/runtime types

Registry
  -> Engine package facade | Registry ports | generated registry/container types

Exporter
  -> generated semantic types

Engine
  -> generated semantic/container types | engine internal packages

Access
  -> generated semantic identity types

generated packages
  -> no handwritten LayerDraw package
```

禁止するedge:

```text
Engine -> Runtime / Access / Registry / Adapter / Framework
Runtime -> Adapter / Framework / Engine internal
Access -> Runtime / Entitlement Provider / Usage Sink / Storage
Registry -> Runtime / Library UI
Exporter -> Runtime / Query / View implementation
Generated -> Handwritten package
Application component -> Framework shell
```

## 8. TypeScript Package Boundaries

### 8.1 Package responsibilities

| Package | Owns | Direct dependencies |
| --- | --- | --- |
| `@layerdraw/protocol` | generated wire型とcodec | runtime dependencyなし |
| `@layerdraw/engine-client` | Engine transport facade、handle lifecycle、cancellation | `protocol` |
| `@layerdraw/query-adapter-ladybug-wasm` | Query / Search / Analysis planとSearchIndexPlanをLadybug WASMで実行しtyped raw rowsだけを返す | `protocol/query-adapter`、Ladybug WASM distribution |
| `@layerdraw/embedding-provider-wasm` | version固定Embedding Profileによるdocument / query embedding実行 | `protocol/runtime`、profile固定model distribution。LDL / SearchDocument生成は含まない |
| `@layerdraw/server-client` | Organization / Workspace / Project / Resource Access / AuthoringPolicy / Share / Audit Server HTTP / WebSocket client | `protocol/access`、`protocol/server-application`、`protocol/runtime`、`protocol/realtime` |
| `@layerdraw/composer` | UI intentからoperation requestを構築し、Engineが返したAuthoringImpact / grant summaryを表示用に保持する | `protocol/semantic`、`protocol/access`、`protocol/engine`。impact分類は持たない |
| `@layerdraw/render` | ViewDataからlayout / RenderData / visual output | `protocol/semantic` |
| `@layerdraw/export` | ExportPlan / ViewDataのbrowser / Node serializer | `protocol/semantic` |
| `@layerdraw/viewer` | readonly / streaming viewer state | `protocol/semantic`、`render` |
| `@layerdraw/registry-client` | Registry / host transport facade。resolution / validation semanticsは持たない | `protocol/registry`、MCP adapterだけoptional `mcp-client` |
| `@layerdraw/library` | Registry content UI、install transaction UI | `registry-client`、React peer |
| `@layerdraw/review` | diff / Required Capability / review / comment / approve UI model | `protocol/semantic`、`protocol/access`、`protocol/runtime` |
| `@layerdraw/react` | Editor、Authoring Policy / grant-aware controls、Search Workbench、Query Editor、Analysis UI、Viewer、Inspector、Workspace UI | `composer`、`viewer`、`review`、`library`、React peer |
| `@layerdraw/mcp-client` | MCP Apps / MCP client transport adapter | `protocol` |
| `@layerdraw/client-sdk` | Viewer / Browser Editor facade | `engine-client`、local Search / Query / Analysis時は`query-adapter-ladybug-wasm`、local semantic Search時は`embedding-provider-wasm`、`composer`、`render`、`export`、`viewer` |
| `@layerdraw/server-sdk` | Node / Next.js / Mastra facade、sidecar lifecycle | `engine-client`、`server-client`、`protocol`、optional `native` |
| `@layerdraw/engine-wasm` | version固定済みWASM bytesとWorker bootstrap | generated distribution artifact |
| `@layerdraw/native` | platform binary resolver、explicit binary path検証 | platform artifact packages |

### 8.2 `@layerdraw/engine-client` transport entrypoints

```text
@layerdraw/engine-client
@layerdraw/engine-client/wasm
@layerdraw/engine-client/stdio
@layerdraw/engine-client/http
@layerdraw/engine-client/wails
```

- rootはtransport-neutral client contractだけをexportする
- browser entrypointからNode built-inをimportしない
- stdio entrypointをbrowser bundleへ混入させない
- Wails entrypointはgenerated Wails bindingをtransport-neutral interfaceへmapする

`@layerdraw/engine-client/http`はCompiler、Workbench preview、Query、View、ExportPlanなどEngine Protocol operationだけを扱う。`@layerdraw/server-client`はsession、workspace、project、commit、history、sharing、realtimeなどLayerDraw Server application APIを扱う。両者は同じHTTP libraryを共有してよいが、互いをimportせず、API surfaceを重複定義しない。

Registry transportは別の`@layerdraw/registry-client`が所有する。

```text
@layerdraw/registry-client
@layerdraw/registry-client/http
@layerdraw/registry-client/host
@layerdraw/registry-client/mcp
```

rootは`RegistryClient` interfaceだけを公開する。HTTP、local host、MCP adapterはGo Registry componentが返すtyped resultをtransportするだけで、dependency resolution、signature decision、install transactionを再実装しない。`@layerdraw/library`はRegistryClientをprops / contextで受け取る。

`@layerdraw/server-client`は次のentrypointを公開する。

```text
@layerdraw/server-client
@layerdraw/server-client/http
@layerdraw/server-client/realtime
```

rootはtransport-neutralなLayerDraw Server client contract、`/http`はrequest transport、`/realtime`はroom / event transportを提供する。Realtime conflictやOperationResultをclient独自型へ再定義しない。

### 8.3 React境界

- Reactは`@layerdraw/react`と`@layerdraw/library`のpeer dependencyとする
- protocol、engine-client、composer、render、export、viewerはReactへ依存しない
- UI componentはEngine clientをglobal singletonから取得せず、props / contextでinjectionする
- UI componentはLDL sourceを直接rewriteしない
- UI componentはsubject kindからAuthoring Capabilityを推測せず、HostのAuthoringGrantSummaryとEngine preview resultだけを使う

### 8.4 SDK境界

LayerDraw SDKは一つの提供形態であり、package compositionに3variantを持つ。

| Variant | Required packages | Engine / Runtime |
| --- | --- | --- |
| Viewer | `protocol`、`render`、`export`、`viewer` | Engineなし。hostがViewData / ExportPlanを供給 |
| Browser Editor | Viewer一式、`engine-client/wasm`、`engine-wasm`、`composer`、`client-sdk`、local Search / Query / Analysis時は`query-adapter-ladybug-wasm`、local semantic Search時は`embedding-provider-wasm`、optional `react` | Go Engine WASM。永続Runtimeはhost injectionまたはremote host |
| Server | `server-sdk`、`server-client`、`engine-client/stdio`、`native` | portable=`layerdraw-engine`、local host=`layerdraw-host`、remote=`layerdraw-server` |

Viewer variantが`.ldl`を受け取ってparseしてはならない。

Browser EditorのGo Engine WASMはAuthoringImpactを生成できるがActor / Policy decisionを所有しない。fixed-schemaをauthoritativeに強制するSDK構成は`layerdraw-host`、`layerdraw-server`、または同契約のtrusted Runtime endpointを必須とする。local-only callbackでのcontrol非表示をsecurity boundaryとして扱わない。

F49-F53のLibrary capabilityはSDK family全体の明示add-onとして`@layerdraw/registry-client` + `@layerdraw/library`を提供する。各variantのminimum runtimeへ強制混入させない。install / update / create-from-template actionはEngine / Runtimeを持つhost capabilityを要求し、Viewer-only hostでは利用不能理由を表示する。

### 8.5 Native npm distribution

`@layerdraw/native`はsemantic implementationを持たない。platform / architectureに対応するbinary artifactを解決するだけである。

```text
@layerdraw/native
@layerdraw/native-<os>-<arch>[-<libc>]
```

Rules:

- platform packageはbinaryとdigest metadataだけを含む
- platform packageは対応targetの`layerdraw-engine`と`layerdraw-host`を含み、resolverがkindを指定して選ぶ
- install scriptで任意URLからbinaryを取得しない
- Server SDKはcaller指定`binary_path`を最優先できる
- binary digestとrelease versionを起動前に検証する
- remote modeはnative packageを要求しない

## 9. TypeScript Dependency DAG

```text
applications / framework shells
  -> react | client-sdk | server-sdk | mcp-client | server-client | registry-client | library

react
  -> composer | viewer | review | library | protocol

client-sdk
  -> engine-client | optional query-adapter-ladybug-wasm | optional embedding-provider-wasm | composer | render | export | viewer | protocol

server-sdk
  -> engine-client | server-client | native | protocol

viewer
  -> render | protocol/semantic

composer
  -> protocol/semantic | protocol/engine

query-adapter-ladybug-wasm | embedding-provider-wasm | render | export | review | mcp-client | server-client | engine-client
  -> required protocol subpaths

library
  -> registry-client | React peer

registry-client
  -> protocol/registry | optional mcp-client

protocol
  -> no handwritten LayerDraw package
```

禁止するedge:

```text
protocol -> any handwritten TS package
render / viewer / composer -> React
viewer -> engine-client
engine-client -> composer / render / viewer / React
query-adapter-ladybug-wasm -> engine semantics / composer / render / React
embedding-provider-wasm -> SearchDocument generation / Hybrid ranking / React
server-client -> React
registry-client -> library / Registry semantic implementation
client-sdk -> server-sdk
server-sdk -> React UI
browser entrypoint -> Node stdio / filesystem package
```

## 10. Executable Composition

### 10.1 `layerdraw-engine`

```text
layerdraw-engine
  Engine
  generated Engine Protocol
  stdio transport
  Ladybug native execution adapter
```

Does not include:

- Runtime
- Access
- storage / state backend
- Registry client
- history / realtime
- Echo / Wails

用途はportable compile、validate、preview、Structural Query、明示subgraph Analysis、View / ExportPlan生成である。Project Searchはcallerが完全なSearch Index identityと、semantic / hybrid modeでは互換するquery embeddingを供給した時だけ実行できる。revision、Access、Embedding Provider、durable index lifecycleを持つ通常hostは`layerdraw-host`または`layerdraw-server`を使う。

### 10.2 `layerdraw-host`

```text
layerdraw-host
  Engine
  Runtime
  Access
  Authoring Policy evaluator / Runtime enforcement
  Ladybug native Query / Search / Analysis adapter
  Search Index adapter
  configured local / remote Embedding Provider
  Registry client
  Review application component
  Native exporters
  MCP Host
  configured local / external adapters
  stdio transport
```

Does not include:

- Workspace / organization management
- user / team management
- Entitlement / Usage application
- provider Web UI
- Echo server shell

VSCode、local MCP、Server SDK sidecar modeで利用する。

### 10.3 `layerdraw-server`

```text
layerdraw-server
  Engine
  Runtime
  Access
  Authoring Policy evaluator / Runtime enforcement
  Ladybug native Query / Search / Analysis adapter
  Search Index adapter
  configured local / remote Embedding Provider
  Registry client
  Native exporters
  MCP Host
  Organization / Workspace / Review / Share / Entitlement / Usage application components
  configured adapters
  Server application facade
  Echo HTTP / WebSocket shell
```

React static assetsをbinaryへembedするかCDN / sidecar containerで配信するかはdeployment選択であり、Go package DAGを変えない。

### 10.4 `layerdraw-registry`

```text
layerdraw-registry
  Registry service application (`internal/application/registryservice`)
  Registry client / resolver
  Engine package validation facade
  Registry auth / storage adapters
  HTTP shell
```

Editor、Runtime、Viewer、Library UIを含めない。

### 10.5 Desktop Wails backend

```text
Wails backend
  Engine
  Runtime
  Access
  optional host-backed Authoring Policy evaluator / Runtime enforcement
  Ladybug native Query / Search / Analysis adapter
  Search Index adapter
  configured local / remote Embedding Provider
  Registry client
  Review
  Native exporters
  MCP Host
  desktop adapters
  Wails binding shell
```

Wails methodはgenerated protocol request / responseへmapするだけである。React frontend assetsはWails application bundleへembedする。

## 11. Framework Shell Boundaries

| Shell | Owns | Must not own |
| --- | --- | --- |
| Echo | route、middleware、session extraction、HTTP / WS framing、static asset delivery | LDL validation、Query、commit、Access decision |
| Wails | window lifecycle、native dialog、binding、asset embed | source rewrite、state compatibility |
| React | UI state、interaction、component composition | canonical model、hash、commit |
| VSCode | command、custom editor、workspace event、webview、binary lifecycle | parser、formatter、Runtime semantics |
| Next.js / Mastra | route / agent integration、host auth、Server SDK lifecycle | LDL / Query / View semantics |
| Provider marketplace shell | OAuth callback、provider launch context、file picker handoff | provider別semantic fork |

Framework shellから別framework shellをimportしない。共有処理はframework-neutral packageへ置く。

## 12. Delivery Bundle Closure

Binaryへcodeがlinkされていることと、delivery channelが機能を提供することは同義ではない。各composition rootは起動時に`CapabilityManifest`を生成し、wired port、policy、provider、entitlement、delivery profileから有効機能を列挙する。

Rules:

- Feature x Delivery Matrixが`-`の機能はroute、MCP tool、UI actionとして公開しない
- packageがbinaryへlinkされていても、必要port / policyが未構成ならcapabilityをdisabledにする
- clientはpackage存在やbinary名から機能を推測せず、CapabilityManifestを読む
- 同じ`layerdraw-server`binaryをSaaS / Self-host / Marketplaceで使っても、adapter / policyによりauth、storage、provider sharing等の接続先を変えられる。Organization / Workspace modelはServer型で共通とする
- capability profileは意味論を変更せず、利用可能operationの集合だけを制限する

### 12.1 SaaS

```text
Backend deployment
  layerdraw-server
  configured managed adapters
  optional connection to layerdraw-registry

Web deployment
  @layerdraw/protocol
  @layerdraw/server-client
  @layerdraw/registry-client/http
  @layerdraw/composer
  @layerdraw/render
  @layerdraw/export
  @layerdraw/viewer
  @layerdraw/review
  @layerdraw/react
  @layerdraw/library
```

通常commitはHTTP / WebSocketでServerへ送る。optionalなclient previewにEngine WASMを追加しても、Server commit validationを省略しない。

### 12.2 Self-host

SaaSと同じmulti-organization package closureを使う。差分はadapter configuration、auth provider、deployment packagingだけである。single container、複数container、native binaryのいずれでも意味論は変わらない。単一組織利用ではdefault Organization / Workspaceをbootstrapする。

### 12.3 Desktop

```text
Wails application
  Go backend
    Engine / Runtime / Access / Registry / Review / Exporter / MCP Host
    local / external storage adapters
    Wails binding shell
  React frontend
    @layerdraw/protocol
    @layerdraw/engine-client/wails
    @layerdraw/registry-client/host
    @layerdraw/composer
    @layerdraw/render
    @layerdraw/export
    @layerdraw/viewer
    @layerdraw/review
    @layerdraw/library
    @layerdraw/react
```

独立した`layerdraw-host`processは要求しない。Go packageをWails backendへin-process linkする。

### 12.4 VSCode

```text
platform-specific VSIX
  VSCode extension JS
  React webview assets
  @layerdraw/protocol
  @layerdraw/engine-client/stdio
  @layerdraw/composer
  @layerdraw/render
  @layerdraw/export
  @layerdraw/viewer
  @layerdraw/review
  @layerdraw/library
  @layerdraw/registry-client/host
  platform-specific layerdraw-host binary
  binary digest metadata
```

VSIXは対象platformのbinaryを同梱し、初回起動時にnetwork downloadしない。

### 12.5 SDK

SDKは8.4節のvariant closureに従う。Viewer、Browser Editor、Serverを別提供形態として数えない。

### 12.6 MCP Apps

```text
MCP Apps client
  @layerdraw/protocol
  @layerdraw/mcp-client
  @layerdraw/registry-client/mcp
  @layerdraw/render
  @layerdraw/export
  @layerdraw/viewer
  @layerdraw/library
  optional @layerdraw/react / @layerdraw/review
```

Engine / Runtimeは接続先hostが所有する。MCP Apps clientへGo Engineを暗黙bundleしない。
Registry toolsが接続先hostに無い場合もLibrary surfaceは存在するが、install / update / repair actionをdisabledにし、capability不足を表示する。

### 12.7 Marketplace Integrations

```text
Provider backend
  layerdraw-server closure
  provider auth / storage / share adapters

Provider Web surface
  SaaS Web closure
  provider launch / picker shell
```

provider storageを利用してもRuntime / Access / Engine境界は維持する。

### 12.8 Registry service

Registry serviceは提供形態ではなくcross-cutting deployable serviceである。公式Registryとself-host Registryは同じ`layerdraw-registry`binary semanticsを使う。

## 13. Public API Rules

### 13.1 Go facade

- package外からは各componentのroot facadeだけを呼ぶ
- input / outputはimmutable valueまたはopaque handleとする
- long-running operationは`context.Context`でcancel可能にする
- user source errorはDiagnosticとして返し、Go `error`だけへ畳まない
- Go `error`はI/O、process、cancellation、invariant violationなどoperationを成立させられないfailureに使う
- package-level mutable singletonと副作用のある`init()`を禁止する
- clock、random ID、filesystem、networkはport injectionする

### 13.2 TypeScript public surface

- `package.json#exports`に存在しないpathをpublic APIとしない
- generated型とhandwritten convenience型を同名で再定義しない
- clientはtransport errorとEngine / Runtime result statusを分けて返す
- browser packageはSSR import時にDOM / Workerへ副作用を起こさない
- Node-specific APIは明示subpathへ隔離する
- UI packageはclient、asset resolver、capabilityをinjectionで受け取る

### 13.3 Port ownership

port interfaceは利用側componentが所有する。adapter側が共通interfaceを定義してconsumerへ押し付けない。

例:

```text
Runtime owns StateBackend port
S3 adapter implements Runtime StateBackend port

Share owns ShareProvider port
Google Drive adapter implements ShareProvider port
```

## 14. Versioning and Compatibility

### 14.1 Fixed release set

公式Go binary、WASM、native artifact package、TS packageは同じLayerDraw release SemVerを付与する。internal Go packageを個別versioningしない。

### 14.2 Independent protocol versions

次はpackage SemVerと別のversionを持つ。

- LDL generation
- Engine Protocol version
- Runtime Protocol version
- Access Protocol version
- Server Application Protocol version
- Realtime Protocol version
- Query Plan Protocol version
- Search / Analysis Plan Protocol version
- generated semantic schema version
- RenderData schema version
- `.layerdraw` format version
- `.ldpack` format version
- Registry Protocol version
- renderer profile version
- exporter profile version
- Search Profile version
- Embedding Profile version

package version一致だけでwire互換性を推測しない。clientとbinaryは起動時handshakeでprotocol rangeとcapabilityを確認する。

### 14.3 Generated artifact matching

- `@layerdraw/engine-wasm`と対応Worker bootstrapは同じreleaseから生成する
- `@layerdraw/native-*`のbinary digestをrelease manifestへ固定する
- generated Go / TS bindingのschema digestをCIで比較する
- renderer / exporter profile ID、version、specification digestをrelease manifestとCIで比較する
- sidecar clientは未知の上位protocol majorを拒否する
- compatible minorで未知fieldを黙示破棄してcommit payloadを作り直さない

## 15. Conformance and Enforcement

### 15.1 Go

- import graph testで禁止edgeを拒否する
- `internal/engine/internal/*`の外部importを拒否する
- Engine packageからframework / provider SDK importを拒否する
- Runtime packageからadapter importを拒否する
- binaryごとのlinked capability manifestをgolden testする

### 15.2 TypeScript

- workspace dependency graphで禁止edgeを拒否する
- Target packageとして定義されていないlegacy package名のimportを拒否する
- package `exports`外のdeep importを拒否する
- browser entrypointのNode built-in importを拒否する
- generated packageの手編集差分をCIで拒否する
- bundle analysisでViewer variantへEngine / Node binaryを混入させない

### 15.3 Cross-runtime

- native、WASM、sidecar、Serverで同じEngine conformance fixtureを実行する
- generated schema digestをGo / TS / binary manifest間で一致させる
- delivery bundleごとに必要capabilityが1つ以上、権威実装が1つだけ存在することを検査する

## 16. Legacy Name Mapping

現行実装名は規範package名として継続しない。

| Current name | Target ownership |
| --- | --- |
| `@layerdraw/sdk` | `viewer`、`client-sdk`、`server-sdk`の公開surfaceへ分解する |
| `apps/server` nested Go module | root単一Go moduleのServer compositionへ統合する |

Legacy packageをcompatibility layerとして残す場合も、Go Engineを呼ぶfacadeに限定し、TS semantic implementationを保持してはならない。

## 17. 完了条件

Component / Package Boundary設計への適合は、次をすべて満たした時に成立する。

- すべてのcapability componentにprimary ownerが1つある
- すべてのimplementation artifactに言語と責務がある
- すべてのpackageに許可依存と禁止依存がある
- すべてのbinary closureが列挙されている
- すべてのdelivery bundle closureが列挙されている
- Engine semanticsを持つartifactが同じGo sourceから生成される
- Viewer / MCP Apps / framework shellがLDL semanticsを再実装しない
- Runtimeとprovider adapterの依存方向が逆転していない
- protocol、container、package release versionを混同していない
- import graph、schema digest、bundle capabilityをCIで機械検証できる
