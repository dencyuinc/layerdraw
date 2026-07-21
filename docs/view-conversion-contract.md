# View Conversion Contract

LayerDraw の View System は、ビューの名前や UI から設計してはいけない。
正本モデルから対象ビューのデータ型へ変換できること、さらにそのデータ型から renderer / exporter 用の型へ変換できることを先に契約化する。
RelationType の意味、制約、projection rules、render hints が固まっていない状態で matrix、flow、impact、tree、composed diagram を進めてはいけない。
RelationType System は [relation-type-system.md](relation-type-system.md) に定義する。

RenderRecipe / RenderData、ExportPlan / serializer、determinism、Source Manifest、Document I/O / Plain Export / Document Generationの境界は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 8章を規範とする。

## 1. Non-Negotiable Model Boundary

正本は常に以下である。本書はproduct-facingな変換境界を説明する。正確なfield、typing、validationは[ldl-language-specification.md](ldl-language-specification.md)、Query評価とViewData materializationの決定的意味論は[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)を規範とする。

本書のcamelCase疑似型とTypeScript interfaceはhost-language binding例であり、canonical normalized/wire fieldは規範仕様のlower_snake_caseへadapterで変換する。

```text
NormalizedProjectDefinition
  layers: Layer[]
  entityTypes: EntityType[]
  relationTypes: RelationType[]
  entities: Entity[]
  relations: Relation[]
  queries: QueryRecipe[]
  views: ViewRecipe[]
  references: Reference[]

Entity
  address: StableAddress
  typeAddress: StableAddress
  layerAddress: StableAddress
  displayName
  description
  attributeRows: AttributeRow[]

AttributeRow
  address: StableAddress
  values: Record<columnAddress, scalar>

Relation
  address: StableAddress
  typeAddress: StableAddress
  fromAddress: StableAddress
  toAddress: StableAddress
  displayName
  description
  attributeRows: RelationAttributeRow[]

RelationAttributeRow
  address: StableAddress
  values: Record<columnAddress, scalar>
```

どの View を作っても正本グラフは変わらない。
Query、filter、traversal は正本から ID 集合と関係集合を選ぶだけであり、Entity / Relation / Attribute rows の意味を破壊してはいけない。

## 2. Two-Step Conversion

LayerDraw の変換は必ず 2 つの明示的な変換に分ける。

```text
Step 1: Semantic materialization
  Master Graph + ViewRecipe
    -> ViewData

Step 2a: Presentation generation
  ViewData + RenderRecipe
    -> RenderData

Step 2b: Export planning and serialization
  ViewData + ExportRecipe + host-resolved ExportProfileRequirements
    -> ExportPlan
      -> ExportArtifact + Source Manifest
```

Step 1はrenderer非依存であり、LayerDraw EngineのView Materializerだけが規範実装する。
Web、desktop、VSCode、SDK、MCP Server、MCP Apps、batch export が同じ ViewData を得られなければならない。

Step 2aは出力先依存でありTS Renderが実装する。Step 2bではhostが選択profileのidentity/specificationとexact asset/font requirementsを検証してclosed inputとして渡し、Go Export Plannerがrecipeとの完全一致を検証してsemantic mappingとsource bindingを持つExportPlanを生成し、versioned exporterがformat固有artifactへserializeする。Engineはこの境界でregistry解決を行わない。
SVG、PNG、PDF、CSV、XLSX、JSON、Markdown、HTML、PPTX などは ViewData から生成する。
正本グラフから直接 exporter に飛ばしてはいけない。

## 3. Semantic Types

### 3.1 ViewRecipe

ViewRecipe は「何を」「何のために」「どの形で見るか」を保存する。
ViewRecipeはproject-local `.ldl` source treeに保存され、`.layerdraw`へそのsource treeを格納する。ViewRecipeのruntime表示状態や共同編集presenceはLDLへ保存しない。

