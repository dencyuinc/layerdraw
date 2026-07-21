# 検索・構造Query・グラフ分析仕様

## 1. 目的

本書は、LayerDrawの大規模Master Graphから探索起点を発見し、決定論的な部分グラフを選択し、その構造を分析するための規範契約を定める。

対象は次の3能力である。

1. **Project Search**: FTS、Vector、Hybridによって候補subjectを発見する。
2. **Structural Query**: LDLに保存された型付きQuery recipeによって部分グラフを決定論的に選択する。
3. **Graph Analysis**: 明示された部分グラフにPageRank等のアルゴリズムを適用する。

これらは同じLadybugDB execution backendを利用できるが、意味、永続性、再現性を混同してはならない。LDL Queryの構文と評価は[ldl-language-specification.md](ldl-language-specification.md)および[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)、Engine / Runtime / adapterのwire契約は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

## 2. 能力の分離

| 能力 | 主目的 | 入力 | 出力 | 正本性 |
| --- | --- | --- | --- | --- |
| Project Search | 探索起点の発見 | 自然言語、keyword、scope filter | 順位付きSearchHit | 派生結果。正本ではない |
| Structural Query | 対象subgraphの選択 | 保存済みQuery、typed arguments、固定state input | canonical QueryResult | recipeはLDL正本。結果は派生物 |
| Graph Analysis | 重要度、cluster、連結性の分析 | 明示されたQueryResultまたはaddress集合 | AnalysisResult | 派生結果。Master Graphを変更しない |

規範パイプラインは次である。

```text
自然言語またはkeyword
  -> FTS / Vector / Hybrid Search
  -> ranked SearchHit + StableAddress
  -> scoped inspection
  -> 明示したStableAddressをQuery rootへ採用
  -> Structural Query / directed traversal
  -> optional Graph Analysis
  -> Query / View recipeのpreviewとcommit
```

SearchHitの順位、score、embedding近傍をViewの正本selectionとして直接保存してはならない。Viewへ使う場合は、ユーザーまたはAIが確認したStableAddress、型、Layer、属性predicate、RelationType、方向、深さをLDL Queryへ明示する。これによりembedding model、index build、近似探索、backend versionの変更で保存済みViewが暗黙変化することを防ぐ。

## 3. LadybugDB必須能力

LayerDraw公式配布物のLadybug execution profileは次を必須とする。

```text
required_ladybug_primitives
  structural_match
  typed_predicate
  directed_traversal
  shortest_path
  fts_bm25
  vector_hnsw
  vector_filtered_search
  algo_page_rank
  algo_k_core
  algo_louvain
  algo_scc
  algo_wcc
```

FTS、VECTOR、ALGOは任意の製品機能ではない。native、Node WASM、browser single-thread WASM、browser multithread WASMを含む公式Query-capable bundleは、上記primitiveをすべて提供しなければならない。

Capability negotiationは残すが、公式bundleで欠落を黙ってdegradeするためには使わない。

- 公式bundle: 必須primitive欠落はbuildまたは起動時の適合性failureとする。
- third-party adapter: capability不足をmanifestで表明できるが、該当operationは`capability_unavailable`として拒否する。
- 実行中: FTSだけ、Vectorだけ、全件走査などへの暗黙fallbackを禁止する。

Ladybug VECTORはvector indexと近傍検索を提供するが、自然言語からembeddingを生成しない。Embedding Providerは別のHost Portとして必須である。

## 4. Search Corpus

### 4.1 対象subject

Project Searchは次をindex可能とする。

- Layer
- EntityType、RelationType
- Entity、Relation
- Entity row、Relation row
- Query、View
- Reference

row hitはrow自身のStableAddressとowner Entity / RelationのStableAddressを返す。AIがgraph traversalの起点として使うのはEntity StableAddressであり、row、Type、View、ReferenceのhitをEntityだと推測してはならない。

### 4.2 SearchDocument

LayerDraw EngineはNormalizedDocumentから次の論理SearchDocumentを生成する。

```text
SearchDocument
  subject_address
  subject_kind
  owner_address?
  graph_entry_addresses[]
  type_addresses[]
  layer_addresses[]
  fields[]
    field_path
    source_ref
    text
    lexical_weight
    include_in_embedding
  content_hash
```

