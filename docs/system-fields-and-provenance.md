# System Fields, State, and Provenance

本書のcamelCase TypeScript interfaceはhost-language binding例であり、state snapshotとcanonical wireはlower_snake_caseを使う。

本書はdurable stateの責務と保存境界を定義する。Query/Viewへ渡す閉じた`StateQuerySnapshot`、`StateInputRef`、`StateReadRef`、field registry、hash、missing/stale/redacted semanticsは[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) 5.4節と6節が唯一の規範ownerである。Durable backend stateに存在するfieldが自動的にLDL Query可能になるわけではない。

LayerDraw の `.ldl` は Terraform の `.tf` や Kubernetes manifest に近い、宣言的な定義ファイルである。
Entity / Relation / Layer / ViewRecipe は authored definition であり、Git review と MCP patch の対象にする。

一方で `created_at` / `updated_at` / `created_by` / `updated_by` / `observed_at` / `verified_at` / raw source URI / field ownership は、定義ではなく runtime state / provenance data である。
これらを `.ldl` に混ぜると、DSL の責務が壊れ、ローカル開発、Git diff、テンプレート化、再利用が破綻する。

結論:

- `.ldl` は declarative definition。
- system fields / provenance / audit は state layer。
- `.ldl` 単体共有は definition-only として扱い、state が無い場合は freshness / provenance を `unknown` にする。
- metadata を含めて共有したい場合は `.ldl + .ldstate.json`、`.layerdraw` package、または server backend を使う。
- Query/View recipeはstate値を含めず、必要なstate field pathとrequired/optional policyだけを`.ldl`へ保持する。
- state backend と backend binding の詳細は [state-backends.md](state-backends.md) に従う。

## 1. Prior Art

設計は既存の宣言型システムに寄せる。

### 1.1 Terraform

Terraform は `.tf` configuration と state を分ける。
公式 docs では、state は configuration 内の resource instance と remote object の対応、依存関係などの metadata、performance cache を保持するものとされている。
state backend は state storage と locking を担当し、local JSON file、remote backend、locking などを切り替えられる。

参考:

- <https://developer.hashicorp.com/terraform/language/state>
- <https://developer.hashicorp.com/terraform/language/state/purpose>
- <https://developer.hashicorp.com/terraform/language/state/backends>
- <https://developer.hashicorp.com/terraform/language/state/locking>

LayerDraw に置き換えると、`.ldl` は `.tf`、`.ldstate.json` / server state は `tfstate` に近い。

### 1.2 Kubernetes

Kubernetes object は desired state の `spec` と、system が更新する current state の `status` を分ける。
`metadata.name` / labels / annotations のようにユーザーが宣言する metadata は manifest に入るが、`resourceVersion` や `managedFields` のような server-managed metadata は control plane 側が管理する。
Server-Side Apply の field ownership も `.metadata.managedFields` に記録され、通常の manifest authoring とは別の責務である。

参考:

- <https://kubernetes.io/docs/concepts/overview/working-with-objects/>
- <https://kubernetes.io/docs/reference/using-api/api-concepts/>
- <https://kubernetes.io/docs/reference/using-api/server-side-apply/>
- <https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/>

LayerDraw に置き換えると、`.ldl` は `spec` / authored metadata、state layer は `status` / `managedFields` に近い。

### 1.3 Pulumi

Pulumi は program を desired definition として扱い、state backend に cloud resource metadata を保存する。
`pulumi refresh` は provider から実状態を読み直して state を更新するが、program 自体は更新しない。
backend は hosted service、S3、Azure Blob Storage、Google Cloud Storage、local filesystem などを選べる。

参考:

- <https://www.pulumi.com/docs/iac/concepts/state-and-backends/>
- <https://www.pulumi.com/docs/iac/operations/stack-management/using-a-diy-backend/>

LayerDraw に置き換えると、runtime / SDK / self-host server は state backend を差し替えられる必要がある。

### 1.4 CloudFormation

CloudFormation template は expected configuration を表す。
実際の stack state や drift detection は service 側で管理され、template と actual state の差分を検出する。
AWS docs でも、stack resource を template 外で変更すると drift が起きると説明されている。

