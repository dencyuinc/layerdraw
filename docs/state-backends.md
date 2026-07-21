# State Backends

LayerDraw の `.ldl` は declarative definition である。
`createdAt` / `updatedAt` / provenance / audit / freshness は state layer に分ける。

その結果、Terraform と同じく state backend が第一級の設計要素になる。
state backend は単なる保存先ではなく、`.ldl` と runtime state を安全に結びつける contract である。

Durable stateの全fieldを直接Queryへ公開しない。LayerDraw RuntimeはAccess判定、redaction、`move_closure`、own-subject hash互換性を適用し、[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) 5.4節の閉じた`StateQuerySnapshot`へprojectionしてからLayerDraw Engineへ渡す。

Runtime / Host Port、open / commit / save / autosave / reconcileの状態遷移、credential境界は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 4章を規範とする。

## 1. Separation

LayerDraw は 3 種類のファイル / 設定を分ける。

```text
project.ldl
  declarative definition

project.ldstate.json
  local state snapshot

layerdraw.backend.json / project.ldbackend.json
  non-secret backend binding
```

Credentials は backend config に入れない。
API token、OAuth refresh token、cloud secret、service account key は environment variables、OS keychain、host application secret store、cloud IAM から注入する。

## 2. Backend Binding

Backend binding は `.ldl` に書かない。
`.ldl` は reusable definition / template / Git diff の対象であり、保存先や workspace 固有設定を混ぜない。

Backend binding は sidecar config として置く。

推奨:

```text
layerdraw.backend.json
```

単一 document の隣に置く場合:

```text
project.ldl
project.ldbackend.json
```

優先順位:

1. Runtime / SDK caller が明示した backend config
2. `project.ldbackend.json`
3. workspace root の `layerdraw.backend.json`
4. host application / managed cloud project settings
5. default local backend: `project.ldstate.json`

この優先順位により、ローカル開発は設定なしで動き、チーム運用では backend binding を commit できる。

## 3. Backend Config Schema

`layerdraw.backend.json` は secret を含まない。
commit してよいのは endpoint、bucket、prefix、state key、document mapping などの non-secret configuration だけである。

例:

```json
{
  "format": "layerdraw-backend",
  "format_version": 1,
  "default_backend": "team-state",
  "documents": {
    "infra.ldl": {
      "backend": "team-state",
      "state_key": "projects/infra/current"
    }
  },
  "backends": {
    "team-state": {
      "type": "s3",
      "bucket": "layerdraw-state",
      "prefix": "workspace-a",
      "region": "ap-northeast-1",
      "lock": {
        "mode": "conditional-write",
        "ttl_seconds": 120
      }
    },
    "local": {
      "type": "local",
      "path": "./project.ldstate.json"
    }
  }
}
```

Credential は次のような外部 mechanism で渡す。

- `LAYERDRAW_BACKEND_TOKEN`
- cloud provider default credentials
- workload identity / IAM role
- OAuth connection owned by host app
- OS keychain entry

## 4. State Identity

State はどの `.ldl` に対応しているかを必ず持つ。

```json
{
  "format": "layerdraw-state",
  "format_version": 1,
  "document_id": "doc_01j_order_platform",
  "definition_project_address": "ldl:project:order_platform",
  "document_path": "infra.ldl",
  "definition_hash": "sha256:...",
  "graph_hash": "sha256:...",
  "subject_semantic_hashes": {
    "ldl:project:order_platform:entity:order_api": "sha256:..."
  },
  "state_version": 42,
  "backend_version": "etag-or-generation",
  "updated_at": "2026-07-10T12:30:00Z"
}
```

`document_id`はhost/storageが発行するimmutableなDocument identity、`definition_project_address`はDSLから再構成できるportable identityであり、同一概念ではない。`definition_hash` / `graph_hash`は高速な全体差分検出に使い、stateの鮮度判定はStableAddress keyedのown-subject `subject_semantic_hashes`で対象subjectごとに行う。dependency cacheが必要なbackendは別にsubtree hashを保存できる。Referenceやdocumentation commentの変更だけで無関係なEntity / Relation stateをstaleにしてはならない。
Local / BYOSでは`document_id`がHostSubjectScopeになる。server-backed modeではServer Applicationが`organization_id + document_id`でscopeしたBackendBinding / adapter instanceを先に選び、そのnamespace内で同じstate formatを使う。Organizationはportable state semanticsではないため`.ldstate.json`へ必須fieldとして埋め込まず、server adapterのobject key、repository query、lease、cache、audit contextで必ずscopeする。
state headの`updated_at`はその`state_version`作成時に1回固定し、StateQuerySnapshot `captured_at`へ使う。同じversionのreadごとに現在時刻で上書きしてsnapshot hashを変えてはならない。
local sidecarは`document_id`を保持し、definition-only fileを初めて永続化する時に生成する。`.layerdraw`をimport-as-newするhostはsource packageのDocument IDを引き継がず、新しい`document_id`を割り当てる。
state読込時はLDL `moves`のtransitive StableAddress mappingを先に適用する。mapping後もsubject hashが一致しないstateは対象単位でrefresh / reconcile / migrateを要求する。subject hashのないlegacy stateは全体hashをcompatibility summaryとして使い、reconcile時にsubject hashをmaterializeする。

