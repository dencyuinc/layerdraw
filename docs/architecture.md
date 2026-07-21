# LayerDraw Technical Architecture

## 1. 文書の位置づけ

この文書は、LayerDraw の実装責務、実装言語、プロセス境界、配布単位を定める規範 Technical Architecture である。

- LDL の構文と意味は [ldl-language-specification.md](ldl-language-specification.md) と [ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) が規範である。
- Compiler / Workbench の入出力と処理段階は [compiler-architecture.md](compiler-architecture.md) が規範である。
- Component、Go / TS package、binary composition、delivery bundle closureは[component-package-boundary-specification.md](component-package-boundary-specification.md)が規範である。
- Engine Protocol、Runtime / Host Port、Realtime、Query adapter、Registry、Render / Export、SDK、Version / Releaseの境界契約は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)が規範である。
- To-Be repository topology、build、governance、CI、release、配布、SaaS CD、license enforcementは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)、licenseの法的適用範囲は[legal/README.md](legal/README.md)が規範である。
- AI / MCP の局所取得・編集契約は [ai-integration.md](ai-integration.md) が規範である。
- 提供形態、機能、責務 component の全体対応は [blueprint.md](blueprint.md) が規範である。

実装がこの文書と異なる場合、実装の現状を仕様として追認しない。移行中の実装差分として扱う。

## 2. 基本原則

### 2.1 LayerDraw Engine を唯一の意味論実装にする

LDL を解釈し、同じ入力から同じ意味結果を作る責務は Go 製の LayerDraw Engine だけが持つ。

LayerDraw Engine が所有するもの:

- LDL lexer、lossless parser、CST、typed AST
- module import、導入済み Pack、name resolution、型検査
- NormalizedDocument、StableAddress、SourceMap、SemanticIndex
- definition / graph / subject / subtree / child-set hash
- diagnostics、format、workspace organization
- semantic operation、局所 source rewrite、semantic diff
- before / after semantic diffからのAuthoringImpact分類
- Search / Query / Graph Analysisの検証、execution plan生成、domain result規範化
- QueryResult の規範化
- ViewRecipe から ViewData への materialization
- ExportPlan の生成
- `.layerdraw` / `.ldpack` の規範検証と生成
- StateQuerySnapshot の検証、hash、definition 互換性判定
- Access適用済みStateQuerySnapshotのschema / redaction marker検証

TypeScript、React、Echo、Wails、VSCode extension はこれらを再実装しない。

### 2.2 TS は表示、統合、transport を担当する

TypeScript が所有するもの:

- `schemas/` の規範 wire schema から生成された protocol 型と binding
- WASM、stdio、HTTP、Wails binding を隠す Engine client
- React editor、View Composer、Viewer、Inspector、Library UI
- ViewData から RenderData、layout、SVG / Canvas / WebGL 表示への変換
- ExportPlan と ViewData を使う visual serializer
- browser file picker、IndexedDB、provider SDK などの host adapter
- MCP Apps client、MCP client、Node / Next.js / Mastra 向け forwarding adapter

TS は LDL text を次の用途に限って扱える。

- source editor で表示・編集する
- syntax highlight、folding、入力補完候補を表示する
- UTF-8 bytes として LayerDraw Engine へ渡す
- LayerDraw Engine が返した diagnostics、source range、canonical source を表示する

syntax highlight 用 Tree-sitter grammar などは non-authoritative である。validate、hash、commit、Query、View materialization の根拠にしてはならない。

### 2.3 Framework は shell であり、意味論 component ではない

- Echo は LayerDraw Server の HTTP / WebSocket shell である。
- Wails は Desktop の native binding / window shell である。
- React は UI shell と component framework である。
- VSCode API は extension host shell である。
- Next.js / Mastra は host application integration shell である。

Framework を変更しても、LayerDraw Engine、protocol、ViewData、semantic operation の意味は変わらない。

### 2.4 論理責務、実装成果物、shell、提供物を混同しない