参考:

- <https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-stack-drift.html>
- <https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/best-practices.html>

LayerDraw に置き換えると、`.ldl` と current state は一致しているとは限らず、state refresh / drift detection が必要になる。

## 2. LayerDraw Boundary

LayerDraw は 4 つのレイヤーに分ける。

```text
project.ldl
  declarative definition

project.ldstate.json
  local generated state snapshot

project.layerdraw
  portable package: definition + state + assets + generated artifacts

LayerDraw Server / Runtime
  revisions + audit log + current state index + storage adapter + locks
```

### 2.1 `.ldl`

`.ldl` に入れるもの:

- project identity
- EntityType / RelationType
- Layer
- Entity
- Entity attribute rows
- Relation
- Relation attribute rows
- Query
- ViewRecipe
- Reference
- authored local IDs、reservations、moves used to construct and migrate StableAddresses
- business attributes
- semantic labels / tags / annotations
- asset references
- template / pack references

`.ldl` に入れないもの:

- `created_at`
- `updated_at`
- `created_by`
- `updated_by`
- `observed_at`
- `verified_at`
- `confidence`
- raw source URI
- field ownership
- row / cell provenance
- audit log
- revision history
- render cache
- generated previews / exports
- server auth / collaboration state

ただし、業務上の意味を持つ日付は business attribute として `.ldl` に入る。
たとえば `service.launch_date`、`contract.expires_on`、`migration.planned_cutover_at` は system field ではない。

### 2.2 `.ldstate.json`

ローカル backend で使う generated state snapshot。
Terraform の local state に近い。
人間が主に編集するファイルではない。

推奨ファイル名:

```text
project.ldl
project.ldstate.json
```

Git 管理方針は project policy で決める。

- semantic memory / audit-aware project: commit してよい
- public template / reusable pack: commit しない
- sensitive project: remote backend or encrypted state

`.ldstate.json` が無い状態で `.ldl` を開いた場合、LayerDraw は definition-only project として扱う。
system / provenance は `unknown` と表示し、編集開始時に新しい state snapshot を生成できる。

### 2.3 `.layerdraw`

`.layerdraw` は zip package。
definition だけでなく、state / assets / previews / exports をまとめて共有する用途に使う。

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

metadata を含めて再現性を持たせたい共有は `.layerdraw` を使う。
`.layerdraw`内の`document.ldl`はentry moduleであり、project-local LDL source treeがdeclarative definitionである。stateは`state/`に分ける。

### 2.4 Server / Runtime State

Server / Runtime は state backend を持つ。

- current state index
- provenance index
- audit log
- revision log
- lock / lease
- collaboration presence

state backend は adapter 化する。

- local filesystem
- S3-compatible object storage
- Azure Blob Storage
- Google Cloud Storage
- SharePoint / OneDrive / Google Drive
- managed LayerDraw storage

Backend binding は `.ldl` に混ぜない。
local / remote の backend 設定は `project.ldbackend.json` または `layerdraw.backend.json` に置き、credential は env / keychain / OAuth / IAM から渡す。

## 3. State Model

State subjectは`HostSubjectScope + StableAddress`でkeyedにする。Local / BYOSのHostSubjectScopeは`document_id`、server-backed modeの物理scopeは`organization_id + document_id`である。Server ApplicationがOrganization-scoped BackendBindingを選択してからRuntimeを呼ぶため、Runtime内部の`StateSubjectRef`は`document_id + StableAddress`を保持し、Organization directoryを直接参照しない。StableAddressの構成は[ldl-language-specification.md](ldl-language-specification.md)を規範とする。

