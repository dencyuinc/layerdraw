# LDL Compiler / Workbench Architecture

## 1. 目的

この文書は LDL の規範意味論を実装する LayerDraw Engine のうち、Compiler、Workbench、Search、Query、Graph Analysis、View、Export の境界を定める。構文そのものは LDL 言語仕様を参照し、この文書では「何を入力し、何を出力し、何を担当しないか」を固定する。検索・Query・分析の意味分離は[search-query-and-analysis.md](search-query-and-analysis.md)、Engine Protocol、execution adapter、Render / Exportのwire operationと状態遷移は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

## 2. Compiler の純粋入力

Compiler は次の閉じた入力だけを受け取る。

```text
CompileInput
  compile_mode
  entry_path
  root_pack_id
  project_source_tree[path] = UTF-8 bytes
  installed_pack_tree[path] = verified UTF-8 / asset bytes
  resolved_dependencies
  referenced_asset_digests
```

- `entry_path` は source tree 内の canonical project-relative path である。
- `root_pack_id` は Pack compile のときだけ必須の canonical `publisher/pack-name` selector であり、Project compile では空でなければならない。これはCompileInputのroot選択metadataで、LDL semantic identityへ含めない。
- LDL source fileはversion headerを持たない。Compilerは自身のsupported LDL generation、release manifest、container/package metadata、migrator capabilityに基づいてsource syntaxを解釈し、未対応構文はdiagnosticとして返す。
- import の network fetch は Compiler 外で完了していなければならない。
- Pack は導入・展開・digest 検証済みの immutable tree として渡す。
- backend binding、credential、Actor、Document ID、revision、current clock は CompileInput に含めない。
- 同じ CompileInput は host、OS、transport に依存せず同じ意味結果を返す。

## 3. Compiler の出力

```text
CompileResult
  lossless_syntax_tree
  typed_ast
  normalized_document
  semantic_index
  source_map
  stable_addresses
  definition_hash
  graph_hash
  subject_semantic_hashes
  subtree_hashes
  child_set_hashes
  authoring_subject_classification
  compiled_query_recipes
  compiled_view_recipes
  compiled_export_recipes
  search_documents
  diagnostics
```

### 3.1 LosslessSyntaxTree

コメント、trivia、引用形式、source range を保持する。局所 rewrite と human-readable diff の基盤であり、NormalizedDocument から復元しない。

### 3.2 TypedAST

name resolution、型、default、endpoint 制約を解決した内部表現である。外部 API が AST の内部 layout に依存しないよう、protocol では必要な projection だけを公開する。

### 3.3 NormalizedDocument

言語仕様で定義された canonical semantic model である。renderer の都合を混ぜず、host metadata や state を埋め込まない。

### 3.4 SemanticIndex と SourceMap

StableAddress、kind、module、owner、参照元、参照先、source span、row / column address を索引する。MCP の局所読取、IDE navigation、影響範囲、局所再検証に使う。

Project Search用のSearchDocumentもNormalizedDocumentとSourceMapからLayerDraw Engineが決定論的に生成する。Embedding vector、Ladybug index内部ID、Actor固有Access結果はCompiler出力に含めない。

### 3.5 Hashes

- definition hash: 規範 definition 全体
- graph hash: graph semantics 全体
- own-subject hash: 対象 declaration 自身
- subtree hash: owner と所有 subtree
- child-set hash: owner 配下の kind ごとの membership

hash input と canonicalization は LDL 詳細仕様に従い、TS や host が再計算しない。

## 4. Compiler pipeline

```text
bytes
  -> lexical analysis
  -> lossless CST
  -> module graph
  -> declaration collection
  -> import / export resolution
  -> StableAddress construction
  -> name and reference resolution
  -> type / constraint validation
  -> normalization
  -> semantic indexes and hashes
  -> Query / View / Export recipe compilation
  -> SearchDocument generation
  -> diagnostics
```

pipeline 内部を incremental にしてよいが、incremental result は full compile と同じ規範出力になることを conformance test で保証する。

## 5. Compiler が担当しないもの

Compiler は次を担当しない。

- file picker、filesystem walk、Registry network fetch
- backend binding 解決、state read / write
- auth、Actor、ACL 永続化、credential refresh
- Host Document ID、revision head、autosave、lease
- MCP / HTTP / stdio transport
- LadybugDB の lifecycle と physical tuning
- embedding modelの実行、credential、Search Indexの永続化
- visual layout、SVG / Canvas / WebGL render
- ZIP の保存先選択、upload、share workflow
- realtime room、presence、Time Machine

これらが必要な component は bytes、resolved tree、typed input を Compiler 境界まで準備する。

## 6. Workbench