`graph_entry_addresses`はEntityなら自身、Entity rowならowner Entity、Relation / Relation rowならendpoint Entityを`from`、`to`順のlistで返す。self Relationでは重複を除く。Type、Layer、Query、View、Referenceには暗黙のgraph entryを生成しない。

SearchDocumentの文字列化、field順、Unicode NFC正規化、null / absent処理、scalar表現はLayerDraw Engineだけが所有する。TS、MCP adapter、Ladybug adapter、Embedding Providerが独自にsubject textを組み立ててはならない。

### 4.3 標準field優先度

既定のlexical weight順は次とする。具体的な数値はversioned Search Profileで固定する。

1. authored ID、display name
2. type、Layer、Relation label、tag
3. description
4. attribute rowの文字列、enum、URI等の表示値
5. Reference本文、Query / View intent

password、credential、backend locator、opaque state、Audit本文、presence、lock、leaseをcorpusへ入れてはならない。StateQuerySnapshotのfieldはProject Searchへ暗黙indexせず、保存済みStructural Queryのtyped state predicateで扱う。

### 4.4 Access適用

server-backed searchはOrganization、Workspace、Project、Actor / agent scopeで閉じる。Accessで拒否またはredactされたfieldをindex検索後に除外するだけでは不十分であり、score、件数、highlight、timingから存在を漏らしてはならない。

実装は次のどちらかを満たす。

- access partitionごとに許可済みcorpus / indexを分離する。
- subject / fieldへaccess labelを付け、FTSとVectorの候補生成前にbackend filterを適用する。

どちらの場合も`access_projection_digest`をindex identityへ含める。post-filterだけによる権限制御は禁止する。portable/local sourceでは入力source全体を呼出主体が読めることをHostが保証する。

## 5. 派生Search Index

### 5.1 Identity

Search indexは削除・再生成可能な派生cacheであり、LDL、State、Historyの正本ではない。host-backed documentとportable sourceを同じopaque IDで偽装せず、共通のSnapshotRefで区別する。

```text
DocumentSnapshotRef =
  { kind: host_revision, host_document_id, committed_revision, definition_hash }
  | { kind: portable_generation, source_tree_digest, document_generation, definition_hash }
```

```text
SearchIndexIdentity
  document_snapshot_ref
  search_profile_id
  search_profile_digest
  embedding_profile_id
  embedding_profile_digest
  access_projection_digest
  ladybug_backend_version
  index_schema_version
```

identityのいずれかが変われば別indexとして扱う。stale indexを新revisionの結果として返してはならない。index bytes、embedding vector、HNSW内部IDを`.ldl`または`.layerdraw`の正本sourceへ保存しない。

Engineが生成するSearch / Query / Analysis / Index planは、正確な`DocumentSnapshotRef`、Access projection、Search / Embedding Profile、Search Index identity、request digestへ署名付きでbindする。plan tokenは暗号学的nonceと短い有効期限を持つ単回使用とし、adapterはbackend実行開始前に原子的にconsumeする。成功、backend失敗、並列実行のいずれでも再利用できず、Host再起動後の旧tokenも拒否する。Runtimeは実行直前にbindingを再検証し、別revision、別session、別profileへplanを付け替えてはならない。Searchのtop-level `query_text` / `mode`はEngine request内の値と完全一致しなければならず、不一致はembedding生成、cursor検証、adapter実行より前に拒否する。

### 5.2 Search Profile

Search Profileはcorpus field、FTS analyzer、候補数、Hybrid fusionを固定する。

```text
SearchProfile
  profile_id
  profile_version
  specification_digest
  unicode_normalization: nfc
  lexical_analyzer
  lexical_field_weights[]
  lexical_candidate_limit
  semantic_candidate_limit
  rrf_k
  lexical_weight
  semantic_weight
  snippet_max_bytes
  highlight_policy
```

`lexical_candidate_limit`と`semantic_candidate_limit`は`max_hits`以上でなければならない。`rrf_k`は1以上、weightは有限の0以上で、Hybridでは少なくとも一方を正とする。同じprofile ID / versionでanalyzer、weight、candidate limit、fusionを変更してはならない。

### 5.3 Embedding Profile

Embedding Profileは少なくとも次を固定する。

