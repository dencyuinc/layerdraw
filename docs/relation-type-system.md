# Relation Type System

status: RelationTypeの設計背景、View連携、product behavior。本書は規範的なLDL grammarやmaterialization semanticsを再定義しない。正確なfield、validation、module、identity、migration規則は[ldl-language-specification.md](ldl-language-specification.md)、defaultsと決定的変換は[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)に従う。

本書のcamelCase TypeScript interfaceとLadybugDB内部列はproduct/host binding例である。LDL source、normalized model、MCP/HTTP/state wireの正準名と意味はLDL規範仕様2文書を優先し、binding adapterがlower_snake_caseとの変換を担う。

Relation は View System、query、export、AI context の入力であり、単なる線ラベルではない。
LayerDraw の Relation は「図に線を引く命令」ではなく、「2 つの Entity の間に存在する構造化された事実」を宣言する。

Relation 周りの設計は、EntityType と同じ原則で分ける。

```text
LayerDraw Engine / TS Render mechanism
  RelationType schema
  endpoint / cardinality validation
  relation attribute table
  projection primitives
  render primitives
  export contract

Pack / Template
  AWS / Azure / GCP / ER / business-flow などの具体 RelationType
  RelationType default values
  render hints
  assets
  starter graph
```

LayerDraw EngineとTS RenderはAWSやERを直接知らない。
LayerDraw EngineはPackが宣言したRelationTypeをViewDataへ変換し、Export時はExportPlanを生成する。TS Render / versioned serializerはViewData、RenderData、ExportPlanとrender hintsからvisual output / ExportArtifactを生成する共通機構だけを持つ。

## 1. Current Gaps

以下はlegacy implementationのAs-Isであり、目標言語仕様ではない。実装移行の差分を示すために残す。

- `Relation.type` はただの string で、project 内に RelationType 定義がない。
- Web UI の relation type list は `apps/web/src/lib/workbench-model.ts` にハードコードされている。
- core は relation type の存在、意味、endpoint 制約を検証しない。
- legacy parserはRelationType schemaとstable row IDを表現できない最小Relation構文しか持たない。
- Relation の属性は `metadata` 任せで、schema と validation がない。
- Entity と違い、Relation に 2 次元の属性表を持てない。
- query / view は relation type string filter しかできず、semantic kind で切れない。
- View 変換時に、RelationType ごとに edge / nest / overlay / badge / hide を選べない。
- 複数 Layer 合成ビューで、Relation によって親枠化・重ね表示・線保持を切り替えられない。
- Matrix cell が relation type label だけになりやすく、source relation refs と cell semantics を保証しづらい。
- Flow view に必要な sequence / branch / join / data-flow / control-flow の区別がない。
- Tree / hierarchy view に必要な containment / ownership と普通の dependency が区別されていない。
- Excel export で relation sheet を型ごとに生成するための relation schema がない。

結論として、今の Relation は「線の種類ラベル」であり、「構造上の意味」と「View 変換ルール」にはなっていない。

## 2. Design Principle

RelationType は EntityType と同じく、project の正本スキーマである。

```text
EntityType
  -> Entity instances

RelationType
  -> Relation instances
```

RelationType は以下を決める。

- 何を意味する関係か
- `from` / `to` がそれぞれ何の役割か
- どの EntityType / Layer 間で許可されるか
- cardinality はどうか
- Relation が持てる属性表の列定義は何か
- 逆向きにはどう読めるか
- Go View MaterializerがViewDataへどう変換するか
- TS Renderで利用できるvisual hintは何か
- Go Export Plannerがどのsource relation / rowとしてExportPlanへ束縛するか

RelationType は UI の線スタイル定義ではない。
ViewData、validation、query、export、AI context の共通 contract である。

ただし、RelationType に任意コードを埋め込まない。
PackはLayerDraw Engineが知っているprojection primitiveとTS Renderが知っているrender primitiveを宣言するだけで、それぞれの規範componentが宣言を解釈する。

```text
OK:
  projection.composed.mode nest
  render.edge.line dashed

NG:
  custom JavaScript renderer
  AWS-specific hardcoded renderer in Engine / Render
```

## 3. Direction Contract

LayerDraw の Relation はすべて directed binary relation として定義する。

```ldl
relations writes {
  api_writes_db: order_api -> order_db
}
```

これは「`app.order_api` が `data.order_db` に writes する」という宣言である。
`from` / `to` は図形上の左右ではなく、RelationType が定義する endpoint role の実体である。

LayerDraw は native undirected graph を採用しない。
理由は、正本モデル、LadybugDB の `FROM/TO`、クエリ生成、差分、重複排除、export を同じ方向付き fact として揃えるためである。

ER 図やネットワーク図で線を無矢印に見せることはできる。
ただし、それは Renderer の表現であり、Relation の意味論は `from -> to` の directed fact のまま扱う。