## 5. Backend Contract

Runtime / SDK / server / desktop / VSCode extension は同じ state backend contract を使う。

概念的なport contractをGo風の表記で示す。wire / TS bindingはこの規範schemaから生成する。

```go
type StateBackend interface {
  GetHead(context.Context, GetStateHeadInput) (*StateHead, error)
  ReadState(context.Context, ReadStateInput) (*LayerDrawState, error)
  WriteState(context.Context, WriteStateInput) (StateWriteResult, error)

  AcquireLease(context.Context, AcquireLeaseInput) (StateLease, error)
  RenewLease(context.Context, RenewLeaseInput) (StateLease, error)
  ReleaseLease(context.Context, ReleaseLeaseInput) error

  AppendAuditEvent(context.Context, AppendAuditEventInput) (AuditEventRef, error)
  ListAuditEvents(context.Context, ListAuditEventsInput) (AuditEventPage, error)

  ExportSnapshot(context.Context, ExportStateSnapshotInput) (LayerDrawState, error)
}
```

すべての write は optimistic concurrency を要求する。

- expected `state_version`
- expected backend ETag / generation / blob version
- active lease token
- definition / graph hash and affected StableAddress keyed own-subject semantic hashes

一致しない場合、backend は上書きしない。

## 6. Lease and Locking

Remote backend は複数 editor / 複数 agent が触る前提である。
state write は lease を通す。

```json
{
  "lease_id": "lease_01",
  "owner": {
    "kind": "agent",
    "id": "agent.local"
  },
  "fencing_token": 17,
  "expires_at": "2026-07-10T12:32:00Z"
}
```

必要な性質:

- lease は TTL を持つ
- renew されなければ expire する
- stale lease holder の write は fencing token で拒否する
- backend native lock が無い場合は conditional write で代替する

Object storage では ETag / generation / version id を使う。
Google Drive / OneDrive / SharePoint では file version / etag を使う。
Git repository backend では commit base / blob sha を使う。

## 7. Backend Types

### 7.1 Local

default backend。

```text
project.ldl
project.ldstate.json
```

用途:

- single-user local workflow
- desktop app
- VSCode extension
- offline MCP bridge

Locking は process lock または file lock。
複数 machine 共有には向かない。

### 7.2 Object Storage

S3-compatible storage、Azure Blob Storage、Google Cloud Storage、R2、MinIO。

代表 layout:

```text
/state/{state_key}/head.json
/state/{state_key}/snapshots/{state_version}.json
/state/{state_key}/audit/{state_version}-{event_id}.json
/state/{state_key}/leases/current.json
```

特徴:

- 低コスト
- self-host / serverless と相性が良い
- conditional write が必須
- realtime notification は別 component が必要

### 7.3 External Drive

Google Drive、OneDrive、SharePoint。

特徴:

- 保存先 ACL を尊重できる
- enterprise document workflow と相性が良い
- etag / file version による conflict detection が必要
- realtime collaboration は provider API だけでは不足しやすい

### 7.4 Repository

GitHub / GitLab repository。

`.ldl` は repository file として管理する。
state は repository 内に置くか、別 remote backend を使う。

推奨:

- `.ldl`: repository
- state: remote object storage or managed backend
- generated exports: release artifact or object storage

Repository に state を置く場合は actor / source の漏洩に注意する。

### 7.5 Managed LayerDraw Backend

Managed cloud / self-host server が state backend を提供する。

含むもの:

- state storage
- audit log
- lease manager
- permissions
- redaction policy
- realtime event bridge
- package export

## 8. Runtime Behavior

### 8.1 Opening `.ldl`

1. `.ldl` を parse する
2. backend binding を解決する
3. state backend から head を読む
4. `definition_hash` / `graph_hash` と subject semantic hash を比較する
5. LayerDraw Accessの判定に従い、actor / credential scopeに許可されたqueryable fieldだけをprojectionし、redacted fieldを明示する
6. 一致するsubject recordからimmutable StateQuerySnapshotを構築し、snapshot hashを固定する
7. 不一致subjectをstaleとして保持し、Query policyに応じた診断とrefresh/reconcileを要求する
8. stateが無ければfreshness/provenanceを`unknown`とし、state非依存評価またはoptional no-state sentinelだけを許可する

### 8.2 Editing

`.ldl` の semantic operation と state update は別で扱う。

- definition change: Working Document に semantic operation を適用し、検証成功時に canonical な Committed Revision を発行する
- provenance / freshness change: state backend を更新する
- actor / audit: audit log に append する

同一 UI 操作で両方変わる場合も、runtime 内部では definition write と state write を分ける。
ただし同じ user / agent operation から発生した write は、同一 `operation_id` と idempotency key で束ねる。
document revision、`state_version`、audit event は同じ operation boundary に属することを記録する。