```text
EmbeddingProfile
  profile_id
  model_id
  model_version
  model_digest
  dimensions
  distance_metric
  normalization
  document_prefix?
  query_prefix?
  max_input_tokens
  chunking_profile_version
```

公式Query-capable hostは互換する既定Embedding Providerを1つ以上構成できなければならない。remote provider、local native model、browser modelの差はHost Adapterの差であり、SearchDocument生成、chunking、result semanticsを変えない。

`mode: lexical`はEmbedding Providerやvector primitiveなしで実行できる。`semantic`と`hybrid`だけが互換するEmbedding ProfileとProviderを必須とし、利用不能時はtyped failureで閉じる。

Embedding Providerへ送るtextはAccess適用後でなければならない。外部providerを使う場合、credential、data residency、retention、provider logging policyをHost policyで制御する。embedding vector自体も元textの派生機密情報として保護する。

### 5.4 更新

Committed Revision公開後、Runtimeは前revisionとのSearchDocumentのcanonical physical digest差分からindex更新を計画する。physical digestはaddress、kind、owner、lexical text、field projection、graph / type / layer address、`content_hash`、存在する場合はembedding vectorを含む。`content_hash`だけが一致しても物理rowの再利用根拠にはしない。

- unchanged document: embeddingとindex entryを再利用できる。
- changed document: lexical entryとembeddingを置換する。
- deleted document: commit publication後に検索対象から原子的に除外する。
- index更新未完了: revisionを偽って古い結果を返さず、`search.index_not_ready`を返す。

Working DocumentはCommitted Search Indexへ混ぜない。編集画面の未commit preview検索が必要な場合は、Working Document専用の一時indexとしてrevision / sessionを分離する。

## 6. Search Request / Result

### 6.1 Request

```text
SearchRequest
  document_handle
  document_generation
  document_snapshot_ref
  text
  mode: lexical | semantic | hybrid
  target_kinds[]
  layer_addresses[]?
  type_addresses[]?
  relation_type_addresses[]?
  owner_addresses[]?
  max_hits
  max_output_bytes
  cursor?
  search_profile_id
  embedding_profile_id?
```

`text`は空であってはならず、`target_kinds`は1件以上、`max_hits`と`max_output_bytes`は1以上かつCapabilityManifest上限以下とする。optional filterの省略は無制限、明示的な空listはvalidation errorとする。`semantic`と`hybrid`はEmbedding Profileを必須とする。filterは候補生成前に適用する。`max_hits`を全件取得の代用にしてはならない。

### 6.2 Result

```text
SearchResult
  document_snapshot_ref
  index_identity_digest
  mode
  query_digest
  hits[]
  result_truncated
  next_cursor?
  diagnostics[]
  search_result_hash

SearchHit
  subject_address
  subject_kind
  owner_address?
  graph_entry_addresses[]
  rank
  score
  score_signals
    lexical_rank?
    lexical_score?
    semantic_rank?
    semantic_distance?
    fused_score?
  matched_source_refs[]
  bounded_snippets[]
  content_hash
```

`rank`は1から始まる連続integerとする。結果は`rank`昇順、同scoreはStableAddress昇順でrankを確定する。raw embedding、backend row ID、index内部ID、backend query textを返さない。snippetとhighlightもAccess適用済みsource spanからだけ作る。

cursorはDocumentSnapshotRef、query digest、index identity、Access fingerprintへ署名付きで束縛する。別revision / generation、別Actor、別filterへの流用を拒否する。

### 6.3 Hybrid ranking

BM25 scoreとvector distanceを直接加算してはならない。既定Hybrid Searchはrank-based Reciprocal Rank Fusionを使う。

```text
fused_score(address) =
  lexical_weight / (rrf_k + lexical_rank)
  + semantic_weight / (rrf_k + semantic_rank)
```

rankは各候補listで1から始める。既定値はversioned Search Profileに保存し、同profile内では固定する。片方に存在しない候補の項は0とする。候補unionを`fused_score`降順、StableAddress昇順で並べ、1からrankを振る。別fusionを追加する場合もprofile ID、version、digestを変え、同じ名前の意味を変更しない。

## 7. Structural Queryとの接続