| 区分 | 意味 | 例 |
| --- | --- | --- |
| Capability component | 機能の意味と不変条件を所有する論理責務 | Engine、Runtime、Access、Registry |
| Implementation artifact | 他 component から利用できる library / binary / generated package | Go library、WASM、native sidecar、`@layerdraw/protocol` |
| Framework shell | artifact を特定環境へ接続する薄い入口 | Echo、Wails、React、VSCode extension host |
| Delivery bundle | ユーザーへ渡す完成した提供物 | SaaS、Self-host、Desktop、VSCode、SDK、MCP Apps、Marketplace |

同じ capability component は複数 artifact に build され、複数 delivery bundle に組み込まれる。delivery bundle の違いを semantic fork にしてはならない。

## 3. 規範 component 境界

### 3.1 Engine

EngineはpureなLDL意味論を所有する。hostのstorage、credential、Actor、revision head、networkを直接解決しない。

内部は Go package として分割してよいが、外部には versioned Engine Protocol を通じて一つの意味論 surface を提供する。

```text
Engine
  Compiler
  Workbench
  Authoring Impact Classifier
  Query Planner / Normalizer
  View Materializer
  Export Planner
  Package Validator / Builder
```

### 3.2 Runtime

Runtime は host document session と永続化 orchestration を所有する。

- Host Document ID と definition Project StableAddress の対応
- Working Document と Committed Revision
- base revision、lease、idempotency、conflict、commit
- autosave、checkpoint、operation log、compaction
- BackendBinding 解決、state head 固定、StateQuerySnapshot 構築
- Access decision の適用
- Authoring Policy / Grant snapshot固定とcommit直前の再評価
- realtime event、history、audit、reconcile

Runtime は Go library として実装し、Engine を process 内で呼ぶ。Runtime が LDL を独自解釈してはならない。

### 3.3 Access

Access は Actor、Instance / Organization / Workspace / Project role、shared ACL、credential scope、agent scope、EntitlementSnapshot / quota input から allow / deny / redact decision を生成する Go component である。Engineが生成したAuthoringImpact、owner protocolが宣言したHostOperationImpact、Host metadataのAuthoringPolicy / GrantSnapshotを入力にし、Schema定義、Graph instance、Query、View、asset、Package、Project設定等のwrite decisionも生成する。

Access は backend credential や state value を保持せず、RuntimeまたはServer Applicationがdecisionを入力へ適用する。Entitlement applicationはexternal / static providerからimmutable EntitlementSnapshotを供給し、最終的な機能可否はAccessがpermissionと合わせて判定する。商用契約や請求の意味論はPublic Coreに含めない。

AccessはLDL sourceをparseせず、EngineはActor / role / policyを解釈しない。EngineがLDL外操作を分類したり、Runtime handlerが自由文字列からcapabilityを推測したりせず、各versioned protocolがHostOperationImpactを固定する。Authoring Accessの分類、判定、強制境界は[authoring-access-control.md](authoring-access-control.md)を規範とする。

### 3.4 Server Application

Server ApplicationはSelf-hostを含むserver-backed利用のHost metadataと管理面を所有する。

- Server Instance administration
- 複数Organizationとtenant isolation
- Organization membership、Team、Service Account
- Workspace / Project directory
- Instance / Organization / Workspace / Project ACL
- Share、Review、Audit、Admin policy
- AuthoringPolicyの階層設定、永続化、membership / policy version
- Project Host settings等のHostOperationImpact評価とmetadata conditional publication
- EntitlementSnapshot取得とUsageEvent配送

Organization / Workspace所属はLDL definition、StableAddress、portable `.layerdraw`へ含めない。LayerDraw Runtime library単体はOrganizationを知らず、Server ApplicationがOrganization-scoped Document ID、Actor / Access context、storage bindingを解決してRuntimeへ渡す。

### 3.5 Host Ports and Adapters

Host port は Go interface と protocol schema で定義する。