protocol binding上のrequired write inputsを次に示す。Go型とTS型は同じschemaから生成し、永続化形式とwireでは`operation_id`、`idempotency_key`、`expected_document_revision`、`expected_document_hash`、`expected_state_version`、`lease_token`を使う。

これはRuntime内部でdefinition / state / auditを束ねるcoordination recordであり、public `runtime.commit_operations` payloadを置き換えない。clientはLDL詳細仕様のOperationBatchを一度だけ渡し、Runtimeがcurrent headとOperationBatchからdocument hash等をmaterializeする。

```ts
interface RuntimeWriteBoundary {
  operationId: string;
  idempotencyKey: string;
  expectedDocumentRevision: string | number;
  expectedDocumentHash: string;
  expectedStateVersion?: number;
  leaseToken?: string;
}
```

Transactional backend がない保存先では、runtime は write-ahead operation record を使う。

```text
1. append pending operation record and durable pending realtime outbox event
2. validate the Working Document and stage changed source/manifest blobs without publishing a new head
3. conditionally update the definition head with expected revision / hash; this success is the Committed Revision publication point
4. write state snapshot or state delta with expected `state_version` and the same `idempotency_key`
5. append audit event
6. record the final OperationResult status and mark the outbox event ready
7. publish the ready realtime event with definition revision and state status
```

途中で失敗した場合:

- step 3のhead公開前に失敗: staged blobは未公開のまま、OperationResultを`rejected`として返す。
- step 3成功後、state write前または途中で失敗: definition revisionは残し、OperationResultを`committed_state_stale`、診断を`LDL1902`としてreconcileを要求する。
- state write後、audit append前に失敗: `committed_state_stale`としてaudit repair jobが`idempotency_key`から補完する。
- step 3のconditional update応答またはstep 6のoutbox ready化が不明: recoveryはpending record、pending outbox、definition head、state headを比較し、公開成否を一意に証明できれば`committed` / `committed_state_stale` / `rejected`へ収束させる。一意に決められない場合だけ`needs_review`と`LDL1903`を返す。pending outboxをdurableに保存できない場合はstep 3へ進まない。

この設計により、`.ldl` と state は物理的に分離したまま、同一操作として追跡できる。

### 8.3 Refresh

Refresh は external source や provider から実状態を読み直し、state を更新する。
`.ldl` は更新しない。

例:

- external CMDB から `verifiedAt` を更新
- cloud API から resource existence を確認
- agent が source confidence を更新

### 8.4 Reconcile

`.ldl` と state の subject semantic hash がずれた時に実行する。

可能な結果:

- state subject を新しい stable ID に対応付ける
- 削除された Entity / Relation の state を tombstone へ移す
- orphan state を audit archive に移す
- manual review を要求する

## 9. Package Export

`.layerdraw` は definition + state + assets + generated artifacts を持てる portable package である。

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
    audit-summary.json
    query-snapshots/
      <snapshot-hash>.json
  assets/
  previews/
  exports/
```

Package export は backend config を同梱しない。
再 import 時は import 先の backend binding を使う。

State依存のmaterialized ViewDataまたはplain exportをpackageへ含める場合、参照するcomplete StateQuerySnapshotを`state/query-snapshots/<snapshot-hash>.json`へ含める。これがMaster Graphから同じQuery/Viewをportableに再実行するための入力であり、`current.json`やExternalStateSummaryから推測してはならない。

Redacted package は actor ID、raw source URI、external ID を削れる。削った場合はLayerdrawManifest `redaction`へ適用済みpolicy IDを残す。全subject共通の削除はStateQuerySnapshot `inaccessible_field_paths`、subject固有の削除は各`redacted_field_paths`へ規範StateFieldPathを記録する。readerはこれを通常のfield absentへ変換してはならず、参照Query/Viewを`LDL1904`で失敗させる。未知provider fieldのredactionはprovider policyへ記録し、StateQuerySnapshot field registryへ追加しない。redaction後はsnapshot hashと全参照派生成果物を再計算し、redacted fieldを読む成果物は同梱しない。旧snapshot hashを持つViewData/Source Manifest/exportを残してはならない。

## 10. Component Boundary

- LayerDraw Runtime: `LayerDrawState` orchestration、StateBackend / BackendBinding解決、StateQuerySnapshot projection、snapshot固定、stale判定、sync / reconcile。
- LayerDraw Engine: StateQuerySnapshot / StateInputRefのpureな型検証とQuery / View評価。backend I/Oを持たない。
- LayerDraw Access: actor、credential、共有ACL、agent scopeからstate field access / redaction decisionを生成する。state取得やsnapshot構築は行わない。
- Host adapter: StorageAdapter、認証済みprovider connection、lease / conditional-write capabilityを実装する。
- Go package component: local `.ldstate.json`と`.layerdraw/state/`の規範serialization / validationを所有する。
- `layerdraw-server`: hosted state、audit、lease、realtime eventの永続service hostである。

Local、object storage、managed / self-host、Drive / SharePoint / OneDriveは同一Runtime portへadapterを供給する。providerごと、またはTS側でQuery semanticsを再実装してはならない。