```ts
type StableAddress = string;

type ViewCategory =
  | "topology"
  | "inventory"
  | "dependency"
  | "hierarchy"
  | "flow"
  | "impact"
  | "diff"
  | "context";

type ViewShape =
  | "diagram"
  | "table"
  | "matrix"
  | "tree"
  | "flow"
  | "context"
  | "diff";

interface ViewRecipe {
  id: string;
  address: StableAddress;
  displayName: string;
  category: ViewCategory;
  intent?: string;
  source: QueryViewSource | DiffViewSource;
  shape: ViewShapeRecipe;
  relationProjectionOverrides?: Record<StableAddress, RelationProjectionOverride>;
  exports?: ExportRecipe[];
}

interface RelationProjectionOverride {
  composed?: Partial<ComposedProjectionRule>;
  diagram?: Partial<DiagramProjectionRule>;
  table?: Partial<TableProjectionRule>;
  matrix?: Partial<MatrixProjectionRule>;
  tree?: Partial<TreeProjectionRule>;
  flow?: Partial<FlowProjectionRule>;
  context?: Partial<ContextProjectionRule>;
}
```

`source`は保存済みStructural QueryまたはDiffだけを許可する。SearchRequest、SearchHit、Search cursor、AnalysisResultをView sourceへ直接入れない。FTS / Vector / HybridまたはGraph Analysisから選んだ対象は、StableAddress rootまたはtyped predicateを持つQueryへ明示的にmaterializeしてから参照する。

`exports` は生成済み artifact ではない。
「この View をどの媒体へ生成するか」という再生成可能な recipe である。
ExportRecipe は View の意味を変えない。
PDF / PPTX / DOCX などのビジネス文書生成は、plain export ではなく別の Document Generation として扱う。

### 3.2 ViewData

ViewData は Step 1 の結果である。
ViewData は永続正本ではないが、renderer / exporter / SDK / MCP の共通入力である。

```ts
type ViewData =
  | DiagramViewData
  | TableViewData
  | MatrixViewData
  | TreeViewData
  | FlowViewData
  | ContextViewData
  | DiffViewData;

interface ViewDataBase {
  category: ViewCategory;
  shape: ViewShape;
  documentId?: string;
  projectAddress: StableAddress;
  viewAddress: StableAddress;
  revisionId?: string;
  statePolicy: "none" | "optional" | "required";
  stateInput: StateInputRef;
  source: ViewSourceRefs;
  diagnostics: ViewDiagnostic[];
}

interface ViewSourceRefs {
  subjectAddresses: StableAddress[];
  entityAddresses: StableAddress[];
  relationAddresses: StableAddress[];
  layerAddresses: StableAddress[];
  rowAddresses?: StableAddress[];
  cellRefs?: CellRef[];
  state: StateRefs;
}

type StateInputRef =
  | { kind: "none" }
  | {
      kind: "snapshot";
      snapshotHash: string;
      stateVersion: string;
      capturedAt: string;
      definitionHash: string;
    };

interface StateRefs {
  reads: Array<{ subjectAddress: StableAddress; fieldPath: string }>;
}

interface CellRef {
  rowAddress: StableAddress;
  columnAddress: StableAddress;
}
```

全 ViewData は`source.entityAddresses`と`source.relationAddresses`を保持する。StableAddressの構成はLDL規範仕様に従い、local ID、module alias、row indexをsource refとして代用しない。
表示上は集約しても、どの正本要素から生成されたか追跡できなければならない。
ViewData item が attribute row または relation attribute row から生成される場合は、item単位のsource refsにrow StableAddressを含める。特定cellに由来する場合はrowとcolumnのStableAddressを組にする。
row index は stable ref として使わない。
state依存ViewDataは、LDL詳細仕様のStateInputRefとStateRefsを保持する。state値をopaque summaryとして自由にjoinせず、閉じたStateFieldPathから生成されたtyped cellまたはQuery selectionだけを使う。Audit log、lock、lease、presenceをViewDataへ載せない。State fieldを直接表示できるshapeはTableであり、他shapeはstate依存Queryによるselectionを利用できる。

RelationType は ViewData materialization の入力である。
RelationType の projection rule により、同じ Relation instance は View ごとに edge、nested occurrence、overlay、badge、hidden support data のいずれにも変換され得る。
いずれの場合も、生成された ViewData item は source relation refs を保持する。
ViewRecipe の `projection.relationTypeOverrides` は RelationType default projection を View 単位で上書きできる。
上書きはLayerDraw Engineが知っているprojection primitiveとTS Renderが知っているrender primitiveの範囲に限り、endpoint制約やcardinality schemaを変えてはならない。
merge order は deterministic にする。