```text
正本:
orders -> users
type: references

ER 表示:
users 1 -------- 0..* orders
```

逆向きの読み方は `labels.reverse` で表現する。
逆向き探索は incoming directed query として扱う。
「関連するもの全部」は incoming と outgoing の明示的な合成であり、無向グラフ探索ではない。

## 4. Semantic Model

```ts
type StableAddress = string;

interface LayerDrawProject {
  projectAddress: StableAddress;
  entityTypes: EntityType[];
  relationTypes: RelationType[];
  layers: Layer[];
  entities: Entity[];
  relations: Relation[];
  queries: QueryRecipe[];
  views: ViewRecipe[];
  references: Reference[];
}

interface RelationType {
  id: string;
  address: StableAddress;
  displayName: string;
  description?: string;
  semanticKind: RelationSemanticKind;
  allowSelf: boolean;
  from: RelationEndpointRule;
  to: RelationEndpointRule;
  cardinality: RelationCardinality;
  duplicatePolicy: RelationDuplicatePolicy;
  labels: RelationLabels;
  columns: AttributeColumn[];
  uniqueConstraints: AttributeUniqueConstraint[];
  traversal?: RelationTraversalPolicy;
  projections: RelationProjectionRules;
  renderHints: RelationRenderHints;
  exportHints?: RelationExportHints;
  reservedColumns: string[];
  reservedConstraints: string[];
  tags: string[];
  annotations: Record<string, string>;
}

type RelationSemanticKind =
  | "dependency"
  | "data_flow"
  | "control_flow"
  | "deployment"
  | "network"
  | "security"
  | "containment"
  | "ownership"
  | "sequence"
  | "impact"
  | "reference"
  | "governance";

interface RelationEndpointRule {
  role: string;
  entityTypeAddresses?: StableAddress[];
  layerAddresses?: StableAddress[];
}

interface RelationCardinality {
  toPerFrom?: RelationCardinalityBound;
  fromPerTo?: RelationCardinalityBound;
}

interface RelationCardinalityBound {
  min: 0 | 1;
  max: 1 | "many";
}

interface AttributeUniqueConstraint {
  id: string;
  address: StableAddress;
  columns: StableAddress[];
}

type RelationDuplicatePolicy =
  | "allow"
  | "denySameTypeBetweenSameEndpoints"
  | "denyAnyBetweenSameEndpoints";

interface RelationLabels {
  forward: string;
  reverse?: string;
}

interface RelationTraversalPolicy {
  defaultDirection: "outgoing" | "incoming" | "both";
  participatesInImpact?: boolean;
  participatesInFlow?: boolean;
  participatesInHierarchy?: boolean;
  participatesInDependencyMatrix?: boolean;
}

interface Relation {
  id: string;
  address: StableAddress;
  typeAddress: StableAddress;
  fromAddress: StableAddress;
  toAddress: StableAddress;
  displayName?: string;
  description?: string;
  attributeRows: AttributeRowData[];
  tags: string[];
  annotations: Record<string, string>;
}
```

Pack version、digest、canonical pack originはRelationTypeのauthored fieldではなくmodule resolution metadataである。Host presetの`builtin` flagもLDLへシリアライズしない。

Relationのsystem / provenanceはRelation本体ではなく、Host Document IDでscopeされたRelation StableAddressへstate layerで紐づける。
Relationの各属性行は`.ldl`でauthored stable row IDを持つ。ViewData、export、state provenance、Matrix cell detail、Table rowのsource refはRelation ownerとrow IDから構成したStableAddressを使い、業務列の値やrow indexから識別子を導出しない。
業務列の組による重複制約は RelationType の `unique` として row identity から分離する。
importer が stable row ID のない legacy data を読む場合は diagnostics を出し、修復時に deterministic row ID を `.ldl` へ materialize する。

## 5. Cardinality

`one_to_many` のような名前だけでは検証できない。
LayerDraw では RelationType の authored direction `from -> to` に対して、以下を使う。

```ts
interface RelationCardinality {
  toPerFrom?: { min: 0 | 1; max: 1 | "many" };
  fromPerTo?: { min: 0 | 1; max: 1 | "many" };
}
```

- `toPerFrom`: 1 つの `from` が、この RelationType で何個の `to` を持てるか。
- `fromPerTo`: 1 つの `to` に、この RelationType で何個の `from` がぶら下がれるか。

例:

| RelationType | authored direction | toPerFrom | fromPerTo | 意味 |
| --- | --- | --- | --- | --- |
| `calls` | caller -> callee | `0..many` | `0..many` | 1 caller は複数 callee を呼べる。1 callee は複数 caller から呼ばれる。 |
| `writes` | writer -> target | `0..many` | `0..many` | 1 writer は複数 target に書ける。 |
| `runs_on` | workload -> runtime | `0..1` | `0..many` | 1 workload は原則 1 runtime。1 runtime は複数 workload を載せる。 |
| `contains` | container -> contained | `0..many` | `0..1` | 1 container は複数 child を含む。1 child は原則 1 parent。 |
| `owns` | owner -> owned | `0..many` | `0..1` | 1 owned object の owner は原則 1 つ。 |
| `references` | referrer -> referenced | `0..1` | `0..many` | 外部キーを持つ側が参照先を指す。参照先は複数から参照され得る。 |

