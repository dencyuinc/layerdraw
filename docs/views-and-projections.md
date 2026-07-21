# View / Projection / Query 方針

## 1. 基本方針

LayerDraw の出発点は、多層・多次元の構造を 1 つの正本グラフとして保持し、必要な切り口を View として切り出すことである。

従来 Excel sheet や個別図面で手動管理していた成果物は、LayerDraw では View として定義する。View は静的コピーではなく、正本グラフに対する永続的な抽出・投影レシピである。

```text
Master Graph
  Entities
  Relations
  Layers
  EntityTypes
  Attribute rows

View definitions
  業務フロー
  データフロー
  アプリケーション構成
  OS / VM
  ハードウェア
  ネットワーク
  本番 / DR環境
  責任分界
  影響範囲
  データリネージ

Projection
  View + Master Graph -> Diagram / Table / Export
```

正本グラフが変われば、View から生成される図や表も自動的に変わる。これにより、各 Excel sheet / 図面 / 設計資料を手作業で同期する運用を避ける。

View System の詳細な型変換契約は [view-conversion-contract.md](view-conversion-contract.md) に置く。
実装は必ず以下の順で進める。

```text
Master Graph + ViewRecipe
  -> ViewData
    -> RenderData
    -> ExportPlan -> ExportArtifact + Source Manifest
```

UI で View 種別を追加する前に、対象 ViewData、変換規則、lossless / traceable-summary / lossy の分類、export capability を定義する。

## 2. View は永続データ

Viewはruntimeの一時状態ではなく、project-local `.ldl` source treeにViewRecipeとして保存し、`.layerdraw`へそのsource treeを格納する。

理由:

- View は成果物に相当する
- View はチームで共有される
- AI / MCP agent が View を作成、更新、参照する
- export / preview / package generation は View を入力にする
- 本番 / DR環境、地域、責任分界などの分類軸もView定義に含める
- `.layerdraw` を渡した相手も同じ View を再生成できる必要がある

標準構成ではViewRecipeを`views/`配下へ置き、`document.ldl`の明示的なimport/export closureから選択する。単一ファイル構成も合法である。`.layerdraw`はproject-local source treeと`document.json`を含め、必要ならViewごとのpreview / export artifactを同梱する。

## 3. View と Projection の関係

ViewRecipe:

- ユーザーが名前を付けて保存する切り口
- category、intent、Query / Diff source、typed shape、RelationType projection override、export recipeを持つ
- `.ldl` に永続化する

ViewData:

- View を実行した結果
- Renderer や exporter が扱う semantic な中間表現
- 原則として永続化しない
- cache してもよいが、正本ではない

```text
View definition
  -> execute against current graph
    -> ViewData
      -> RenderData
      -> SVG / PNG / table / CSV / XLSX / PDF / MCP Apps preview
```

## 4. View が持つ条件

View は最低限、以下を持つ。

- id
- display name
- category
- optional intent
- exactly one Query / Diff source
- exactly one typed shape
- optional RelationType projection overrides
- zero or more export recipes

Layer、root、traversal、EntityType / RelationType filter、typed predicateはViewへ重複保存せず、参照先Queryが保持する。表示上の階層展開・集約はRelationType projectionとtyped shapeからViewData occurrenceへ変換する。

```ldl
view production_infrastructure "本番インフラ構成" topology {
  source query production_infrastructure_scope {}
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
  }
}
```

## 5. Query-backed View

browserではLayerDraw Engine WASMがQueryExecutionPlanを生成し、TS LadybugDB WASM adapterがそれを機械的に実行できる。TSはQueryを構築・解釈せず、typed raw rowsをLayerDraw Engineへ返す。

この仕組みはProject Searchとは異なり、View切り出しの決定論的基盤として扱う。FTS / Vector / Hybrid Searchは探索起点候補を発見するUI / MCP operationであり、そのrankやscoreをView sourceにしない。確認済みStableAddressをQuery rootへ固定してからViewへ接続する。

View の抽出条件は、以下を設計対象にする。

1. explicit filters
   - layers
   - entityTypes
   - relationTypes
   - roots
   - depth