```text
resolved RelationType declaration
  -> ViewShape common projection fields
    -> ViewRecipe RelationType-specific projection override
```

Pack RelationType と同名の project RelationType を merge しない。pack declaration は canonical pack identity のまま immutable に参照し、意味を変える場合は別の local RelationType ID を定義する。

### 3.3 RenderData

RenderData は Step 2 の中間表現である。
これは表示媒体ごとに異なる。

例:

- `DiagramRenderData`: nodes with geometry, edges with paths, labels, ports
- `TableRenderData`: columns, rows, cells, column widths, frozen columns
- `MatrixRenderData`: row axis, column axis, cell payloads, totals
- `TreeRenderData`: occurrences, cycle refs, duplicate refs, indentation
- `FlowRenderData`: lanes, steps, connectors, branches, joins
- `ContextRenderData`: grouped cards, facts, relation summaries

## 4. View Type Contracts

### 4.1 Diagram

Purpose:

- 多層構造図
- インフラ図
- アプリケーション依存図
- ネットワーク図
- 影響範囲図

ViewData:

```ts
interface DiagramViewData extends ViewDataBase {
  kind: "diagram";
  occurrences: DiagramOccurrence[];
  edges: DiagramEdge[];
  containers: DiagramContainer[];
  overlays: DiagramOverlay[];
  badges: DiagramBadge[];
  supportItems: DiagramSupportItem[];
}

interface DiagramOccurrence {
  occurrenceKey: string;
  entityAddress: StableAddress;
  layerAddress?: StableAddress;
  parentOccurrenceKey?: string;
  viaRelationAddress?: StableAddress;
  role?: "node" | "container" | "support";
  source: ViewSourceRefs;
}

interface DiagramEdge {
  edgeKey: string;
  fromOccurrenceKey: string;
  toOccurrenceKey: string;
  relationAddress: StableAddress;
  relationTypeAddress: StableAddress;
  source: ViewSourceRefs;
}

interface DiagramOverlay {
  overlayKey: string;
  targetOccurrenceKey: string;
  overlayEntityAddress?: StableAddress;
  relationAddress: StableAddress;
  relationTypeAddress: StableAddress;
  source: ViewSourceRefs;
}

interface DiagramBadge {
  badgeKey: string;
  targetOccurrenceKey: string;
  relationAddress: StableAddress;
  relationTypeAddress: StableAddress;
  label?: string;
  source: ViewSourceRefs;
}

interface DiagramSupportItem {
  supportKey: string;
  kind: "hiddenRelation" | "hiddenEntity" | "sourceOnly";
  entityAddress?: StableAddress;
  relationAddress?: StableAddress;
  source: ViewSourceRefs;
}
```

Conversion:

- Entity -> DiagramOccurrence
- RelationType `projection.composed.mode edge` -> DiagramEdge
- RelationType `projection.composed.mode nest` -> parent / child occurrence tree。Relation は必要に応じて edge としても残す。
- RelationType `projection.composed.mode overlay` -> DiagramOverlay
- RelationType `projection.composed.mode badge` -> DiagramBadge
- RelationType `projection.composed.mode hide` -> DiagramSupportItem
- containment RelationType の `nest` projection -> ViewData 上の parent / child occurrence。Master Entity 自体は移動しない。
- Entity attributeRows -> node detail, table node, tooltip, inspector payload

`nodes` は renderer 向けの派生名として扱えるが、semantic ViewData contract では occurrence を正とする。
同一Entityが複数箇所に現れるため、diagram renderer / exporterはEntity StableAddressではなくViewData-localなoccurrenceKeyをprimary display keyにする。occurrenceKeyは派生表示identityでありLDL StableSymbolではない。

Losslessness:

- selected Entity / RelationのStableAddress、occurrenceKey、source row/cell refsを保持すればsemantic conversionはlossless。
- Layout coordinates、edge path、label placement は派生 presentation なので lossless 対象ではない。

Export:

- SVG: canonical visual export
- PNG: raster snapshot
- PDF: fixed-page visual artifact
- HTML: interactive readonly viewer
- JSON: `DiagramViewData`
- XLSX: diagram workbook。Occurrences、Edges、Containers、Overlays、Badges、SupportItems、SourceRefs sheet を持てる。
- `.layerdraw`: package with recipe and optional artifacts

### 4.2 Table

Purpose:

- ノード型ごとの属性表
- 複数ノードの属性表統合
- オブジェクト一覧
- リレーション一覧
- 移行対象一覧
- OS / VM 対応表
- 属性台帳

LayerDraw の Table は、まず「ノードに紐づく属性表」である。
正本には EntityType の列定義と Entity.attributeRows があり、これをそのまま table view の本体にする。

Dynamic table view は、同じ TableViewData を TanStack Table などの table runtime で表示する。
Excel export は、View の意図を継承した workbook profile で生成する。
汎用 workbook だけを出すのではなく、ViewCategory と ViewShape から sheet 構成を決める。
Export 側に別のユースケース意図を持たせない。

ユーザー向けの分類:

- 属性表: 1 つ、または複数の同型 Entity の attributeRows を表示する。
- ノード一覧: Entity 自体を一覧する。
- リレーション一覧: Relation 自体を一覧する。
- 型別 workbook: EntityType ごとの属性表 sheet と relation sheet をまとめる。

内部的には、表の 1 行が何を表すかを `TableRowUnit` として持つ。

```ts
type TableRowUnit =
  | "attributeRow"
  | "entity"
  | "relation"
  | "relationEndpoint"
  | "group";

interface TableViewData extends ViewDataBase {
  kind: "table";
  mode: "attributeTable" | "entityList" | "relationList" | "typeWorkbook";
  rowUnit: TableRowUnit;
  columns: TableColumn[];
  rows: TableRow[];
}
```

Attribute table:

```ts
interface AttributeTableRow {
  entityAddress: StableAddress;
  entityTypeAddress: StableAddress;
  rowAddress: StableAddress;
  values: Record<StableAddress, scalar>;
  source: ViewSourceRefs;
}
```

Table workbook export:

```ts
interface TypeWorkbookData extends ViewDataBase {
  kind: "table";
  mode: "typeWorkbook";
  typeSheets: EntityTypeSheet[];
  relationSheets: RelationSheet[];
  lookupSheets: LookupSheet[];
}

interface EntityTypeSheet {
  entityTypeAddress: StableAddress;
  sheetName: string;
  rows: AttributeTableRow[];
}
```

This is only the default table workbook profile.
Other view category / shape combinations can derive another Excel profile.

Conversion:

- EntityType.columns -> sheet columns
- Entity.attributeRows -> sheet rows
- Entity id / displayName / layer / type -> fixed metadata columns
- Relation -> relation sheet rows with fromEntityId / toEntityId
- Relation type -> relation sheet or filtered relation table
- containment Relation -> relation sheet and hierarchy occurrence refs

Excel relation handling:

- Every generated sheet keeps source StableAddresses in hidden or protected columns.
- Display columns may use `XLOOKUP` / structured references to show node names, layers, and types.
- Relation sheets are the bridge between type sheets.
- Matrix sheets can be generated from relation sheets with formulas or pivot-like summaries.
- The workbook is an artifact; editing it does not become the source of truth unless explicitly imported through an import pipeline.

Legacy internal row units:

```ts
type LegacyTableRowUnit =
  | "entity"
  | "relation"
  | "entityAttributeRow"
  | "relationEndpoint"
  | "group";
```

UI では「属性表」「ノード一覧」「リレーション一覧」「型別 workbook」と呼ぶ。

Losslessness:

- 属性表はEntity、EntityType、row、columnのStableAddressを保持すればlossless。
- ノード一覧で attributeRows を summary 表示にすると lossless ではない。
- リレーション一覧は Relation ID、from / to Entity ID、Relation type を保持すれば lossless。
- 集約 table は必ず `sourceRefs` と aggregation rule を保持する。

Export:

- XLSX: primary export。EntityType ごとの sheet、Relation sheet、Lookup sheet を持てる。
- CSV / TSV: single table または bundle。型別 workbook は複数 CSV と manifest で表す。
- JSON: `TableViewData`
- HTML: readonly table / workbook viewer。
- Markdown: review / docs 向け。lossy。
- PDF: fixed report。

### 4.3 Matrix

Purpose:

- アプリケーション x データストア
- 業務 x アプリケーション
- VM x ネットワーク
- owner x system
- capability coverage

```ts
interface MatrixViewData extends ViewDataBase {
  kind: "matrix";
  rows: MatrixAxisItem[];
  columns: MatrixAxisItem[];
  cells: MatrixCell[];
}

interface MatrixCell {
  rowKey: string;
  columnKey: string;
  relationAddresses: StableAddress[];
  rowAddresses?: StableAddress[];
  entityAddresses?: StableAddress[];
  value: MatrixCellValue;
  source: ViewSourceRefs;
}
```

Conversion:

- row axis と column axis は Entity set または group set。
- cell は row item と column item の間にある Relation set、または attribute / aggregate から作る。
- direct relation、reverse relation、both、path based relation のどれを見るかを recipe で指定する。

Losslessness:

- cellが`relationAddresses`を保持するdirect adjacency matrixはlossless。
- path based matrixはRelation StableAddressの順序付きpathを保持すれば追跡可能。
- count / boolean / label だけに潰すと lossy。これは presentation value として扱う。

Export:

- XLSX: primary export。matrix sheet、legend sheet、source relation sheet を出せる。
- CSV: matrix values only。source refs は別 CSV が必要。
- JSON: `MatrixViewData`
- HTML: interactive matrix。
- PDF: fixed matrix report。
- SVG / PNG: visual matrix snapshot。

### 4.4 Tree

Purpose:

- 組織階層
- システム構成階層
- container / parent / children 表示
- impact tree
- dependency tree

Graph を Tree にする時点で危険がある。
DAG、cycle、shared child があり得るため、TreeViewData は Entity そのものではなく occurrence を持つ。

```ts
interface TreeViewData extends ViewDataBase {
  kind: "tree";
  roots: TreeNodeOccurrence[];
}

interface TreeNodeOccurrence {
  occurrenceKey: string;
  entityAddress: StableAddress;
  viaRelationAddress?: StableAddress;
  duplicateOfKey?: string;
  cycleToKey?: string;
  source: ViewSourceRefs;
  children: TreeNodeOccurrence[];
}
```

Conversion:

- containment Relation 由来の occurrence tree。
- relation traversal 由来の dependency tree。
- roots、direction、maxDepth、relationTypes、cycle policy を recipe で指定する。

Losslessness:

- Tree nodeが`entityAddress`とoccurrenceKeyを持つ限り、同一Entityの重複出現を表現できる。
- cycleを消すのではなく`cycleToKey` markerとして保持する。
- shared childを1箇所に畳む場合はlossyなので`duplicateOfKey`を保持する。

Export:

- JSON: `TreeViewData`
- Markdown: outline
- HTML: expandable tree
- SVG / PNG: tree diagram snapshot
- PDF: fixed tree report
- CSV / XLSX: flattened tree with path columns

### 4.5 Flow

Purpose:

- 業務フロー
- データフロー
- request / event flow
- migration sequence

Flow は「線形リスト」ではない。
分岐、合流、loop、parallel を持つため、FlowViewData は step graph として扱う。

```ts
interface FlowViewData extends ViewDataBase {
  kind: "flow";
  lanes: FlowLane[];
  steps: FlowStep[];
  connectors: FlowConnector[];
}
```

Conversion:

- Entity -> FlowStep
- Relation -> FlowConnector
- layer / type / owner / attribute -> lane
- order は recipe の sort / rank / relation traversal で決める
- FlowStepはsource Entity / row StableAddress refsを持つ。
- FlowConnectorはsource Relation / row StableAddress refsを持つ。

Losslessness:

- step と connector が source Entity / Relation を保持すれば graph としては lossless。
- strict sequence export は lossy になり得る。
- BPMNは汎用Flow semanticsを完全保存できず、Language 1 plain exportではlossyな視覚近似として扱う。domain-specificなgateway / event / task mappingは別のexporter profileまたはDocument Generation契約で追加する。