```ts
interface StateSubjectRef {
  documentId: string;      // selected host namespace内のimmutable document identity
  address: StableAddress;  // canonical LDL subject identity
}

type StableAddress = string;

interface SystemMeta {
  createdAt?: string;
  updatedAt?: string;
  createdBy?: ActorRef;
  updatedBy?: ActorRef;
  createdRevision?: string | number;
  updatedRevision?: string | number;
}

interface ActorRef {
  kind: "user" | "agent" | "service_account" | "anonymous";
  id: string;
  displayName?: string;
}

interface ProvenanceMeta {
  source?: SourceRef;
  observedAt?: string;
  verifiedAt?: string;
  verifiedBy?: ActorRef;
  confidence?: number;
  staleAfter?: string;
}

interface SourceRef {
  kind: "manual" | "import" | "api" | "agent" | "external_system";
  label?: string;
  uri?: string;
  externalId?: string;
}
```

上記adapter modelのうちLanguage Queryへprojectionできるのは、詳細仕様5.4節のfield registryに列挙されたscalar leafだけである。Field ownership、cell provenance、任意provider payload、audit historyを`StateQuerySnapshot`へ入れてはならない。RuntimeはAccess判定とredactionを先に行い、拒否されたfieldを単なるmissingへ変換しない。

Asset、preview、export artifactはLDL StableSymbolではないため、`StateSubjectRef`へ偽装しない。content digestとhost-owned artifact IDを持つ別の`ArtifactRef`で扱う。field provenanceが必要な場合はowner StableAddressとschema-defined field pathを使い、attribute cellはrow StableAddressとcolumn StableAddressの組で指す。

State snapshot 例:

```json
{
  "format": "layerdraw-state",
  "format_version": 1,
  "document_id": "doc_01j_order_platform",
  "definition_project_address": "ldl:project:order_platform",
  "definition_hash": "sha256:...",
  "graph_hash": "sha256:...",
  "subject_semantic_hashes": {
    "ldl:project:order_platform:entity:order_api": "sha256:..."
  },
  "subjects": {
    "ldl:project:order_platform:entity:order_api": {
      "system": {
        "created_at": "2026-07-10T10:00:00Z",
        "updated_at": "2026-07-10T12:30:00Z"
      },
      "provenance": {
        "source": {
          "kind": "api",
          "label": "CMDB"
        },
        "verified_at": "2026-07-10T12:00:00Z",
        "confidence": 0.92
      }
    }
  }
}
```

`subject_semantic_hashes`は各StableAddressのown-subject hashである。owner-scoped childを含む依存無効化が必要なcacheはsubtree hashを別fieldで保持し、競合検出用のown-subject hashと混同しない。
State restoreは現在のsubject lookupより先にLDL `moves`を適用し、owner renameではchild address prefixも移行する。対応するmoveがない旧addressを表示名や構造類似度から推測して結合してはならない。

## 4. Attribute Row Tracking

Entity の `attributeRows` は 2D table であり、鮮度管理の粒度が問題になる。

方針:

- default は Entity 単位 tracking。
- Attribute row-level tracking は optional capability。
- `.ldl` の全 attribute row は authored stable row ID を持つ。tracking の有無によって ID の有無を変えない。
- ViewData / exportがattribute rowをsource refとして保持する場合は、ownerとstable row IDから構成した同じrow StableAddressを使う。
- row index は state subject、source ref、audit target として使わない。
- Cell-level tracking は対応しない。

Row-level state は `.ldl` の row data に混ぜず、state subject として持つ。

```text
ldl:project:order_platform:entity:order_api:row:production
ldl:project:order_platform:relation:order_api_writes_db:row:primary
```

## 5. Freshness Semantics

`updatedAt` と `verifiedAt` は違う。

- `updatedAt`: そのsubjectのown-subject semantic hashが最後に変更された時刻。row/child変更をownerへ暗黙伝播しない
- `observedAt`: source から事実を観測した時刻
- `verifiedAt`: user / agent / source により事実を検証した時刻
- `staleAfter`: stale と見なす時刻
- `confidence`: optional score
- `source`: source system or human input

AI / MCP context は state が無い場合に「古い」と断定してはいけない。
`freshness: "unknown"` として扱う。

## 6. Import / Export Modes

LayerDraw は export mode を明示する。

### 6.1 Definition export

`.ldl` のみ。
テンプレート、Git review、MCP patch、定義共有に使う。

```text
project.ldl
```

State / provenance は含まれない。
受け取った側では freshness / provenance は `unknown`。