2. no-code query builder
   - attribute filter
   - relation hop filter
   - layer / type / name filter

3. saved graph query
   - typed predicate tree
   - named query
   - parameterized query

`.ldl`にはCypher / SQLなどbackend固有のquery stringを保存しない。no-code query builderとMCPは同じtyped Query recipeを生成し、LayerDraw EngineがLadybugDB等のparameterized QueryExecutionPlanへcompileする。

PageRank、K-Core、Louvain、SCC、WCCのAnalysisResultもView sourceではない。分析結果からViewを作る場合は、採用するStableAddressまたは一般化したtyped predicateをQuery recipeとしてpreview / commitする。

Query-backed Viewはcanonical QueryResultのEntity / Relation集合とsource referencesをGo View Materializerへ渡す。

```text
Saved View
  filter/query definition
  -> graph engine
    -> matching entity ids / relation ids
      -> projection
```

## 6. Sheet としての View

LayerDraw の View は、従来の Excel sheet に近い役割も持つ。

Excel sheet 的な用途:

- 業務フロー sheet
- データフロー sheet
- アプリケーション一覧 sheet
- OS / VM 対応表 sheet
- ハードウェア構成 sheet
- ネットワーク接続 sheet
- 本番環境 sheet
- DR環境 sheet
- 移行対象一覧 sheet
- 影響範囲 sheet

LayerDraw では、これらをコピーされた表ではなく、正本グラフから生成される View として扱う。

Notion database や GitHub Projects に近く、元データの本体は同じで、見せ方と export が変わる。
LayerDraw ではその本体が rows だけではなく、graph、layers、entity types、node attribute tables、relations である。

View は diagram だけでなく table export を持てる。

Table view の基本はノードに紐づく属性表である。
ただし Excel export は汎用 table dump ではない。
ViewCategory と ViewShape から自然な workbook profile を自動決定し、View の意味を媒体に合わせて表現する。

- table view: EntityType ごとの sheet、Relation sheet、Lookup sheet
- matrix view: matrix sheet、axis sheets、cell source relation sheet
- diagram view: occurrence / edge / overlay / badge inventory sheet、layer sheet、review checklist
- tree / impact view: tree paths、depth、duplicates、cycles、source refs
- flow view: steps、connectors、lanes、branches / joins
- diff view: changes、added、removed、updated、before / after refs

Dynamic view と Excel export は同じ ViewData から生成する。
Excel では stable ID と structured reference / `XLOOKUP` でノード間参照をつなぐ。

PDF / PPTX / DOCX には 2 種類ある。

- plain export: ViewData をそのまま PDF snapshot、PPTX visual slide、DOCX appendix として出す
- Document Generation: design document、approval report、review pack などのビジネス文書を template から生成する

Document Generation は View System の本体ではない。
ViewData / plain export の契約を変えず、別機能として ViewData を入力にする。

```text
Master Graph + ViewRecipe + optional immutable StateQuerySnapshot
  -> QueryResult
  -> View
  -> DiagramViewData
  -> TableViewData
  -> MatrixViewData
  -> TreeViewData
  -> FlowViewData
  -> ContextViewData
  -> DiffViewData
  -> SVG / PNG / CSV / XLSX / JSON / YAML / Markdown / HTML / PDF / PPTX / DOCX / Mermaid / BPMN / draw.io export
```

ただし export は ViewData から生成する。正本グラフから直接 CSV や SVG に飛ばしてはいけない。
State依存recipeはLDLへstate値を埋め込まず、Runtimeが固定したStateInputRefを使う。QueryはEntity/Relation/rowのstandard state fieldをfilterでき、Tableだけが`source state` Columnとして値を直接表示・sort・aggregateできる。他shapeはQuery selectionを利用する。ViewData/Source ManifestはStateRefsと、snapshot利用時はsnapshot hash、optional no-state評価時は規範none入力を保持する。
各ViewCategory / ViewShapeがどの形式をlossless / traceable-summary / visual-only / lossyとして出せるかは、[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) 7.9節の規範マトリクスで管理する。
同節のExportPlanとSource Manifestがsemantic mappingと追跡可能性を所有し、font/layout/rasterizer/各ファイルserializerは明示的なversioned exporter profileが所有する。