LDL Queryはexact selector、typed predicate、明示root、directed traversalを保存する。FTS query text、embedding vector、SearchHit score、HNSW parameter、Ladybug procedure名をLDL Queryへ保存しない。

SearchからQueryを作る操作は次を行う。

1. SearchHitのDocumentSnapshotRefとcontent hashを再検証する。
2. AIまたはユーザーが対象subjectとgraph entryを選択する。
3. StableAddressをQueryの`select roots`へ明示するか、型付きpredicateへ一般化する。
4. RelationType、方向、深さ、cycle policy、result inclusionを明示する。
5. Go Workbenchでpreviewし、semantic diffとQuery結果を確認する。
6. RuntimeでLDLへcommitする。

Query実行結果は`document_snapshot_ref`、保存済み`query_address`、typed `arguments`、固定した`state_policy` / `state_input`、canonicalなEntity / Relation address集合、paths / cycle refs、diagnosticsを返し、全体を`query_result_hash`へ束縛する。backendの任意row mapや内部IDを規範結果として公開しない。

検索条件から属性predicateへの一般化をLLMの推測だけで自動commitしてはならない。候補predicateと影響件数をpreviewし、承認対象にする。

## 8. Graph Analysis

### 8.1 Scope

Graph Analysisは次のいずれかを入力とする。

- canonical QueryResultのhashと完全なselected subgraph。
- revisionに束縛された明示Entity / Relation address集合。

2形式は排他的であり、必ず片方だけを指定する。明示集合はRelationの両endpointを含み、全addressが同じDocumentSnapshotRefへ解決されなければならない。

Search cursorや「上位N件」という可変順位だけをanalysis scopeにしてはならない。SearchHitを使う場合はaddress集合へmaterializeし、DocumentSnapshotRefを固定する。Access適用前のProject全体を暗黙scopeにしない。

### 8.2 Algorithms

標準algorithmは次である。

| Algorithm | 入力 | 主出力 |
| --- | --- | --- |
| PageRank | directed subgraph、damping、iteration / tolerance | Entityごとのimportance score |
| K-Core | subgraph | Entityごとのcore number |
| Louvain | subgraph、weight selector、seed / profile | Entityごとのcommunity ID、modularity |
| SCC | directed subgraph | strongly connected component ID |
| WCC | directionを無視したanalysis projection | weakly connected component ID |

WCCはMaster Graphを無向化する機能ではない。分析時だけ各有向Relationを連結判定用projectionへ写す。Structural Queryの方向、Relation identity、View projectionは変更しない。

### 8.3 Request / Result

```text
AnalysisRequest
  document_handle
  document_generation
  document_snapshot_ref
  scope
    query_result_hash?
    entity_addresses[]?
    relation_addresses[]?
  algorithm
  parameters
  max_output_bytes

AnalysisResult
  document_snapshot_ref
  input_subgraph_hash
  algorithm
  algorithm_profile_id
  backend_version
  values[]
    subject_address
    metric_name
    typed_value
  summaries[]
  diagnostics[]
  result_hash
```

floating pointはprofileで定めた正規化と直列化を行い、同値時はStableAddressで順序を決める。乱数を使うalgorithmは固定seedまたはprofileで明示したseed policyを必須とする。それでもbackend versionを跨ぐbitwise同一性を主張せず、結果にprofileとbackend versionを束縛する。

AnalysisResultはEntity / Relation属性へ暗黙書込みしない。分析値を正本へ採用する場合は、別のsemantic operationとして対象column、値、provenanceを明示し、通常のpreview / commitを通す。

## 9. MCPによるAIワークフロー

MCPは検索・探索・分析の主要操作面である。AIにraw Cypher、Ladybug procedure、index bytes、NormalizedDocument全文を既定公開しない。

規範workflowは次である。

```text
1. get_capabilities
2. list_modules / list_references / read_references
3. search
4. inspect_subgraph
5. run_query
6. analyze_graph
7. preview_operations / preview_fragment
8. materialize_view / render preview
9. apply_operations
```

規範toolを追加する。

| Tool | 目的 |
| --- | --- |
| `layerdraw.search` | lexical / semantic / hybridでsubject候補を取得する |
| `layerdraw.inspect_subgraph` | 選択したEntityを中心に型、Layer、rows、incoming / outgoing Relationをbounded取得する |
| `layerdraw.analyze_graph` | QueryResultまたは明示subgraphへ標準algorithmを適用する |