### 6.2 Local snapshot export

`.ldl + .ldstate.json`。
ローカル backend の state を含めて共有する。

```text
project.ldl
project.ldstate.json
```

### 6.3 Package export

`.layerdraw`。
assets / state / previews / exports を含む portable package。

### 6.4 Redacted export

actor ID、raw source URI、external ID などを削る。
これはplain exportのSource Manifestではなく、redacted `.layerdraw` packageの境界である。削った場合はLayerdrawManifestの`redaction`にpolicy IDを残し、StateQuerySnapshot hashとそれを参照するViewData/Source Manifest/exportを再計算する。削除fieldを読む成果物は`LDL1904`により生成せず、旧snapshot由来の成果物をpackageへ残さない。

## 7. Query、View、およびExport

State依存Query / Viewでは、Go Runtimeがbackend stateからAccess判定済みのimmutable StateQuerySnapshotを1件構築し、Go Engineへ明示入力する。

```text
LayerDrawProject (.ldl recipe)
  + StateQuerySnapshot (.ldstate / server stateからの型付きprojection)
    -> QueryResult / ViewData
      -> RenderData
      -> ExportPlan -> ExportArtifact + Source Manifest
```

Stateが無い場合:

- state非依存Query/Viewは通常どおり評価できる
- `state_input required`は`LDL1604`で失敗する
- `state_input optional`は規範no-state sentinelを使い、state fieldをmissingとして評価する
- AI/MCPはprovenance unknownを明示し、backendやclockから値を捏造しない

Stateがある場合:

- Query predicateでEntity、Relation、attribute rowのsystem/provenance fieldを条件にできる
- Table Viewの`source state` Columnとして`updated_at`、`verified_at`、`source`等を表示・sort・aggregateできる
- Queryによるselection結果はDiagram/Matrix/Tree/Flow/Contextにも利用できるが、それらのshapeがstate値を独自表示するとは暗黙に解釈しない
- ViewDataはStateInputRefとStateRefsを持ち、state依存plain exportではExportPlan/Source Manifestがsnapshot hashまたはoptional no-state入力を必ず束縛する
- Source Manifestは成果物の出自検証を担う。Master Graphからのportable再materializeには、`.layerdraw/state/query-snapshots/`に同じhashのcomplete snapshot本体が必要である
- Diff Viewはdefinition revision差分であり、auditのchangedBy/changedAt enrichmentはReview/Time Machine機能として分離する

`created_at`/`updated_at`はLayerDraw recordの時刻である。業務上の作成日、契約期限、移行予定日時はauthored ColumnとしてLDLに置き、state system fieldで代用しない。相対時刻Queryは暗黙の`now()`を使わず、callerがdatetime parameterを渡す。

## 8. Privacy and Security

State は `.ldl` より敏感である。

含みうるもの:

- actor IDs
- internal source URI
- imported external IDs
- agent IDs
- confidence / verification history
- stale business facts

そのため、state backend は permissions / encryption / redaction policy を持つ。
public repository に `.ldstate.json` を commit するかは project policy で決める。

## 9. Integration Obligations

- Go EngineはStateQuerySnapshotをpure inputとして評価し、backendを解決しない。
- Go RuntimeはBackendBinding、state取得、Access判定の適用、snapshot固定、hash / subject compatibility、sync / reconcileを所有する。
- Go Accessはactor、credential scope、共有ACL、agent scopeからfield-level allow / redact decisionを生成し、state値やbackend credentialを保持しない。
- Host adapterは認証済みStorage / Connectionを提供し、credentialをEngineへ渡さない。
- Go package componentは`.ldstate.json`と`.layerdraw/state/`の規範serialization / validationを所有する。
- stateなしimportはprovenance unknownとし、編集時に新stateを生成しても過去metadataを推測しない。
- redacted packageは削除fieldをLayerdrawManifestとquery projectionで識別可能にし、missingへ偽装しない。snapshotと派生成果物はredaction後の入力から再生成する。
- row-level stateはstable row IDで参照する。Field/cell provenanceはdurable stateに存在してもStateQuerySnapshotへ含めない。