Cardinality は RelationType の validation rule である。
Relation instance ごとに `one-to-many` のような文字列を持たせない。

## 6. Relation Attribute Table

Relation は Entity と同じく 2 次元の属性表を持てる。
RelationType の `columns` が列定義で、Relation の `attributeRows` が実データである。

```ldl
relation_type allows "Allows traffic" network {
  from source types [subnet, security_group, service]
  to destination types [subnet, security_group, service]
  label "allows"
  reverse "is allowed by"
  unique traffic_rule [protocol, port, cidr]
  columns {
    protocol "Protocol" enum [tcp, udp, icmp] required
    port "Port" string required
    cidr "CIDR" string required format cidr
    action "Action" enum [allow, deny] required
  }
}

relations allows {
  sg_app_to_db: sg_app -> sg_db
}

relation_rows allows [protocol, port, cidr, action] {
  sg_app_to_db postgres: tcp, "5432", "10.0.0.0/16", allow
  sg_app_to_db pgbouncer: tcp, "6432", "10.0.1.0/24", allow
}
```

Relation の属性表が必要になる代表例:

- firewall / security group rule
- API contract
- data mapping
- join condition
- batch schedule
- interface field mapping
- dependency condition
- responsibility split

1 行だけで足りる Relation も `attributeRows` を使う。
これにより Entity と Relation の属性モデルを揃える。

## 7. Projection Rules

ProjectionRule は、Relation を ViewData にどう変換するかを宣言する。
これは Renderer の見た目より前の、構造変換の契約である。

```ts
interface RelationProjectionRules {
  composed?: ComposedProjectionRule;
  diagram?: DiagramProjectionRule;
  table?: TableProjectionRule;
  matrix?: MatrixProjectionRule;
  tree?: TreeProjectionRule;
  flow?: FlowProjectionRule;
  context?: ContextProjectionRule;
}
```

### 7.1 Composed Projection

複数 Layer を重ねたビューでは、RelationType によって表示用の包含ツリーや overlay を作る。

```ts
type ComposedProjectionMode =
  | "edge"
  | "nest"
  | "overlay"
  | "badge"
  | "hide";

interface ComposedProjectionRule {
  mode: ComposedProjectionMode;
  parentEndpoint?: "from" | "to";
  childEndpoint?: "from" | "to";
  targetEndpoint?: "from" | "to";
  overlayEndpoint?: "from" | "to";
  badgeEndpoint?: "from" | "to";
  priority?: number;
  conflict?: "keep_edge" | "prefer_first" | "diagnostic";
  keepEdge?: boolean;
}
```

Modes:

- `edge`: Relation を線として ViewData に残す。
- `nest`: 一方の Entity を親枠、もう一方を子として表示用 tree に変換する。
- `overlay`: security / policy / ownership などを対象 Entity の overlay として付ける。
- `badge`: 対象 Entity に小さな badge / chip / icon として付ける。
- `hide`: ViewData の構造変換や filter には使うが、表示要素としては出さない。

Projection rule は RelationType default であり、ViewRecipe は同じ primitive の範囲で上書きできる。
上書きは View の意図を表すためのもので、RelationType の endpoint 制約、cardinality、attribute schema、duplicate policy を変更してはならない。

```text
effective projection =
  RelationType.projection
  merged with ViewRecipe.projection.relationTypeOverrides[typeId]
```

例:

```ldl
relation_type deployed_to "Deployed to" deployment {
  from workload
  to placement
  label "is deployed to"
  projection composed {
    mode nest
    parent_endpoint to
    child_endpoint from
  }
}

relation_type protects "Protects" security {
  from control
  to target
  label "protects"
  projection composed {
    mode overlay
    overlay_endpoint from
    target_endpoint to
  }
}

relation_type routes_to "Routes to" network {
  from source
  to destination
  label "routes to"
  projection composed {
    mode edge
  }
}
```

これにより、正本は分離したまま、View では 1 枚の合成図を生成できる。

```text
正本:
  ecs_service -> subnet        deployed_to
  security_group -> ecs_service protects
  alb -> ecs_service           routes_to

Composed View:
  [Subnet]
    [ECS Service + SecurityGroup overlay]

  ALB -> ECS Service
```

### 7.2 Diagram Projection

Diagram projection は、Relation を基本線として出すか、composed rule を優先するかを決める。

```ts
interface DiagramProjectionRule {
  mode?: "edge" | "hide";
  sourceEndpoint?: "from" | "to";
  targetEndpoint?: "from" | "to";
  edgeLabel?: "type" | "display_name" | "forward_label" | "reverse_label" | "none";
  includeRelationType?: boolean;
}
```