Export:

- SVG / PNG: flow diagram
- PDF: fixed flow document
- Mermaid: flowchart export。semantics は limited。
- BPMN 2.0 XML: generic lossy profile。domain-specific mappingは別契約。
- JSON: `FlowViewData`
- Markdown: ordered or grouped flow summary。lossy。

### 4.6 Context

Purpose:

- AI / MCP 用 context package
- system card
- architecture decision context
- prompt-ready structured memory
- review / approval surface

```ts
interface ContextViewData extends ViewDataBase {
  kind: "context";
  groups: ContextGroup[];
}
```

Conversion:

- selected Entities を layer / type / groupBy で grouped facts にする。
- relations は incoming / outgoing / related facts として保持する。
- attributeRows は typed facts として保持する。

Losslessness:

- selected subgraph の facts、attributeRows、relation refs を保持する JSON context は lossless にできる。
- Markdown / prompt text単体はlossy。Language 1のSource Manifestを伴う場合はtraceable summaryにできる。

Export:

- JSON: primary export。AI / MCP が読む構造化 context。
- YAML: human-editable context。
- Markdown: review / prompt companion。
- HTML: readonly context viewer。
- PDF: review artifact。

### 4.7 Diff

Purpose:

- baseline / target definition comparison
- revision 差分
- branch / proposal diff
- migration delta

Diff は現在の単一 graph からは作れない。
必ず baseline と target が必要である。

```ts
interface DiffViewData extends ViewDataBase {
  kind: "diff";
  recipeRevisionId: string;
  beforeRevisionId: string;
  afterRevisionId: string;
  changes: DiffChange[];
}
```

Conversion:

- baseline graphとtarget graphをStableAddressで比較する。
- Entity / Relation / Type / Layer / Attribute row の add / remove / update / move を扱う。
- selected LDL `moves`がold/new StableAddressを結ぶ場合だけrenameとして表示する。それ以外のID変更はadd/removeであり、heuristic rename detectionは行わない。

Losslessness:

- recipe / before / after revision、side別SourceRefs、changed fieldsを保持すればdiffとしてlossless。
- visual diff は presentation。

Export:

- JSON: `DiffViewData`
- HTML: interactive diff
- Markdown: review summary
- CSV / XLSX: change list
- PDF: approval artifact

## 5. Export Contract

Export は View の後付け機能ではない。
各 ViewCategory / ViewShape は、生成可能な artifact と fidelity を capability として持つ。
ただし export は View の意味を変更しない。
ExportFormat は同じ ViewData を媒体に合わせて写すだけである。

```ts
type ExportFidelity = "lossless" | "traceable_summary" | "visual_only" | "lossy";
type ExportFormat =
  | "json"
  | "yaml"
  | "svg"
  | "png"
  | "pdf"
  | "html"
  | "csv"
  | "tsv"
  | "xlsx"
  | "markdown"
  | "pptx"
  | "docx"
  | "mermaid"
  | "bpmn"
  | "drawio";

interface ExportCapability {
  format: ExportFormat;
  extension: string;
  mediaType: string;
  fidelity: ExportFidelity;
  requiresRenderer: boolean;
  requiresPagination?: boolean;
  requiresBaseline?: boolean;
}

interface ExportRecipe {
  id: string;
  address: StableAddress;
  format: ExportFormat;
  fileName: string;
  fidelity: ExportFidelity;
  sourceRefs: boolean;
  exporterProfile: {
    id: string;
    format: ExportFormat;
    registrySchemaVersion: 1;
    registryDigest: string;
    specificationDigest: string;
  };
  options?: Record<string, unknown>;
}

interface ExportArtifact {
  id: string;
  documentId?: string;
  viewAddress: StableAddress;
  recipeAddress?: StableAddress;
  format: ExportFormat;
  mediaType: string;
  fileName: string;
  generatedFromRevision: string;
  inputHash: string;
  statePolicy: "none" | "optional" | "required";
  stateInput: StateInputRef;
  fidelity: ExportFidelity;
  source: ViewSourceRefs;
}
```