Workbench は編集可能な source tree を扱う Go component である。Compiler と同じ lossless CST / typed model を使用するが、compile と編集を一つの副作用 API に混ぜない。

Workbenchは全previewでbefore / afterのNormalizedDocument、semantic diff、source diffからAuthoringImpactを生成する。ActorやPolicyを受け取らず、allow / denyを決めない。AuthoringImpactの規範分類は[authoring-access-control.md](authoring-access-control.md)に従う。

### 6.1 入力

```text
WorkbenchInput
  base_source_tree
  base_compile_result or document_handle
  edit_request
  preconditions
  format_policy
```

`edit_request` は次のいずれかである。

- semantic operation batch
- scoped LDL fragment
- revision-protected source patch
- explicit format / organize request

### 6.2 出力

```text
WorkbenchResult
  changed_source_files
  source_edits
  semantic_diff
  compile_result
  conflicts
  diagnostics
```

Workbench は変更対象 CST node と必要な参照範囲だけを書き換える。未変更 module、コメント、Reference text、format を不要に churn させない。

### 6.3 semantic operation

通常の GUI / MCP / SDK 更新は StableAddress と precondition hash を持つ semantic operation を使う。

1. target を SemanticIndex で解決する。
2. base revision / subject / subtree / child-set precondition を検査する。
3. lossless CST へ局所変更を適用する。
4. 完全な変更 node だけ canonical format する。
5. Compiler で再検証する。
6. semantic diff と新しい source tree を返す。

Workbench 自身は revision を commit しない。Runtime が結果を storage へ条件付きで公開する。

### 6.4 scoped LDL fragment

大量作成や DSL 固有表現では、AI / user が対象 scope の LDL fragment を渡せる。

- fragment の許可 declaration kind と挿入 owner を request で固定する。
- fragment 単体を Go parser で解析する。
- 既存 symbol と合わせて name resolution する。
- semantic operations または等価な局所 CST edit へ変換する。
- preview で作成・更新・削除・参照影響を返してから apply する。

fragment を文字列連結で source に挿入しない。

fragmentの`allowed_kinds`はparse scopeを狭める入力であり、Authoring Capabilityではない。fragment適用後の完全なsemantic diffからAuthoringImpactを生成し、Runtime / Access判定を省略しない。

### 6.5 raw source patch

コメント、Reference、未知の将来構文など semantic operation で損失なく表せない変更の低レベル経路である。module、source range、expected source digest、base revision を必須とし、compile 成功前に commit しない。

raw source patchもbefore / afterをcompileしてAuthoringImpactを生成する。operation名を隠すことで`schema:write`を迂回できない。semantic diffが空でsource bytesだけが変わるpatchは`source:maintain`へ分類する。

## 7. Search / Query / Graph Analysis pipeline

### 7.1 Search pipeline

Project Searchは保存済みLDL Queryではなく、探索起点を発見する派生operationである。

```text
NormalizedDocument
  -> canonical SearchDocuments
  -> Runtime Access projection
  -> Embedding Provider / Search Index update

SearchRequest + fixed revision + Search Index identity
  -> SearchExecutionPlan
  -> Ladybug FTS / Vector adapter
  -> typed raw hits
  -> LayerDraw Engine ranking / RRF / source binding
  -> canonical SearchResult
```

LayerDraw Engineはcorpus文字列化、filter、Hybrid fusion、StableAddress binding、orderingを所有する。Runtimeはrevision、Access、index lifecycle、Embedding Providerを所有する。execution adapterはFTS / Vectorを物理実行するだけである。

SearchResultはQueryResultの代用ではない。SearchHitをViewへ使う場合、選択したStableAddressを明示rootまたはtyped predicateとしてLDL Queryへ変換し、Workbench previewを通す。

### 7.2 Structural Query pipeline

Query の意味評価は LayerDraw Engine が所有する。

```text
CompiledQueryRecipe
  + NormalizedDocument
  + optional StateQuerySnapshot
  + typed arguments
    -> QueryExecutionPlan
    -> execution adapter
    -> typed raw rows
    -> QueryResult normalizer
    -> canonical QueryResult
```

QueryExecutionPlan は parameterized であり、adapter が文字列補間を必要としない。plan は physical backend の公開 API ではなく Engine と execution adapter 間の versioned internal protocol である。

Runtime は Query 開始時に definition revision と state input を一件へ固定する。Engine は StateQuerySnapshot の schema、access result、hash、definition compatibility を再検証する。

browser / sidecar境界ではQueryを二段protocolで実行する。

```text
prepare_query(document_handle, query_address, arguments, state_input)
  -> query_execution_token
  -> parameterized QueryExecutionPlan

execution adapter(QueryExecutionPlan)
  -> TypedRawRows

complete_query(query_execution_token, TypedRawRows)
  -> canonical QueryResult
```