effective composed projectionの`keepEdge`がfalseの場合、`nest`に使われたRelationは線としては消える。
true の場合は親子化しつつ補助線も残せる。

### 7.3 Table Projection

Table projection は Relation を表に変換する単位を決める。

```ts
interface TableProjectionRule {
  rowMode: "relation" | "relation_rows" | "automatic";
  includeFrom?: boolean;
  includeTo?: boolean;
  includeRelationType?: boolean;
}
```

- `relation`: 1 Relation = 1 row。
- `relation_rows`: 1 Relation attribute row = 1 row。
- `automatic`: Relation rowがあれば`relation_rows`、なければ`relation`としてViewData materialization時にRelationごとに解決する。

Relation に属性表がある場合、Excel では `relation_rows` が自然になる。

```text
Allows sheet:
relation_id | from | to | protocol | port | cidr | action
```

### 7.4 Matrix Projection

Matrix projection は、Relation を matrix の row / column / cell にどう写すかを決める。

```ts
interface MatrixProjectionRule {
  rowEndpoint: "from" | "to";
  columnEndpoint: "from" | "to";
  includeRelationRows?: boolean;
}
```

Matrix projectionはRelationTypeのどちらのendpointをrow/columnへ置くかと、Relation row詳細を使えるかを決める。`relation_refs` / `path_refs`のsemantic sourceと、`exists`、`count`、`relation_types`、`attribute_summary`の表示方法はView Matrix shapeが所有する。cellは必ずsource RelationまたはQueryPath refsを持ち、属性表を使う場合はrowとColumnのStableAddress refsを含める。

### 7.5 Tree Projection

Tree projection は、Relation から tree occurrence を生成する。

```ts
interface TreeProjectionRule {
  parentEndpoint: "from" | "to";
  childEndpoint: "from" | "to";
}
```

`contains`、`owns`、`runs_on` などは tree / composed view に参加できる。
ただし master graph の Entity 自体を移動するのではなく、ViewData 上の occurrence を作る。
cycle policyとshared-child policyは複数RelationTypeをまとめて評価するView Tree shapeが所有し、RelationType projectionへ重複定義しない。

### 7.6 Flow Projection

Flow projection は Relation を flow edge / branch / join / data dependency に変換する。

```ts
interface FlowProjectionRule {
  sourceEndpoint: "from" | "to";
  targetEndpoint: "from" | "to";
  connectorKind?: "sequence" | "control" | "data" | "message" | "error";
  branchValueColumnAddress?: StableAddress;
}
```

`next`、`yes`、`no`、`calls`、`flows_to` などは flow view で異なる connector として描画できる。

### 7.7 Context Projection

Context projection は AI / MCP 向けの事実文を生成する。

```ts
interface ContextProjectionRule {
  factTemplate?: string;
  reverseFactTemplate?: string;
  includeAttributeRows?: boolean;
}
```

例:

```text
{from.display_name} writes to {to.display_name}
{to.display_name} is written by {from.display_name}
```

## 8. Render Hints

RenderHints は ViewData から RenderData / Artifact を作るときの見た目のヒントである。
ProjectionRule と違い、正本構造を変えない。

```ts
interface RelationRenderHints {
  edge?: RelationEdgeRenderHints;
  nested?: RelationNestedRenderHints;
  overlay?: RelationOverlayRenderHints;
  badge?: RelationBadgeRenderHints;
}

interface RelationEdgeRenderHints {
  arrow?: "forward" | "backward" | "both" | "none";
  line?: "solid" | "dashed" | "dotted";
  color?: string;
  label?: "display_name" | "type" | "forward_label" | "reverse_label" | "none";
}

interface RelationNestedRenderHints {
  frameLabel?: "parent" | "type" | "display_name" | "none";
  frameStyle?: "subtle" | "strong" | "none";
}

interface RelationOverlayRenderHints {
  kind?: string;
  position?: "top_right" | "top_left" | "bottom_right" | "bottom_left" | "center";
  maxItems?: number;
}

interface RelationBadgeRenderHints {
  icon?: string;
  label?: "type" | "display_name" | "count" | "none";
  position?: "top_right" | "top_left" | "bottom_right" | "bottom_left";
}
```

TS Renderが対応するrender primitiveの範囲では、Pack追加だけで描画表現を変えられる。
完全に新しい意味表現はRelationTypeへ埋め込まず、LayerDraw EngineのViewShape / ProjectionModeとTS Renderのversioned primitiveとして追加する。

```text
Pack 追加だけで対応:
  edge / nest / overlay / badge / hide の組み合わせ
  line style / arrow / label / icon / color

Engine / Render拡張が必要:
  heatmap
  lifeline
  swimlane engine
  timeline engine
  custom layout algorithm
```

