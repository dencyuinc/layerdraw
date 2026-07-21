# LayerDraw AI / MCP 統合仕様

## 1. 目的

LayerDraw は AI agent が大規模な構造モデルを、全文を毎回読まずに把握・編集できることを製品要件とする。

AI にとっての正本は project-local LDL source tree である。ただし、MCP の標準更新入力を自由な source 全文や正規化 JSON にはしない。LayerDraw Engine が作る SemanticIndex、StableAddress、semantic operation、scoped LDL fragment を組み合わせる。

MCP toolの公開範囲、CapabilityManifest、Engine-only / Runtime-required境界、Document I/OとView exportの区別は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 9章を規範とする。FTS / Vector / Hybridによる起点発見、Structural Query、Graph Analysisの意味とAI workflowは[search-query-and-analysis.md](search-query-and-analysis.md)、Schema / Graph等の編集権限分離は[authoring-access-control.md](authoring-access-control.md)を規範とする。

```text
AI intent
  -> lexical / semantic / hybrid discovery
  -> scoped graph inspection / scoped LDL read
  -> structural query / optional graph analysis
  -> structured operation or scoped LDL fragment
  -> Go Workbench preview
  -> validation / semantic diff / render preview
  -> Runtime commit
```

## 2. LDL を使う理由

LDL は人間がレビューでき、Git diff に残せ、コメントと Reference を保持できる宣言的 source である。JSON は MCP wire と操作入力には適するが、永続 source の代わりにはしない。

LDL の強み:

- EntityType、RelationType、Entity、Relation、Layer、Query、View の意図が source に残る
- module と標準 workspace 構成により局所読取できる
- StableAddress により display name や file path 変更から identity を分離できる
- lossless CST によりコメントと小さい diff を保てる
- 同じ source を人間、GUI、MCP、Git、`.layerdraw` が共有できる

MCP の JSON は command protocol であり、第二の canonical document model ではない。

## 3. 実装責務

### 3.1 LayerDraw Engine / Workbench

次を唯一の権威実装として所有する。

- parse、validate、format、name resolution、StableAddress、hash
- SemanticIndex と source span
- scoped read の選択と規範 envelope
- semantic operation と scoped LDL fragment の検証
- 局所 CST rewrite と canonical source 生成
- QueryExecutionPlan、QueryResult、ViewData、ExportPlan
- SearchDocument、SearchResult、AnalysisResultとHybrid ranking
- conflict、diagnostic、semantic diff
- before / afterからのAuthoringImpactとRequired Capability分類

### 3.2 Runtime / Access

- Host Document ID、revision、lease、idempotency
- Actor / agent scope、ACL、redaction、entitlement
- state backend と immutable StateQuerySnapshot
- Access適用済みSearch Index、Embedding Provider、revision固定
- commit、audit、history、realtime event
- AuthoringPolicy / Grant固定、Access decision、apply直前の再評価

### 3.3 MCP adapter

MCP adapter は Engine / Runtime の protocol を MCP tools / resources へ写す transport adapter である。LDL を parse せず、操作を source text へ変換せず、diagnostic を独自解釈しない。

- server-backed host: Go MCP endpoint が Engine / Runtime を直接呼ぶ
- local host: `layerdraw-host` sidecarまたはDesktop hostにMCP adapterを載せる。hostなしのportable previewだけなら`layerdraw-engine`を使う
- Node / Next.js / Mastra: TS Server SDKが`layerdraw-engine`、`layerdraw-host`、またはLayerDraw Serverへ用途別にforwardする
- MCP Apps: client UI と streaming Viewer を提供し、接続先 host の tools を呼ぶ

## 4. Context model

### 4.1 全文を既定で返さない

MCP read は次の順で対象を狭める。

1. project / module manifest を列挙する
2. 正確なID / nameが分かる場合は`find_symbols`、自然言語や曖昧な語句では`search`を使う
3. SearchHitのStableAddressとscore理由を確認する
4. `inspect_subgraph`で型、Layer、rows、incoming / outgoing Relationの必要部分だけ読む
5. 必要なら保存済みQueryを実行し、明示scopeへGraph Analysisを適用する
6. 追加 context が必要な場合だけ cursor で広げる

NormalizedDocument 全体や `.layerdraw` ZIP を既定応答に含めない。

### 4.2 読取表現

AI が構文と意図を理解する標準表現は compact な scoped LDL source である。機械的な位置決めと競合制御のため、各 scope に metadata を付ける。