`find_symbols`はID、name、kind、module等によるシンボル解決であり、Project Searchの代用ではない。`get_neighbors`は単純な隣接取得、`inspect_subgraph`はAI向けに必要な型・row・Relationを1つのbounded envelopeへまとめるconvenience operationである。両者は同じLayerDraw Engine indexとAccess結果を使う。

すべてのMCP resultは次を満たす。

- host-backedではHost Document IDとrevision、portableではsource tree digestとgenerationをDocumentSnapshotRefとして返す。
- StableAddressとsubject hashを次のwrite preconditionへ再利用できる。
- `max_items`、`max_output_bytes`、cursorでboundedにする。
- score理由、matched field、truncationを明示する。
- sourceやrow本文は要求されたinclude flagの範囲だけ返す。
- Access拒否をempty resultへ偽装しない。

## 10. UI操作面

UIとMCPは同じEngine / Runtime operationを使い、別の検索実装を持たない。

### 10.1 通常ユーザー

- 自然言語またはkeyword検索
- lexical / semantic / hybridのmode選択
- Layer、Type、subject kindによるfilter
- hitから周辺構造を展開
- 選択hitをQuery rootへ固定

### 10.2 Query Editor

Query EditorはLDL Structural Queryをno-codeで編集する。

- explicit roots
- Layer / EntityType / RelationType
- typed predicates
- direction、depth、cycle policy
- result inclusion

Search modeやscore thresholdをStructural Queryの隠しfieldとして保存してはならない。

### 10.3 Graph Analysis

Analysis UIは対象Queryまたは明示selection、algorithm、parameterを表示し、score / component / communityを表、overlay、filter候補として確認できる。AnalysisResultを正本属性やView selectionへ採用する操作は別のpreview / commitとして表示する。

raw Cypher editorは開発・診断用に限定できるが、raw queryをLDL Query、View、MCPの規範APIにしてはならない。

## 11. 実装責務

### 11.1 LayerDraw Engine

- SearchDocumentとSearch Profileの規範化
- search / analysis request validation
- filter、fusion、ordering、deduplication
- SearchResult / AnalysisResultのcanonicalization
- SearchHitからStructural Queryを作るsemantic operation preview
- Structural Query planner、QueryResult、ViewData

### 11.2 Runtime / Access

- revision固定
- Access projectionとagent scope
- index lifecycle、stale判定、rebuild orchestration
- Embedding Provider呼出
- credential、quota、usage、audit
- Search / Analysis cursorのActor binding

### 11.3 Ladybug execution adapter

- Engineが作ったparameterized execution planの実行
- FTS、Vector、Algo extensionの物理呼出
- typed raw rowsの返却
- index create / update / deleteの物理適用

adapterはSearchDocument生成、Hybrid fusion、StableAddress補完、Access decision、QueryResult / SearchResult / AnalysisResult生成を行わない。

### 11.4 TypeScript / UI / MCP

- generated protocolのtransport
- Search / Query / Analysis UI state
- MCP Appsでのstreaming表示
- ViewData / Analysis overlayのrender

TSはLDL、search corpus、ranking、algorithm resultを独自解釈しない。browserのEmbedding ProviderやLadybug WASM adapterもHost Adapterであり、意味論を所有しない。

## 12. Host Ports

```go
type EmbeddingProvider interface {
  Describe(context.Context) (EmbeddingCapability, error)
  EmbedDocuments(context.Context, EmbedDocumentsInput) (EmbeddingBatch, error)
  EmbedQuery(context.Context, EmbedQueryInput) (QueryEmbedding, error)
}

type QueryExecutionPort interface {
  Capabilities(context.Context) (QueryAdapterCapability, error)
  Execute(context.Context, ExecuteQueryPlanInput, QueryRowSink) (QueryAdapterExecutionResult, error)
  ExecuteSearch(context.Context, ExecuteSearchPlanInput, SearchHitSink) (QueryAdapterExecutionResult, error)
  ExecuteAnalysis(context.Context, ExecuteAnalysisPlanInput, AnalysisRowSink) (QueryAdapterExecutionResult, error)
  Cancel(context.Context, CancelQueryExecutionInput) error
}

type SearchIndexStore interface {
  Describe(context.Context, SearchIndexIdentity) (SearchIndexStatus, error)
  ApplyPlan(context.Context, SearchIndexPlan) (SearchIndexApplyResult, error)
  Activate(context.Context, ActivateSearchIndexInput) (SearchIndexStatus, error)
  Invalidate(context.Context, InvalidateSearchIndexInput) error
}
```