## 9. Built-in Relation Types

Built-in RelationType は、label ではなく schema として提供する。

| id | semanticKind | from role -> to role | cardinality | composed default | note |
| --- | --- | --- | --- | --- | --- |
| `depends_on` | dependency | dependent -> dependency | many / many | edge | impact, dependency matrix |
| `calls` | control-flow | caller -> callee | many / many | edge | flow, impact, context |
| `reads` | data-flow | reader -> source | many / many | edge | data lineage, matrix |
| `writes` | data-flow | writer -> target | many / many | edge | data lineage, matrix |
| `runs_on` | deployment | workload -> runtime | one / many | nest parent=`to` child=`from` | topology, impact |
| `connected_to` | network | source -> destination | many / many | edge | network view may render without arrow, but relation is still directed |
| `routes_to` | network | source -> destination | many / many | edge | topology, flow |
| `owns` | ownership | owner -> owned | many / one | nest parent=`from` child=`to` | hierarchy, responsibility |
| `contains` | containment | container -> contained | many / one | nest parent=`from` child=`to` | hierarchy, topology |
| `protects` | security | control -> target | many / many | overlay target=`to` overlay=`from` | security overlays |
| `next` | sequence | previous -> next | one / one | edge | flow |
| `impacts` | impact | cause -> affected | many / many | edge | impact |
| `references` | reference | referrer -> referenced | one / many | edge | ER, dependency, context |

`connects_to` のような既存 label は、migration / alias で `connected_to` へ正規化できる。
ただし、正規化後も Relation は directed fact として扱う。

## 10. Pack Extension

AWS、Azure、GCP、ER、業務フロー、AI memory などの具体表現は Pack として追加する。

PackはRelationTypeにdefault valueを入れたデータ定義であり、Engine / Renderのprimitiveを増やすものではない。Pack moduleの完全な例は[ldl-language-specification.md](ldl-language-specification.md)のComplete Multi-Module Exampleに置く。

この設計なら、新しいRelationTypeとprojection / render hintsをPackで追加するだけで、LayerDraw Engine / TS Renderが知っているprimitiveの範囲では新しい合成表示を使える。

## 11. DSL

RelationTypeはproject / pack module、Relationはproject moduleの第一級宣言にする。Packはproject factであるEntity / Relation / rowを宣言しない。正確なLDL grammar、属性名、enum、stable row ID、import / exportは[ldl-language-specification.md](ldl-language-specification.md)と[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)を規範とする。

未定義 RelationType はLanguage 1ではerrorである。legacy documentは専用importerが明示的なplaceholder RelationTypeとdiagnosticsへ変換する。
Built-in catalog は暗黙の言語 prelude ではない。使用時に project-local RelationType 宣言へ materialize するか、展開済み pack module から明示的に import する。

## 12. Validation Rules

LayerDraw Engine validation must cover:

- RelationType ID duplicate
- RelationType alias duplicate or conflicts with another id
- RelationType missing `semanticKind`
- RelationType missing `from.role` / `to.role`
- Relation references unknown RelationType
- Relation endpoint Entity missing
- Relation endpoint violates `from.types` / `to.types`
- Relation endpoint violates `from.layers` / `to.layers`
- Relation self-loop violates `self deny`
- Relation violates duplicate policy
- Relation violates cardinality
- Relation attribute row uses unknown column
- Required relation attribute is missing
- enum relation attribute is invalid
- RelationType `unique.columns` references an unknown column
- Relation attribute row ID is missing or duplicated
- Relation attribute rows violate a `unique` constraint
- Projection rule references an endpoint that is not usable for that mode
- Projection rule uses a mode unsupported by LayerDraw Engine
- Render hint uses a primitive unsupported by TS Render capability profile
- `nest` projection creates a cycle in the ViewData occurrence tree
- `nest` projection creates multiple parents where conflict policy forbids it
- flow relation cycle warnings for `next` / `sequence` where strict sequence is requested
- hierarchy relation cycle error for containment-like relations

Validation severity:

- schema / endpoint / unknown column: error
- undefined type in language `"1"`: error; legacy importer materializes an explicit placeholder and diagnostic
- cardinality conflict: error
- duplicate row ID or `unique` tuple: error
- invalid projection mode: error
- nested occurrence cycle: error
- multi-parent composed conflict: warning or error based on conflict policy
- flow cycle: warning unless view requires acyclic flow
- hierarchy cycle: error

Duplicate key:

```text
type + from + to
```

No reversed endpoint duplicate rule is needed because LayerDraw does not model native undirected relation.

## 13. Composed View Materialization

Composed View は、複数 Layer の正本を 1 つの表示ツリーへ合成する。

Materialization order:

```text
Master Graph
  -> select entities / relations by ViewRecipe
  -> resolve RelationType
  -> merge ViewRecipe projection overrides
  -> sort projection candidates by canonical order
  -> apply projection.composed by priority
  -> build display containment occurrences
  -> attach overlays / badges
  -> keep remaining edges
  -> produce DiagramViewData
```