```json
{
  "document_id": "doc_01j_logistics",
  "revision": 42,
  "definition_hash": "sha256:...",
  "scope": {
    "address": "ldl:project:logistics:entity:order_api",
    "kind": "entity",
    "module_path": "layers/application/entities.ldl",
    "source_span": { "start": 120, "end": 248 },
    "subject_hash": "sha256:..."
  },
  "ldl": "entity order_api: api @application { ... }",
  "related_addresses": [],
  "diagnostics": [],
  "result_truncated": false,
  "next_cursor": null
}
```

`document_id` は host identity、StableAddress は portable definition identity である。どちらかを他方の代わりに使わない。
server-backed MCPではServer Applicationがrequest envelopeまたは接続sessionからOrganization scopeとActor access fingerprintを解決してからMCP Hostへ渡す。semantic operation内へ`organization_id`を重複格納せず、`document_id`だけを受けたMCP adapterがunscoped lookupを行うことも禁止する。

### 4.3 Token budget

すべての list / search / graph read tool は次を受け取る。

- `max_items`
- `max_output_bytes` または `max_output_tokens`
- `cursor`
- relation direction / depth
- include flags for source、rows、relations、type definitions、references

Search cursorはさらにDocumentSnapshotRef、index identity、Search Profile、Embedding Profile、Access fingerprintへ束縛する。SearchHitの順位だけを次requestのidentityとして使わず、StableAddressとhost revisionまたはportable generationを使う。

上限超過時は黙って切り捨てず、`result_truncated: true` と `next_cursor` を返す。cursorはDocumentSnapshotRefとquery digestに束縛し、別host revisionまたはportable generationへ流用できない。

### 4.4 Reference

LDL の `Reference` declaration は project / Pack / Template 固有の自然言語情報である。

- `list_references` は存在、title、scope を列挙する
- `read_references` は選択された Reference 本文をまとめて返す
- Reference を Entity 単位の隠し metadata へ拡張しない
- AI は必要な時だけ Reference を読み、常時 context に注入しない

## 5. Read tools

規範 tool capability は次である。MCP 上の公開名には `layerdraw.` prefix を付けてよい。

| Tool | 目的 |
| --- | --- |
| `list_modules` | entry / import closure、module digest、declaration count を列挙する |
| `find_symbols` | kind、ID、name、type、layer、moduleから正確なStableAddress候補を探す |
| `search` | lexical / semantic / hybrid検索で曖昧な要求から候補subjectとgraph entryを探す |
| `read_declarations` |指定 declaration の compact LDL と metadata を読む |
| `read_rows` | Entity / Relation の owner-scoped row subtree を page 単位で読む |
| `get_neighbors` | incoming / outgoing Relation と endpoint を深度制限付きで読む |
| `inspect_subgraph` | 選択Entityの型、Layer、rows、Relation、endpointを一つのbounded envelopeで読む |
| `find_usages` | StableAddress の参照元を読む |
| `read_scope` | module、layer、owner、View に閉じた複合 scope を読む |
| `run_query` | saved Query を typed arguments と固定 state input で実行する |
| `analyze_graph` | QueryResultまたはrevision固定済みaddress集合へ標準graph algorithmを適用する |
| `explain_view` | ViewRecipe、selection、projection、omission diagnostics を説明する |
| `list_references` | Reference を列挙する |
| `read_references` | 指定した複数の Reference 本文を読む |

`get_capabilities`はoperation availabilityに加え、effective `AuthoringGrantSummary`、policy / membership version、constraint summaryを返す。grantの完全なrole / policy内部表現や他Actorのgrantは返さない。

`get_normalized_json` のような全内部表現取得は標準 tool にしない。debug / interoperability 用 capability として明示的に分離し、通常の agent context へ使わない。

`find_symbols`はsymbol resolutionでありProject Searchの代用ではない。`search`は順位付きの派生結果であり、結果をView selectionへ直接保存しない。AIは採用したStableAddressを明示rootまたはtyped predicateとしてQueryへ落とし、previewしてからcommitする。

`run_query` は LDL 詳細仕様の `QueryExecutionRequest` / `QueryExecutionResponse` を使う。Runtime は開始時の definition revision と state head を固定し、Access が許可した field だけから StateQuerySnapshot を構築する。redacted field を missing や空値へ偽装しない。`analyze_graph`はcanonical QueryResultまたは明示address集合だけをscopeとし、Searchの「上位N件」を可変なまま入力にしない。

## 6. Write modes

### 6.1 Semantic operations

既存構造の通常更新、削除、rename、row 更新、Relation endpoint 更新は semantic operation を標準にする。