これはView plain export用のhost-language adapter型である。`.ldl`と`.layerdraw`はDocument-level I/Oであり、この`ExportFormat`には含めない。正準wire field、閉じたoption、shape×format×fidelity、ExportPlan、Source ManifestはLDL詳細仕様7.9節だけが所有する。

### 5.1 Excel Export Profiles

Excel は単なる table dump ではない。
Excel workbookはViewのcategory、typed shape、intentを媒体に合わせて表現する成果物である。
Export 側に別の業務ユースケースを持たせない。
通常は View から profile を自動決定し、必要な時だけ override する。

```ts
type ExcelWorkbookProfile =
  | "typeWorkbook"
  | "diagramWorkbook"
  | "composedDiagramWorkbook"
  | "matrixWorkbook"
  | "treeWorkbook"
  | "impactWorkbook"
  | "flowWorkbook"
  | "diffWorkbook"
  | "contextWorkbook"
  | "diagramInventoryWorkbook";

interface ExcelExportRecipe extends ExportRecipe {
  format: "xlsx";
  profileOverride?: ExcelWorkbookProfile;
  includeSourceRefs?: boolean;
  includeLookupSheets?: boolean;
  includeHiddenIds?: boolean;
  includeFormulas?: boolean;
  includeViewDataJson?: boolean;
}

interface ExcelWorkbookArtifact extends ExportArtifact {
  format: "xlsx";
  profile: ExcelWorkbookProfile;
  sheets: ExcelSheetArtifact[];
}
```

Default Excel profiles are shape-based:

| ViewShape | Default Excel profile | Main sheets | Support sheets |
| --- | --- | --- | --- |
| `table` | `typeWorkbook` | selected typed rows | Relations, Lookups, SourceRefs |
| `matrix` | `matrixWorkbook` | Matrix | RowAxis, ColumnAxis, Relations, SourceRefs |
| `diagram` | `composedDiagramWorkbook` when composed, otherwise `diagramWorkbook` | Occurrences, Edges, Containers, Overlays, Badges | SupportItems, Type sheets, Lookups, SourceRefs |
| `tree` | `treeWorkbook` | TreeOccurrences | Relations, Links, Cycles, SourceRefs |
| `flow` | `flowWorkbook` | Steps, Connectors, Lanes | Branches, Joins, Cycles, SourceRefs |
| `context` | `contextWorkbook` | Facts, Entities, Relations | AttributeRows, SourceRefs |
| `diff` | `diffWorkbook` | Changes | Added, Removed, Updated, Moves, SourceRefs |

`impactWorkbook` is an explicit override only for category `impact` with Diagram/Table/Matrix. `diagramInventoryWorkbook` is an explicit Diagram override. Category alone never changes the default profile.

Profile examples:

- `typeWorkbook`
  - one sheet per EntityType
  - relation sheet
  - lookup sheet
  - optional source refs sheet

- `matrixWorkbook`
  - matrix sheet shaped like the dynamic matrix view
  - row axis sheet
  - column axis sheet
  - cell source relation sheet
  - formulas or links from matrix cells to source relation rows

- `composedDiagramWorkbook`
  - occurrences sheet keyed by occurrenceKey with source Entity StableAddress
  - edges sheet keyed by edgeKey with source Relation StableAddress
  - containers sheet for parent / child occurrence refs
  - overlays sheet keyed by overlayKey
  - badges sheet keyed by badgeKey
  - support items sheet for hidden relation / entity refs
  - source refs sheet including attribute row and relation row refs

- `impactWorkbook`
  - scoped nodes sheet
  - scoped relations sheet
  - path / depth sheet
  - risk or review columns
  - source refs sheet

- `flowWorkbook`
  - steps sheet ordered by flow rank
  - connectors sheet
  - lanes sheet
  - branch / join sheet when detected

- `diffWorkbook`
  - changes sheet
  - added / removed / updated sheets
  - before / after references
  - approval status columns

Excel formulas are presentation helpers, not source of truth.
StableAddress source refs are the source tracking mechanism. Host境界を越える参照はDocument IDと組にし、server-backed locator / access checkではOrganization scopeも解決する。portable ViewDataへOrganization metadataを意味情報として埋め込まない。
The workbook can be used for review and manual annotation, but importing Excel changes back into `.ldl` must be an explicit import pipeline with validation.