## 7. 非時間的な分類軸

同じMaster Graphを、deployment environment、region、responsibility boundaryなどの分類軸で切り出せるようにする。

たとえば本番系とDR系を分ける場合、EntityType / RelationTypeが宣言したtyped `environment`列へ`production`または`disaster_recovery`を入れ、通常のtyped Queryで選択する。自由形式metadataやLayer名を暗黙のQuery条件として解釈しない。`tags`をad-hocな分類に使うことはできるが、標準templateで意味を固定する分類はtyped columnを使う。

Coreは`environment`、`region`、`responsibility`等のdomain値を特別扱いしない。Pack / Templateが列schemaとenum値を定義し、Query / Viewは通常のattribute predicateとして扱う。分類軸を追加してもMaster Graphの正本型やViewSourceRecipeを拡張しない。

状態の前後関係を示唆するラベルは、非時間的な分類機能の標準例に使わない。利用者が通常のtagやtyped属性として任意の分類値を定義することは妨げないが、Coreは専用scenario semanticsを持たない。

LayerDrawはtemporal graphを提供しない。valid-time / transaction-time、`AS OF` query、期間・window traversal、temporal relation / cardinality、bitemporal modelはGraph / Query / Viewの規範外である。date / datetime列は通常のscalarとして比較できるが、Engineは履歴軸として解釈しない。revision、Audit、Time MachineはMaster Graphの時間dimensionではない。

## 8. View と更新伝播

View は正本グラフを参照するため、以下の更新は自動的に反映される。

- Entity の名前変更
- Entity の layer 移動
- Relation の追加 / 削除
- attributeRows の更新
- EntityType の column 変更
- containment Relationの追加、削除、endpointまたはRelationType projectionの変更
- typed `environment` / `region` / `responsibility`属性の変更

View 自体が保存するのは、表示対象のコピーではなく、抽出条件と表示意図である。

## 9. AI / MCP との関係

AI / MCP agent は View を第一級データとして扱う。

必要な操作:

- list views
- get view definition
- preview view
- create view
- update view
- delete view
- explain view coverage
- find entities not covered by any view
- propose view from natural language
- generate environment-specific view
- export view

AIは個別の図形座標ではなく、Queryのselection / predicate / traversalと、ViewRecipeのcategory / source / typed shape / projection overrideを操作する。

例:

```text
"本番環境の受注業務からDBとネットワークまで辿るビューを作って"
  -> create_subject Query production_order_scope
     select.layers: [business, application, data, os_vm, hardware, network]
     select.roots: [business_order]
     select.relationTypes: [calls, writes, runs_on, connected_to]
     where: attribute.environment = "production"
     traverse: outgoing 0..5 visit_once
  -> create_subject View production_order_impact
     category: impact
     source: production_order_scope
     shape: diagram
```

## 10. Export と Artifact

`.layerdraw` packageはeffective documentで選択されたView定義を含むproject-local source treeを必ず含む。

任意で同梱できるもの:

- View preview image
- View SVG export
- View PNG export
- View table CSV
- View report PDF

これらは artifact であり、正本ではない。正本は `.ldl` 内の graph と View 定義である。

## 11. 実装方針

対応するもの:

- Query / ViewRecipeをproject-local `.ldl` source treeの正本として維持する
- View Composer を「Excel sheet 相当の切り出し作成 UI」として強化する
- View preview / export を `.layerdraw` package に含められるようにする
- Query-backed View を導入する
- no-code query builderの条件をtyped Query declarationとして保存し、ViewRecipeはQueryを参照する
- table projection / CSV export を追加する
- typed `environment` filterの例をインフラ標準templateに入れる
- backend queryへcompile可能なsaved typed Query
- parameterized Query / View source arguments
- View dependency analysis
- CSV / XLSX / JSON / Markdown / HTML / PDF / PPTX / DOCX / Mermaid / BPMN の plain export contract

View System から分けるもの:

- generated document update pipeline
- design document / approval report / review pack generation
- narrative section templates