Rules:

- RelationType `projection.composed.mode nest` は ViewData 上の occurrence tree を作る。Master Entity を移動しない。
- 同じ child に複数 parent 候補がある場合は、priority と conflict policy で決める。
- nest に使われた Relation は、effective composed projectionの`keep_edge`がtrueの時だけedgeとして残す。
- overlay / badge は target occurrence に付与する。source Entity は必要に応じて hidden support node として ViewData source refs に残す。
- すべてのderived display itemは`source.entityAddresses` / `source.relationAddresses` / `source.rowAddresses` / `source.cellRefs`を持てる。

Determinism rules:

- `projection.composed.priority` の default は `0`。
- priority は大きいほど強い。
- projection candidate の canonical order は以下で固定する。

```text
priority desc
relationType StableAddress asc
relation StableAddress asc
from Entity StableAddress asc
to Entity StableAddress asc
```

- multi-parent candidate は canonical order で比較する。
- `conflict preferFirst` は canonical order の先頭を採用する。
- `conflict keepEdge` は nest せず edge として残す。
- `conflict diagnostic` は ViewData diagnostics にwarningを出し、ambiguousなnestを生成せず全候補をsupport itemとして保持する。
- overlay / badge は target occurrence ごとに canonical order で並べる。
- ViewDataは全overlay / badgeを保持する。`render.overlay.maxItems`を超えたvisual overflow集約はRenderDataだけで行い、ViewData keyとSourceRefsを失わせない。
- occurrence / edge / overlay / badge / support itemはLDL StableSymbolではなくViewData-local itemである。

Canonical item tuple、`vdi:<kind>:<base64url-digest>`形式、collision規則は[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) 9節だけが所有する。rendererが短縮keyを使う場合も同一ViewData内のcollision検査が必要であり、item keyをstate、MCP subject、LDL referenceとして永続化してはならない。

Example:

```text
Network layer:
  VPC
  Subnet

Security layer:
  SecurityGroup

Application layer:
  ALB
  ECS
  RDS

Relations:
  VPC -> Subnet              contains
  ECS -> Subnet              deployed_to
  RDS -> Subnet              deployed_to
  SecurityGroup -> ECS       protects
  ALB -> ECS                 routes_to
  ECS -> RDS                 writes

Composed View:
  [VPC]
    [Subnet]
      [ECS + SecurityGroup overlay]
      [RDS]

  ALB -> ECS
  ECS -> RDS
```

## 14. LadybugDB Mapping

LayerDraw Relation の `from` / `to` は LadybugDB の `FROM` / `TO` と一致させる。

Query indexのprimary keyとsemantic referenceにはlocal IDではなくStableAddressを使う。これによりproject/pack origin、宣言kind、owner-scoped rowが衝突しない。
LadybugDB propertyの`NULL`はLDLで省略されたoptional fieldをindex内で表す実装値であり、LDL source scalarとして`null`を許可するものではない。

```cypher
CREATE NODE TABLE Entity(
  address STRING,
  localId STRING,
  typeAddress STRING,
  layerAddress STRING,
  displayName STRING,
  description STRING,
  tags STRING[],
  annotations MAP(STRING, STRING),
  PRIMARY KEY(address)
)
```

```cypher
CREATE REL TABLE Rel(
  FROM Entity TO Entity,
  address STRING,
  localId STRING,
  typeAddress STRING,
  displayName STRING,
  description STRING,
  tags STRING[],
  annotations MAP(STRING, STRING)
)
```

Relation の属性表は、複数行を持てるため Rel edge の scalar property だけでは表現しきれない。
Query indexではEntity rowとRelation rowを別ノードとして投影する。

```cypher
CREATE NODE TABLE EntityRow(
  address STRING,
  entityAddress STRING,
  entityTypeAddress STRING,
  attrs MAP(
    STRING,
    UNION(
      str_value STRING,
      int_value INT64,
      number_value DOUBLE,
      bool_value BOOL,
      date_value DATE,
      datetime_value TIMESTAMP
    )
  ),
  PRIMARY KEY(address)
)
```

```cypher
CREATE NODE TABLE RelationRow(
  address STRING,
  relationAddress STRING,
  relationTypeAddress STRING,
  attrs MAP(
    STRING,
    UNION(
      str_value STRING,
      int_value INT64,
      number_value DOUBLE,
      bool_value BOOL,
      date_value DATE,
      datetime_value TIMESTAMP
    )
  ),
  PRIMARY KEY(address)
)
```