`query_execution_token`はprocess / Worker内だけで有効なopaque handleであり、document generation、definition / state input digest、plan versionへ束縛する。永続化、別processへの転送、StableAddressの代用を禁止する。`complete_query`はraw row schemaとplan bindingを検証してからQueryResultを規範化する。TS Ladybug WASM adapterは`TypedRawRows`を返すだけで、ordering、deduplication、StableAddress resolution、StateRefsを補わない。native adapterも同じprepare / complete contractを内部的に通し、short pathが別 semanticsにならないようにする。

### 7.3 Graph Analysis pipeline

```text
canonical QueryResult or explicit revision-bound subgraph
  -> AnalysisExecutionPlan
  -> Ladybug ALGO adapter
  -> typed metric rows
  -> LayerDraw Engine StableAddress binding / ordering / normalization
  -> canonical AnalysisResult
```

Analysis scopeはQueryResult hashまたは明示Entity / Relation address集合で閉じる。Search cursor、可変な上位N件、Access適用前のProject全体を暗黙scopeにしない。AnalysisResultはMaster Graph、LDL属性、View selectionを変更せず、採用する場合は別semantic operationとしてpreview / commitする。

## 8. View pipeline

View は正本 graph を変更しない。ViewRecipe と QueryResult から category 固有の ViewData を決定論的に生成する。

```text
ViewRecipe
  + QueryResult or DiffInput
  + RelationType projection rules
  + typed state refs
    -> Go View Materializer
    -> ViewData
    -> TS layout / render
```

Go が所有するもの:

- occurrence identity
- source Entity / Relation / row / column refs
- grouping、lane、hierarchy、matrix cell、table cell の意味
- relation projection と containment rules
- deterministic order と omission diagnostics
- ViewData hash と source binding

TS が所有するもの:

- size measurement
- visual layout coordinates
- routing、zoom、selection
- theme、font、SVG / Canvas / WebGL representation

TS の layout が変わっても ViewData の意味は変わらない。

## 9. Export pipeline

```text
ViewRecipe + ViewData + export recipe
  -> Go Export Planner
  -> ExportPlan
  -> format serializer
  -> ExportArtifact + Source Manifest
```

ExportPlan は、出力すべき semantic section、sheet / page / slide、source refs、ordering、fidelity、required assets を定める。visual serializer は TS、native library、または service 実装でよいが、ViewData を独自再解釈して別の文書意味を作らない。

`.layerdraw` / `.ldpack` container writer は LayerDraw Engine の package component を使う。plain visual export と portable container export を同じ責務にしない。

## 10. Document handle と大規模入力

Engine Protocol は source tree 全体を毎回送らずに済む opaque `document_handle` を提供してよい。

- handle は process / Worker session 内だけで有効である。
- 永続 ID や StableAddress の代わりにしない。
- handle は source digest と compile options に束縛する。
- 変更後は新 generation を返し、stale handle を拒否する。
- cache eviction 後は同じ CompileInput から再構築できる。

局所 API は SemanticIndex を使い、全 NormalizedDocument を返さない。これは性能最適化であり、言語意味を分割するものではない。

## 11. Build targets

同一 Go source から次を build する。

Go package path、single-module方針、binary compositionは[component-package-boundary-specification.md](component-package-boundary-specification.md)に従う。

- in-process Go library
- browser 用 Go WASM
- stdio 用 `layerdraw-engine` native sidecar
- local Runtime用`layerdraw-host`へlinkされるlibrary
- `layerdraw-server` に link される library
- Wails backend に link される library

build tag や adapter 差分で parser、hash、SearchResult、QueryResult、AnalysisResult、ViewData の意味を変えてはならない。platform 差分は filesystem、network、embedding、Ladybug execution、clock などの port 実装に限定する。

## 12. Test obligations

- grammar positive / negative corpus
- lossless parse / print round trip
- canonical format idempotence
- StableAddress / hash golden vectors
- full compile と incremental compile の一致
- semantic operation と scoped fragment の同値性
- semantic operation、fragment、source patch、import、Registry plan、restore diffのAuthoringImpact同値性
- conflict / precondition vectors
- native / WASM / sidecar の protocol conformance
- native / Node WASM / browser single-thread WASM / browser multithread WASMのFTS、Vector、Hybrid、PageRank、K-Core、Louvain、SCC、WCC conformance
- Access適用前のSearch候補、score、snippetが漏れないこと
- ViewData / ExportPlan golden vectors
- `.layerdraw` / `.ldpack` canonical package vectors

これらを通過しない artifact は同じ LayerDraw Engine implementation として配布しない。