- `ProjectRepository`
- `DocumentStore`
- `StateBackend`
- `AssetStore`
- `ArtifactStore`
- `HistoryStore`
- `RealtimeProvider` / `RealtimeRoom`
- `RegistrySource`
- `CredentialProvider`
- `Clock`
- `OrganizationRepository`
- `WorkspaceRepository`
- `AuthoringPolicyRepository`
- `IdentityRepository`
- `AccessRepository`
- `EntitlementProvider`
- `UsageSink`

native host は Go adapter を実装する。browser 固有能力は TS adapter が扱い、Engine Protocol を介して必要な bytes / typed result を受け渡す。provider ごとに LDL、Query、View の意味論を再実装しない。

### 3.6 Presentation

Presentation は TS / React component である。

- Composer は GUI 操作を semantic operation または ViewRecipe operation に変換する。
- Viewer は ViewData を読み、View 選択、zoom、focus、streaming update を提供する。
- Render は ViewData から visual layout と RenderData を生成する。
- Library UI は Registry client を通じて Pack / Template を閲覧・導入する。

Presentation は source text を直接書き換えず、Workbench に operation を渡す。

## 4. Engine Protocol

Go と TS、native process と browser、server と client の境界は単一の versioned Engine Protocol を使う。

### 4.1 Schema source of truth

protocol wire shapeはmonorepoの`schemas/`に置く言語非依存schemaを正本とする。LayerDraw Engineはsemantic validation behaviorを所有するが、Go structをwire schemaの正本にしない。JSON Schema、Protocol Buffers、または同等のcode generation可能なschemaを使い、次を生成する。

- Go request / response 型
- TypeScript request / response 型
- JSON serialization / validation fixture
- protocol compatibility test vector

手書きの TS interface を規範型にしない。生成型に presentation convenience type を重ねる場合も、wire semantics を変更しない。

### 4.2 共通 envelope