LadybugDBの`MAP` valueは単一logical typeであるため、`MAP(STRING, STRING)`へ潰してはならない。[LadybugDBの公式data type contract](https://docs.ladybugdb.com/cypher/data-types/)にあるnested `UNION`をvalue typeに使う。`UNION` tagはLDL Columnのscalar typeから決定し、enumは`str_value`、UTC-normalized datetimeは`datetime_value`へ投影する。Go Query compilerはColumn StableAddressから期待tagを決めて`union_extract`相当のtyped expressionを生成し、文字列比較への暗黙coercionを行わない。
複数tagを一つのMAP value listへ挿入する時は、各`union_value`を上記の完全な共通`UNION(...)`型へ明示castしてからmapを構築する。WASM/JavaScript round-tripに合わせ、datetime sourceはUTC millisecond精度を上限とする。
使用するLadybugDB runtimeはnested `UNION`と必要なscalar logical typeをsupportしなければならない。未対応engineはtyped queryを文字列fallbackで実行せず、capability errorにする。

`EntityRow.address` / `RelationRow.address`はLDLのauthored owner IDとrow IDからStableSymbol規則で構成する。row index、値、query engine内部IDから生成してはならない。

```text
ldl:project:order_platform:relation:order_api_writes_db:row:primary
```

すべてのRelation attribute rowはauthored stable row IDを持ち、Relation ownerと組み合わせてrow StableAddressを構成する。row-level state tracking自体はoptionalだが、ViewData、export、matrix cell detailのsource refは常にこのStableAddressを使う。
修復可能な legacy import では formatter が stable row ID を `.ldl` に materialize し、以後はその値を正とする。

Base relation query:

```cypher
MATCH (a:Entity)-[r:Rel]->(b:Entity)
WHERE r.typeAddress = $relation_type_address
RETURN a.address, r.address, b.address
```

Relation row query:

```cypher
MATCH (a:Entity)-[r:Rel]->(b:Entity), (row:RelationRow)
WHERE r.address = row.relationAddress
  AND row.relationTypeAddress = $allows_type_address
  AND union_extract(map_extract(row.attrs, $port_column_address)[1], 'str_value') = '5432'
RETURN a.address, r.address, row.address, b.address
```

無向 relation へのコンパイルは行わない。
必要な探索は outgoing / incoming / explicit both の directed query として生成する。

Projection / render rules は LadybugDB の schema ではなく、LayerDraw project schema として保持する。
Graph query は Relation facts と Relation rows を返し、View materializer が RelationType の projection rules を解釈する。

## 15. View Implications

RelationType is required before reliable ViewData.
View materialization must use RelationType, not raw string labels.

| View shape | Required RelationType data | Lossless condition |
| --- | --- | --- |
| Diagram / topology | labels, projection rules, render hints, endpoint roles | Edge / nested / overlay items keep source relation refs. |
| Composed diagram | composed projection rules, priority, conflict policy | Occurrence tree keeps source Entity and Relation StableAddresses. |
| Table / inventory | relation columns, endpoint roles, table projection | Relation rows keep Relation, row, and column StableAddresses. |
| Matrix | semanticKind, matrix projection, cell relation filters | Cell stores source Relation and row StableAddresses, not only labels. |
| Tree / hierarchy | tree projection, containment / ownership semantics, cardinality | Occurrence keeps source Entity and via-Relation StableAddresses. Cycle/shared-child policyはView shapeが持つ。 |
| Flow | flow projection, sequence / control / data-flow semantics, traversal flags | Step / connector keeps source StableAddresses and branch diagnostics. |
| Impact | traversal policy, reverse labels | Traversal result keeps ordered Relation StableAddress paths. |
| Context / MCP | labels, reverse labels, endpoint roles, attributes | Context facts cite source Relation and row StableAddresses. |
| Diff | RelationType schema version, attribute schema, projection schema | Diff compares semantic meaning and projection behavior. |

### Diagram / Topology

- RelationType projection decides whether the relation becomes edge / nest / overlay / badge / hidden support data.
- RelationType render hints decide line style, color, arrow marker, overlay style, badge icon, and label.
- Network views may render `connected_to` with `render.edge.arrow none`.
- Renderer display does not change query semantics.

### Inventory / Table

- Relation sheets are generated from RelationType schema.
- RelationType labels become column headers and workbook sheet names.
- Relation attribute rowsはeffective `projection table { row_mode relation_rows }`、またはRelationごとに評価した`automatic`でtable rowになる。

### Dependency / Matrix

- Matrix cells store source `relationAddresses` and optional row/cell StableAddress refs.
- Cell detail can include stable relation row IDs when the relation has an attribute table.
- RelationTypeはrow/column endpointとrelation-row inclusionを決め、cell semantic/displayはView Matrix shapeが決める。

### Hierarchy / Tree

- `containment` and `ownership` can materialize trees.
- DAG / cycle handling depends on semantic kind, cardinality, and tree projection rule.
- containment is represented only by typed Relations; Entity does not carry a second parent/children source of truth.

### Flow

- `sequence`, `control-flow`, and `data-flow` relations participate in flow.
- `next` can define strict order.
- Branch / join detection requires relation semantic kind and flow projection rule, not string labels.

### Impact

- Impact traversal is relation-type aware.
- Some relations are traversed by default; some are only context.
- Reverse traversal uses incoming directed edges and `labels.reverse`.

### Context

- RelationType labels and reverse labels are essential for readable AI context.
- Context export includes semanticKind, endpoint labels, Relation StableAddresses, and relation attribute rows.

## 16. UI Requirements

Relation UI must become type-aware.

- Relation type selector comes from project relationTypes, not hardcoded constants.
- Creating a relation filters candidate types by selected source / target.
- Drag creation rejects invalid endpoint pairs or shows why disabled.
- Inspector shows RelationType description, semanticKind, endpoint roles, labels, cardinality, projection behavior, and render behavior.
- Inspector edits typed relation attribute rows.
- Inspector may show freshness / provenance metadata when available, but does not treat it as business attributes.
- RelationType manager allows custom RelationType creation.
- Built-in relation packs are installable from registry / templates.
- Relation lines, nesting, overlays, and badges use RelationType projection / render hints.
- Composed View preview can explain why a relation became edge / nested child / overlay / badge / hidden support data.

## 17. Export Requirements

以下はRelationTypeからExportArtifactへ直接変換する規則ではない。Go Export PlannerがViewData、RelationType、ExportRecipeからExportPlanを生成し、各versioned serializerがそのplanをXLSX、JSON / YAML、draw.io / SVGへ写す。

XLSX:

- Relation sheet per RelationType or one normalized `Relations` sheet with `relationType`.
- Readable local IDs plus hidden/protected `relation_address`, `from_entity_address`, `to_entity_address`.
- Relation attribute rows become typed rows.
- Display columns use labels / `XLOOKUP`.
- Matrix workbooks link cells to source relation rows.
- Composed view workbooks include occurrence / overlay / badge source refs.

JSON / YAML:

- Include RelationType schema.
- Include relation instances with `attributeRows`.
- Include ViewData projection result when exporting a View artifact.

drawio / SVG:

- Use render hints for line style, color, markers, labels, overlays, and badges.
- Preserve Relation StableAddress in metadata where supported.
- Nested / overlay display items preserve source relation refs.

Context / MCP:

- Include forward and reverse labels.
- Include semanticKind.
- Include endpoint Entity、Relation、row、columnのStableAddress refsとrelation attribute rows。
- Include projection explanation when relevant to the View.

## 18. Schema Evolution

RelationType は project schema なので、変更は migration として扱う。

Allowed non-breaking changes:

- description update
- label update
- render hint update
- export hint update
- adding optional attribute column
- adding projection hints that only affect new View categories

Breaking changes:

- `id` change without explicit migration
- `semanticKind` change
- endpoint role change
- endpoint rule narrowing
- cardinality tightening
- stable row ID or `unique` constraint change
- required column addition
- enum value removal
- projection mode change that alters existing ViewData materialization
- render primitive removal used by existing Views

Migration policy:

- project-local RelationType ID変更は`rename_subject`で全参照を原子的に更新し、`moves { relation_type old -> current }`をentryへ残す。
- import aliasはsource bindingにすぎず、identity rename aliasとして使わない。
- breaking changeはproject migrationとしてdiagnosticsを出す。
- pack-origin RelationTypeはconsumer projectから変更せず、canonical pack identityとresolved digestのまま参照する。
- Registry / template 更新で既存 project の RelationType を勝手に書き換えない。
- committed enum optionの削除は既存row / Query / defaultを明示migrationし、ColumnまたはQuery parameterの`reserve_values`へ旧値を残す。

## 19. Implementation Checklist

- Add `RelationType` model and built-in definitions in core.
- Replace the legacy parser with Language 1 grouped `relation_type`, `relations`, and `relation_rows` support.
- Add projection / render rule model and validation.
- Add validation for relation type schema, endpoints, attributes, stable row IDs, unique constraints, duplicate policy, cardinality, projection conflicts, and render primitives.
- Replace web hardcoded `relationTypeOptions` with project relationTypes + built-ins.
- Make relation creation endpoint-aware.
- Add relation inspector attribute row editor.
- Add RelationType manager UI for schema, projection, render, and export hints.
- Update graph/query mapping to expose RelationType semanticKind and relation rows.
- Add real-engine tests for StableAddress primary keys and every LDL scalar through `MAP(STRING, UNION(...))` insert/filter round-trips.
- Update ViewRecipe filters to support relation semantic kind in addition to relation type IDs.
- Implement composed View materialization with edge / nest / overlay / badge / hide.
- Join relation state / provenance in ViewData materialization or inspector, not in canonical Relation DSL.
- Implement relation-aware `ViewData` materialization.
- Implement relation-aware XLSX / matrix / context exports.

View UI and new ViewData renderers must not advance before RelationType schema, DSL, validation, projection rules, and UI type source are implemented.