実際のGo packageはprovider SDKやLadybugをcompile-time compositionする。Engineはconcrete provider、filesystem、network credential、TS packageをimportしない。browser境界では同じgenerated plan / raw row schemaをWorker messageへmapする。

## 13. Failure Contract

| Code | Meaning |
| --- | --- |
| `search.invalid_request` | text、mode、filter、limit、profileが不正 |
| `search.index_not_ready` | 要求revisionの完全なindexが未公開 |
| `search.index_stale` | index identityが要求revision / profileと不一致 |
| `search.embedding_unavailable` | 必須Embedding Providerが利用不能 |
| `search.embedding_profile_mismatch` | model、dimension、normalization等がindexと不一致 |
| `search.capability_missing` | FTS / Vector primitiveが不足 |
| `search.raw_schema_mismatch` | adapter hit rowがplan schemaと不一致 |
| `search.token_invalid` | tokenがexpired、consumed、別generation / snapshot |
| `search.cursor_invalid` | DocumentSnapshotRef、query、Actor、index identityが不一致 |
| `search.access_denied` | search scope自体が許可されない |
| `search.result_limit_exceeded` | bounded result上限を超えた |
| `search.backend_failed` | safeに正規化されたbackend failure |
| `search.cancelled` | 呼出側またはdeadlineによるcancel |
| `analysis.invalid_scope` | subgraph closureまたはhashが不正 |
| `analysis.algorithm_unsupported` | algorithm primitiveが不足 |
| `analysis.raw_schema_mismatch` | adapter metric rowがplan schemaと不一致 |
| `analysis.token_invalid` | tokenがexpired、consumed、別generation / snapshot |
| `analysis.result_limit_exceeded` | bounded result上限を超えた |
| `analysis.backend_failed` | safeに正規化されたbackend failure |
| `analysis.cancelled` | 呼出側またはdeadlineによるcancel |

Vector障害時にHybridをFTSだけで成功扱いせず、`search.embedding_unavailable`または`search.capability_missing`で拒否する。利用者が明示的に`mode: lexical`で再実行する。

## 14. Conformance

公式releaseは次をnative、Node WASM、browser single-thread WASM、browser multithread WASMで検証する。

1. FTS index作成とBM25順位。
2. HNSW index作成、filtered vector search、dimension mismatch拒否。
3. Hybrid RRFの同一input / profileに対する同一順位。
4. PageRank、K-Core、Louvain、SCC、WCCのschemaとStableAddress binding。
5. SearchHitからexplicit root Queryをpreviewした時のnative / WASM一致。
6. Access filterが候補生成前に適用され、件数、snippet、scoreから秘匿subjectが漏れないこと。
7. revision変更後にstale cursor / stale indexが拒否されること。
8. MCPとGUIが同じrequestで同じdomain resultを受け取ること。
9. official bundleのCapabilityManifestから必須primitiveが1つでも欠けた場合にrelease gateが失敗すること。

backend固有のraw scoreや浮動小数のbitwise一致を、profileが保証しない範囲で要求しない。ただしcanonical ordering、StableAddress binding、fusion、filter、failure code、source refsは一致させる。

## 15. 実装根拠

LadybugDBの物理能力は公式の[Extensions](https://docs.ladybugdb.com/extensions/)、[Full Text Search](https://docs.ladybugdb.com/extensions/full-text-search/)、[Vector Search](https://docs.ladybugdb.com/extensions/vector/)、[Graph Algorithms](https://docs.ladybugdb.com/extensions/algo/)、[WASM API](https://docs.ladybugdb.com/client-apis/wasm/)を参照する。ただしLayerDrawの必須primitive、profile固定、failure、native / WASM適合性は外部ドキュメントへ委譲せず、本書とLayerDraw release conformanceが保証する。