## 6. Plain Export Capability

Plain export は、同じ ViewData を媒体ごとの自然な表現に写す機能である。
ここではビジネス文書テンプレートを扱わない。

shape×format×最大fidelity×必須optionの閉じた規範マトリクスは[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) 7.9節だけが所有する。本書は媒体ごとの意図を説明するが、対応形式を追加・削除したり最大fidelityを再定義してはならない。

Document-level I/O and derived artifacts（Plain View Exportの`ExportFormat`ではない）:

- `.ldl`: source document, always available.
- `.layerdraw`: package bundle, always available.
- normalized JSON document: internal / SDK / agent use.
- package previews: derived artifacts, never source of truth.

## 7. Document Generation

Document Generation は plain export とは別機能である。
これは View の意味を変えるものではなく、1 つ以上の ViewData を使ってビジネス文書を組み立てる機能である。

Examples:

- design document
- approval report
- review pack
- migration plan appendix
- architecture decision pack
- executive summary deck
- operational runbook

```text
ViewData[]
  + DocumentTemplate
  + Narrative sections
    -> PDF / PPTX / DOCX
```

DocumentTemplate は ViewRecipe の一部ではない。
ViewRecipe は「何をどう見るか」を定義する。
DocumentTemplate は「その ViewData をどう文書化するか」を定義する。

```ts
interface DocumentGenerationRecipe {
  recipeId: string; // host/registry-owned recipe identity, not an LDL StableSymbol
  name: string;
  template: "design-doc" | "approval-report" | "review-pack" | "runbook" | "executive-summary";
  inputViewAddresses: StableAddress[];
  outputFormat: "pdf" | "pptx" | "docx";
  sections?: DocumentSectionRecipe[];
}
```

Priority:

1. ViewData and dynamic views
2. Plain exports
3. Document Generation

Document Generation は有用だが、View model を主導してはいけない。
If a PDF / PPTX / DOCX needs a different business meaning, create a different View or a separate document template; do not overload ExportRecipe.

## 8. DSL Requirements

View DSL must be able to represent:

- source selection
- query / filter
- category and primary shape
- table row unit / axis / cell payload
- presentation hints
- plain export recipes
- baseline references for diff

Example:

```ldl
view app_data_matrix "Application x Data Store" dependency {
  intent "Application to data dependency matrix"
  source query app_data_scope {}

  matrix {
    row_axis {
      entity_types [application_service]
      label display_name
    }
    column_axis {
      entity_types [data_store]
      label display_name
    }
    cell {
      relation_types [writes, reads]
      direction outgoing
      semantic relation_refs
      display relation_types
    }
  }

  export matrix_xlsx xlsx "application-data-matrix.xlsx" {
    fidelity lossless
    source_refs
    profile matrix_workbook
    hidden_ids
    view_data_json
  }
}
```

`semantic relation_refs` は semantic ViewData 用である。
`display relation_types` は presentation summary であり、正本変換ではない。
Matrixをlosslessにするには、LDL / ViewData modelが`relationRefs`または同等のcell payloadを表現できる必要がある。
`relationTypes` だけでは cell の出自を復元できないため、traceable-summary までしか保証できない。

## 9. Implementation Gates

ViewCategory / ViewShape を追加する前に、必ず以下を満たす。

1. ViewData type を定義する。
2. Master Graph から ViewData への変換規則を書く。
3. 変換が lossless / traceable-summary / lossy のどれかを明示する。
4. RenderData type を定義する。
5. Plain export capability matrix に形式を追加する。
6. DSL recipe で必要条件を保存できるようにする。
7. LayerDraw Engine conformance testで`Master Graph -> ViewData`を検証する。
8. Render testで`ViewData -> RenderData`、Export conformance testで`ViewData + ExportRecipe + ExportProfileRequirements -> ExportPlan -> Artifact + Source Manifest`を検証する。

UI はこの後で作る。
UI が先に ViewCategory / ViewShape を作ってはいけない。
RelationType System が未実装のまま relation-aware ViewData を実装してはいけない。