```json
{
  "document_id": "doc_01j_logistics",
  "base_revision": 42,
  "idempotency_key": "agent-run-7f7f4c4d",
  "expected_subject_hashes": {
    "ldl:project:logistics:entity:order_api": "sha256:..."
  },
  "expected_subtree_hashes": {},
  "expected_child_sets": [],
  "operations": [
    {
      "operation": "update_subject_field",
      "target_address": "ldl:project:logistics:entity:order_api",
      "field": "display_name",
      "value": "受注 API"
    },
    {
      "operation": "create_relation",
      "parent_address": "ldl:project:logistics",
      "id": "order_calls_shipping",
      "type_address": "ldl:project:logistics:relation-type:calls",
      "from_address": "ldl:project:logistics:entity:order_api",
      "to_address": "ldl:project:logistics:entity:shipping_api"
    }
  ]
}
```

このwrite object全体がhost-backed Runtimeへ渡すOperationBatchである。MCP request envelopeやtool adapterが`document_id`、`base_revision`、`idempotency_key`を外側へ重複定義してはならない。

operation は JSON で受け取るが、Go Workbench が lossless CST の対象 node を書き換え、canonical LDL source を生成する。TS / MCP adapter が source template を組み立てない。

Workbench previewはAuthoringImpact、Required Capability、impact digestを返す。MCP adapterはoperation名から`schema:write`等を推測せず、host-backed previewではAccess decisionもそのまま返す。

### 6.2 Scoped LDL fragment

新しい Layer 一式、複数 Entity / Relation、Query / View のように構造をまとめて作る場合は scoped LDL fragment を使える。

```json
{
  "owner_address": "ldl:project:logistics",
  "allowed_kinds": ["entity", "relation"],
  "expected_child_set_hash": "sha256:...",
  "ldl": "entities api @application {\n  shipping_api \"出荷 API\"\n}\n\nrelations calls {\n  order_calls_shipping: order_api -> shipping_api\n}\n"
}
```

Go Workbench は fragment を parse し、既存 scope と name resolution し、等価な semantic change と source edit を preview する。fragment を file へ文字列連結しない。

`allowed_kinds`はfragment parse scopeでありAccess grantではない。fragmentがSchema subjectを作る場合は`schema:write`を要求する。

### 6.3 Source patch fallback

コメント、Reference text、syntax-level migration など operation で表現しきれない場合だけ使う。

必須 input:

- module path
- base revision
- expected source digest
- source range
- replacement text

Go parser / validator を通過しない patch は Committed Revision へ公開しない。

source patchもbefore / afterのAuthoringImpactを生成する。generic patch toolを`schema:write`の迂回経路にしない。意味不変なsource変更は`source:maintain`を要求する。

### 6.4 Asset staging

binary asset は source operation と分けて先に staging する。

1. `stage_asset` が`asset:write`のHostOperationImpactをAccess評価し、digestとtemporary asset refを返す。
2. semantic operation / fragment がその digest を参照する。
3. Runtime commit がAuthoringImpactとHostOperationImpactのevaluation digest、source、asset manifestを同じoperation boundaryへ束縛する。
4. commit されなかった staging asset は期限切れで回収する。

## 7. Preview と apply

### 7.1 Portable preview

host document がない場合、closed source tree と操作を受け取り次を返す。

- changed source files
- diagnostics
- semantic diff
- preview definition hash
- optional ViewData / render preview

Document ID、revision、audit、state write、idempotency record は生成しない。

### 7.2 Host preview

Host Document ID と base revision に対して preview する。現在 head を変更せず、conflict と expected hash を評価する。

### 7.3 Apply

`layerdraw.apply_operations`はhost-scoped Runtimeがある時だけ使える。入力ではLDL詳細仕様のOperationBatchを一度だけ渡し、RuntimeCommitResult内のOperationResultを変更せず、次のstatusを保持する。

- `committed`
- `rejected`
- `committed_state_stale`
- `needs_review`

HTTP 2xx や MCP call success だけを commit 成功として扱わない。

apply時は最新のAuthoringPolicy、membership、agent delegationでAuthoringImpactを再評価する。`agent:apply`だけではRequired Capabilityを満たさない。`agent:propose`だけのagentはproposalを作成できるが、正本へ適用できない。

## 8. Tool set

更新・検証 tool capability:

| Tool | 目的 |
| --- | --- |
| `layerdraw.preview_operations` | `scope=portable|host`でoperationを副作用なしに試す |
| `layerdraw.apply_operations` | Runtime を通じてOperationBatchをcommitする |
| `layerdraw.preview_fragment` | scoped fragmentをparse・解決・diff化する |
| `layerdraw.preview_source_patch` | revision-protected low-level patchを副作用なしに試す |
| `layerdraw.apply_source_patch` | Runtimeを通じて確認済みsource patchをcommitする |
| `layerdraw.format_scope` | declaration / moduleをGo formatterで整形する |
| `layerdraw.organize_workspace` | source treeを標準構成へ再配置する |
| `layerdraw.stage_asset` | Runtime AssetStoreへdigest付きtemporary assetをstagingする |

render / export tool は ViewRecipe address、revision、typed arguments、state input policy を受け取る。MCP adapter が graph を直接図形へ変換しない。

検索・分析 tool:

| Tool | 目的 |
| --- | --- |
| `layerdraw.search` | FTS / Vector / Hybridで探索起点候補を返す |
| `layerdraw.inspect_subgraph` | 選択した起点周辺をbounded contextとして返す |
| `layerdraw.analyze_graph` | PageRank、K-Core、Louvain、SCC、WCCを明示subgraphへ適用する |

MCP adapterは自然言語からembeddingを生成せず、RuntimeがEmbedding Providerを呼ぶ。raw Cypher、Ladybug procedure、embedding vector、backend query textをagentへ公開しない。

## 9. GUI、SDK、MCP の統一

```text
GUI intent
  -> semantic operation
  -> Engine client
  -> Go Workbench

SDK intent
  -> semantic operation
  -> Engine client
  -> Go Workbench

MCP intent
  -> search / inspect / query / analyze
  -> semantic operation or scoped LDL fragment
  -> MCP adapter
  -> Go Workbench
```

入口は異なっても、validation、source rewrite、hash、conflict、Query、View の実装は一つである。

## 10. MCP Apps

MCP Apps は AI session 内の client 提供形態である。

- AI が生成・更新した LDL の ViewData を streaming render する
- SearchHit、scoped subgraph、AnalysisResultを段階的に表示する
- Query / View を作成・編集できる tools を接続先から利用する
- `.ldl` と `.layerdraw` を接続先 host へ upload / import / export する
- preview、semantic diff、diagnostics、approve / apply UI を表示する
- 接続先に Project Management がなければ project tools を表示しない
- 接続先に Runtime がなければ apply、history、realtime tools を表示しない

MCP Apps 自身が authoritative Runtime や Compiler を持つ必要はない。connected host の capability discovery に従う。

## 11. Security

- すべての host-scoped tool は Actor / agent scope を評価する。
- delegated agent grantは委任元ActorのAuthoring CapabilityとProject Policyを拡張しない。
- read tool にも field-level redaction を適用する。
- source span が許可範囲外を含む場合、部分文字列で漏洩させず declaration 単位で拒否または規範 redaction する。
- query backend text、credential、backend locator を agent へ公開しない。
- raw embedding、index内部ID、Access適用前の件数やscoreをagentへ公開しない。
- 破壊的操作は影響する usages、View、rows と semantic diff を preview する。
- import、Registry、restore、reconcile、source patchもAuthoringImpactを評価し、generic toolからSchema制限を迂回させない。
- agent が state system field を authored LDL attribute へ暗黙 copy することを禁止する。

## 12. Context efficiency の受入条件

- project 全体を読まずに module / symbol 一覧を取得できる。
- 一つの Entity と incoming / outgoing Relation、型、必要 rows を一回の bounded call で取得できる。
- 自然言語だけからFTS / Vector / Hybridで候補Entityを探し、score理由とStableAddressを取得できる。
- SearchHitを確認後、Project全体を読まずに周辺subgraphを取得して保存済みQueryのrootへ変換できる。
- 明示したQueryResultへGraph Analysisを適用でき、結果がMaster Graphを暗黙更新しない。
- pagination / truncation が明示される。
- 一つの declaration 更新で無関係な module の source bytes が変わらない。
- bulk creation は scoped fragment で表現できる。
- AI が受け取る StableAddress と hash をそのまま write precondition に使える。
- validation error は diagnostic code、StableAddress、source span、repair hint を持つ。
- 同じ操作が GUI、MCP、SDK で同じ canonical source と OperationResult を生成する。

## 13. 非目標

- AI に ZIP container や generated `document.json` を直接編集させること
- 毎回 source tree 全文を prompt に入れること
- 自由 JSON graph を別の正本として維持すること
- MCP adapter ごとに LDL parser や mutation logic を持つこと
- LLM の推測だけで StableAddress、hash、Relation endpoint を補うこと
- Search score、embedding近傍、algorithm resultを承認なしにLDL属性またはView selectionへ固定すること