共通request / response envelope、`outcome`、Diagnostic / ProtocolFailure、handle、cursor、CapabilityManifestは[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 2章を規範とする。すべてのresponseはprotocol version、request ID、typed outcome、diagnosticsを保持し、pagination時はPageInfoを返す。

診断 code、StableAddress、source span、hash の算出は LayerDraw Engine だけが行う。

### 4.3 Transport

同じ protocol operation を transport ごとに変形しない。

| Transport | 用途 |
| --- | --- |
| in-process Go call | Server、Wails backend、native host |
| Go WASM Worker message | browser editor SDK、必要な client-side preview |
| stdio framed RPC | pure Engine sidecar、VSCode local host sidecar、Node Server SDK sidecar mode、local MCP bridge |
| HTTP / WebSocket | SaaS、Self-host、remote Server SDK、remote MCP |
| Wails generated binding | Desktop React shell と Go backend の接続 |

transport 固有 error は共通 diagnostic / operation result を隠してはならない。

### 4.4 Protocol operation groups

Engine Protocolは少なくとも次のoperation groupを持つ。transportごとに別APIモデルを作らない。

operationの規範名と個別契約は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 3章に従う。

| Group | Operations |
| --- | --- |
| Document | compile / open handle / update source bytes / close handle |
| Index | list modules / find symbols / read declarations / read rows / usages / neighbors |
| Search | prepare lexical / semantic / hybrid search / complete typed hits / cancel / inspect subgraph |
| Workbench | preview semantic operations / preview scoped fragment / apply to Working Document / format / organize |
| Query | prepare parameterized structural execution / complete typed raw rows / explain result |
| Analysis | prepare graph algorithm / complete typed metric rows / explain result |
| View | materialize ViewData / explain projection / analyze coverage |
| Export | plan export / validate serializer result manifest |
| Package | validate / build `.layerdraw` and `.ldpack` |

`apply to Working Document`はsource treeを変換するがrevisionをcommitしない。revision、lease、idempotency、audit、storage publicationはRuntime Protocolの責務である。

## 5. LadybugDB 境界

LadybugDB は graph query、FTS、Vector、Graph Analysisのexecution backendであり、LDL、Search、Hybrid ranking、Analysis resultの意味論source of truthではない。能力分離と必須primitiveは[search-query-and-analysis.md](search-query-and-analysis.md)を規範とする。

### 5.1 Native

Server、Desktop、native sidecar では LayerDraw Engine が Go binding を通じて LadybugDB を実行する。

```text
NormalizedDocument + StateQuerySnapshot + typed arguments
  -> Go Query Planner
  -> parameterized QueryExecutionPlan
  -> LadybugDB Go adapter
  -> typed raw rows
  -> Go Query Normalizer
  -> canonical QueryResult
```

同じnative adapterはSearchExecutionPlanとAnalysisExecutionPlanも実行する。FTS / VECTOR / ALGO extension呼出、indexの物理更新、typed raw row返却だけを行い、SearchDocument生成、embedding生成、Hybrid fusion、StableAddress bindingを所有しない。

### 5.2 Browser

browser では LadybugDB WASM を TS adapter から呼べる。ただし TS adapter は LayerDraw Engine が生成した parameterized Query / Search / Analysis ExecutionPlanを機械的に実行し、typed raw rows を LayerDraw Engine WASM へ返すだけである。

```text
LayerDraw Engine WASM
  -> QueryExecutionPlan / SearchExecutionPlan / AnalysisExecutionPlan
  -> TS Ladybug WASM adapter
  -> typed raw rows
  -> LayerDraw Engine WASM
  -> canonical QueryResult / SearchResult / AnalysisResult / ViewData
```

TS は query text を組み立てず、SearchDocumentやHybrid scoreを生成せず、StableAddress を解決せず、row を domain resultへ正規化しない。native、Node WASM、browser single-thread WASM、browser multithread WASMは同じconformance vectorでQueryResult、SearchResult、AnalysisResultの契約一致を検証する。公式Query-capable bundleはFTS、VECTOR、ALGOを任意extensionとして扱わない。

## 6. 実装成果物

### 6.1 Go artifacts

Go artifactは単一Go module内の独立packageから構成する。package path、依存DAG、binary closureは[component-package-boundary-specification.md](component-package-boundary-specification.md)を規範とする。

| Artifact | 形式 | 内容 |
| --- | --- | --- |
| LayerDraw Engine library | Go package | Compiler、Workbench、AuthoringImpact、Search、Query、Graph Analysis、View、ExportPlan、package semantics |
| LayerDraw Runtime library | Go package | session、state、storage、revision、AuthoringImpact / HostOperationImpactの原子的なAuthoring Access強制、Search Index / Embedding orchestration、realtime orchestration |
| LayerDraw Access library | Go package | Actor / hierarchical ACL / credential / agent scope / AuthoringPolicy / entitlementとAuthoringImpact / HostOperationImpactを統合したdecision |
| LayerDraw Registry client library | Go package | registry source、signature / publisher policy、dependency、install transaction |
| LayerDraw native exporters | Go package | ExportPlanをnative format profileでserializeする。semantic mappingは変更しない |
| LayerDraw MCP Host adapter | Go package | Engine / Runtime ProtocolをMCP tools / resourcesへ写す |
| LayerDraw Engine WASM | `.wasm` + loader | browser 内の Engine Protocol endpoint |
| `layerdraw-engine` | native binary | pureなstdio Engine Protocol sidecar。portable compile / preview用 |
| `layerdraw-host` | native binary | Engine、Runtime、Access、Registry client、Review、native exporters、MCP Hostをlinkしたlocal host |
| `layerdraw-server` | native / container binary | Engine、Runtime、Access、Registry、export、MCPとserver application componentをlinkしたHTTP / WebSocket host |
| `layerdraw-registry` | native / container binary | Registry client libraryとEngine package validatorを使う公式・self-host Registry service |

WASM、sidecar、serverは同じLayerDraw Engine sourceとconformance suiteからbuildする。

### 6.2 TypeScript artifacts

| Artifact | 責務 |
| --- | --- |
| `@layerdraw/protocol` | generated protocol types、diagnostic code、wire codecs |
| `@layerdraw/engine-client` | WASM / stdio / HTTP / Wails transport facade |
| `@layerdraw/query-adapter-ladybug-wasm` | LayerDraw EngineのQuery / Search / Analysis ExecutionPlanとSearchIndexPlanをLadybug WASMで実行しtyped raw rowsだけを返すbrowser adapter |
| `@layerdraw/embedding-provider-wasm` | version固定Embedding Profileでvector生成だけを行うbrowser Host Adapter。SearchDocumentやrankingを生成しない |
| `@layerdraw/render` | ViewData から layout / RenderData / visual output |
| `@layerdraw/export` | ExportPlanとViewDataをbrowser / Node format profileでserializeする |
| `@layerdraw/viewer` | framework-neutral readonly viewer |
| `@layerdraw/composer` | GUI intent から semantic operation / ViewRecipe operation を作る |
| `@layerdraw/react` | Authoring grant-aware React editor / viewer / inspector / project UI |
| `@layerdraw/library` | Pack / Template Library UI |
| `@layerdraw/registry-client` | Registry / local host / MCP transport facade。Registry semanticsは持たない |
| `@layerdraw/review` | diff / review / comment / approve UI model |
| `@layerdraw/server-client` | LayerDraw Server Resource Access / AuthoringPolicy / HTTP / WebSocket client |
| `@layerdraw/client-sdk` | browser / client embedding facade |
| `@layerdraw/server-sdk` | Node / Next.js / Mastra から server / sidecar を使う facade |
| `@layerdraw/mcp-client` | MCP Apps と host client の protocol adapter |
| `@layerdraw/engine-wasm` | version固定済みEngine WASMとWorker bootstrap |
| `@layerdraw/native` | Server SDK用platform binary resolver |

TS package は Engine semantics を fork しない。`@layerdraw/client-sdk` と `@layerdraw/server-sdk` は facade であり、compiler を再実装しない。

## 7. 提供形態への組み込み

| 提供形態 / SDK variant | Engine の実行場所 | shell / binding | 永続 host |
| --- | --- | --- | --- |
| SaaS | `layerdraw-server` 内の native Go | React Web + HTTP / WebSocket | managed Runtime / storage adapters |
| Self-host | `layerdraw-server` 内の native Go | React Web + Echo | self-host Runtime / storage adapters |
| Desktop | Wails Go backend 内 | Wails + React | local / external storage adapter |
| VSCode | bundled `layerdraw-host` sidecar | VSCode extension + stdio | workspace file / optional remote backend |
| SDK / Viewer variant | 実行しない | TS Viewer | host が ViewData / optional ExportPlan を供給 |
| SDK / Browser Editor variant | LayerDraw Engine WASM Worker | TS Engine client + React optional | browser / injected host adapter |
| SDK / Server variant | `layerdraw-engine` / `layerdraw-host` sidecarまたはremote server | TS facade for Node / Next.js / Mastra | host appまたはLayerDraw Server |
| MCP Apps | 接続先 host の Engine | MCP Apps client + Viewer | 接続先 host |
| Marketplace | provider integration backend 内の native Go | provider Web shell | provider storage + LayerDraw Runtime |

SaaS / Self-host の browser は通常 server を権威 host とする。低遅延 preview のため LayerDraw Engine WASM を併用してよいが、commit は server が再検証し、client preview を信用しない。

## 8. Server と shell

LayerDraw Server は Engine を含む host 基盤であり、Engine 自体ではない。

```text
React Web
  -> HTTP / WebSocket / MCP
  -> Echo shell
  -> LayerDraw Server application
     -> Organization / Workspace / Share / Review / Entitlement / Usage
     -> Access
     -> Runtime
        -> Engine
        -> Host ports
           -> storage / metadata / realtime / registry adapters
```

Echo は route、middleware、stream、WebSocket upgrade を担当する。document commit、Query、View、hash、package validation を Echo handler に実装しない。

Cloudflare Workers のように Go native binary を実行できない環境は、LayerDraw Server 互換 API を独自の TS semantic implementation で作らない。LayerDraw Engine を実行できる service / WASM host へ委譲する。deployment convenience のために意味論を二重化しない。

## 9. Storage と state

storage と state backend は Runtime port の adapter である。

- `.ldl` source tree は definition の正本である。
- `.ldstate.json` または remote state は provenance / freshness / system fields を持つ。
- `layerdraw.backend.json` / `project.ldbackend.json` は non-secret backend binding である。
- credential は env、OS keychain、OAuth、IAM、host secret store から注入する。
- `.layerdraw` は source、resolved dependency、assets、任意 state、派生成果物を持つ portable container である。

Runtime は backend から state を読み、Access decision と compatibility を適用して immutable StateQuerySnapshot を構築する。Engine はその snapshot を pure input として検証・評価し、backend locator や credential を受け取らない。

## 10. Realtime と commit

共同編集は Runtime の Host Document session 単位で行う。

1. client は base revision、precondition hash、semantic operation を送る。
2. Runtime は current head、lease、Access を確認する。
3. Workbench が Working Document に操作を適用する。
4. Engine が全 validation を行う。
5. Runtime が canonical source tree を conditional write し、Committed Revision を公開する。
6. state、audit、realtime event を同じ operation ID で追跡する。

CRDT / OT / command log は transport と Working Document coordination の選択肢であり、Committed Revision の意味論を変更しない。すべての commit は LayerDraw Engine の validation と canonicalization を通る。

## 11. Conformance と禁止事項

### 11.1 必須 conformance

- native library、WASM、sidecar、server で同じ language fixture を実行する。
- parse diagnostics、NormalizedDocument、hash、SearchResult、QueryResult、AnalysisResult、ViewData、ExportPlan が byte-level または規範比較で一致する。
- semantic operation、fragment、source patch、import、Registry、restoreの同じbefore / afterが同じAuthoringImpactを生成する。asset、Package transaction、Project設定はentrypointによらず同じHostOperationImpactを生成する。
- `schemas/` とGo / TypeScript generated bindingのdriftをCIで拒否する。
- native、Node WASM、browser single-thread WASM、browser multithread WASMでFTS、Vector、Hybrid、Structural Query、PageRank、K-Core、Louvain、SCC、WCCを同じtest vectorで比較する。
- 公式Query-capable bundleは必須Ladybug primitive欠落、Embedding Profile不一致、stale Search Indexをrelease gateで拒否する。
- package writer の canonical JSON、ZIP entry、digest を実行形態間で一致させる。

### 11.2 禁止事項

- TS で LDL parser / validator / formatter / hash を権威実装すること
- UI が source text を ad hoc に書き換えること
- Echo handler、React component、Wails method、VSCode command に domain semantics を置くこと
- provider adapter が Query / View / access semantics を変更すること
- TS / MCP / Ladybug adapterがSearchDocument、Hybrid ranking、AnalysisResultを独自生成すること
- TS、MCP、Runtime handlerがsubject kindからAuthoring Capabilityを独自分類すること
- UI controlのdisabled状態だけでSchema / Graph writeを強制したと主張すること
- `document.json` を source に逆変換して正本を修復すること
- MCP transport が Engine の diagnostics や conflict result を独自形式へ丸めること

## 12. Repository 構造方針

LayerDrawのEngine、Runtime、schema、generated binding、TS package、application shellはpublic product monorepoでatomicに管理する。Goはroot単一moduleとし、repository分割をpackage境界の代わりにしない。

private SaaS control plane / GitOps、公式Registry content、生成専用distribution repositoryは、公開可否、credential、content lifecycle、write authorityが異なるため別repositoryとする。正準layout、ownership、CI、release、CD、license enforcementは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)、licenseの法的適用範囲は[legal/README.md](legal/README.md)を規範とする。

LayerDraw Engine は React、Echo、Wails、VSCode、Node package に依存しない。TS presentation package は `@layerdraw/protocol` と client facade を通じて Engine を利用する。private cloudはpublic Go `internal/`をimportせず、署名済みrelease artifactとversioned public protocolを使う。
