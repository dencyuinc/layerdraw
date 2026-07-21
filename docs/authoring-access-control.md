# Authoring Access Control仕様

## 1. 目的

本書は、LayerDrawの正本を変更する権限を、Schema定義、Graph instance、Query、View等の意味領域ごとに分離し、GUI、SDK、MCP、import、Registry、realtimeを含む全書込経路で強制する規範契約を定める。

主な利用例は次である。

- AI製品へ固定済みsemantic modelを組み込み、利用者とAI agentにはEntity / Relation instanceだけを編集させる。
- 業界固有のVertical SaaSで、事業者だけがEntityType / RelationType / Layerを管理する。
- LayerDraw SaaS上のMarketplace製品で、publisherがSchemaを管理し、tenant userには許可されたdata / workflowだけを操作させる。
- Self-hostで部署ごとにSchema管理者とinstance編集者を分離する。

本機能はLicense判定をbuttonやrole名だけから自動確定する仕組みではない。ただし[LayerDraw License 1.0](../LICENSE)のFixed Model Application Safe Harborを技術的に成立させる中核であり、ProviderだけがSchema Definitionを管理し、End Userとagentの全write経路でSchema変更を拒否する。具体例は[legal/use-cases.md](legal/use-cases.md)に従う。

## 2. 基本原則

### 2.1 四段階の責務分離

```text
LayerDraw Engine
  definition / source changeをAuthoringImpactへ分類する

Runtime / Registry / Server Application protocol
  非definition操作をHostOperationImpactへ宣言する

LayerDraw Access
  Actor、role、agent delegation、policy、constraintからdecisionを作る

Publication owner
  Runtimeはdocument / asset / package適用、Server Applicationまたはlocal Host Applicationは
  Host metadataのpreviewとcommitでdecisionを強制し、各resourceを公開する
```

- EngineはActor、Organization、role、licenseを解釈しない。
- AccessはLDLをparseせず、subject kindや変更fieldを推測しない。
- Runtime / Server ApplicationはSchema / Graphの分類を手書きせず、Definition変更にはEngineのAuthoringImpactを使う。asset、package transaction、Host設定はversioned protocolが定義したHostOperationImpactを使う。
- TS、React、MCP adapter、framework shellは分類・判定・強制を再実装しない。

### 2.2 Policyは正本Definitionへ入れない

Authoring Policy、role、grant、Actor bindingはHost metadataであり、次へ保存してはならない。

- `.ldl`
- `.layerdraw`のportable definition
- `.ldpack`
- StateQuerySnapshot
- ViewData / RenderData / ExportPlan

同じ`.ldl`を別Hostへ移してもGraph semanticsは変わらない。import先のHostが新しいPolicyを明示的に割り当てる。portable fileがAccess grantを持ち込み、import先で権限昇格することを禁止する。

### 2.3 UI制限はSecurity Boundaryではない

UI control、MCP tool advertisement、SDK method availabilityはeffective grantを反映するが、最終的なsecurity enforcementではない。authoritative writeはresourceのpublication ownerがcommit直前に再判定する。

local filesystemを利用者自身が直接編集できる構成では、アプリ内制限だけでOS所有者の変更を防げない。強制が必要な製品は、Runtimeを唯一のpublication pathにし、利用者へ正本storageの直接write credentialを渡さない。

### 2.4 Wire schemaとbehavior owner

| Concern | Wire schema | Behavior / persistence owner |
| --- | --- | --- |
| Capability、Definition差分 | `semantic/AuthoringCapability`, `semantic/AuthoringImpact` | capabilityとimpactはActor非依存。AuthoringImpactの生成はLayerDraw Engine |
| Host operation、Policy、Grant、Decision | `access-protocol`（`semantic/AuthoringCapability`を参照） | LayerDraw Access。Policy persistence / bindingはServer Application |
| preview / commit precondition | `runtime-protocol/AuthoringProof` | LayerDraw Runtime |

schema directoryは実装責務を意味しない。依存は`access-protocol -> semantic`の一方向であり、`access-protocol`はAuthoringCapabilityを再定義しない。`access-protocol`は言語中立wire正本であり、TS policy evaluatorや独立serviceを要求しない。

## 3. Authoring Capability

Authoring Capabilityはclosed enumとする。

| Capability | 許可する意味変更 |
| --- | --- |
| `schema:write` | EntityType、RelationType、Column、Constraint、Layerとそれらの意味規則 |
| `graph:write` | Entity、Relation、Entity / Relation row、instance binding |
| `query:write` | Query recipeとparameter |
| `view:write` | View recipe、shape、projection override、export recipe |
| `reference:write` | Reference本文 |
| `asset:write` | asset staging、置換、削除 |
| `source:maintain` | format、module配置、comment / trivia等の意味を変えないsource保守 |
| `project:configure` | Project identity、settings、backend / package policy等の管理 |
| `package:manage` | Pack / Templateのinstall、update、remove、repair transaction |

readは既存の`project:read`、declaration / field redaction、export permissionを使う。instance編集者が型を解釈できるよう、`graph:write`と`schema:read`を別機能にしない。Schemaを秘匿する必要がある場合はread projection / redactionで扱う。

`project:write`や`dsl:write`をauthoritativeな包括grantとして使用してはならない。roleや旧client互換入力として受ける場合は、Hostが上記closed capability setへ展開してから判定する。`dsl:write`だけでSchema変更を許可してはならない。

`agent:propose`と`agent:apply`はAuthoring Capabilityの代用ではない。

- `agent:propose`: 許可外変更を含むproposalを作成できるが、正本へ適用できない。
- `agent:apply`: proposalを適用する入口を許可するだけで、変更内容が要求するAuthoring Capabilityを追加で必要とする。
- approver: proposalの全Required Capabilityを持つActorでなければ承認できない。

## 4. Semantic Classification

### 4.1 Subject分類

LayerDraw Engineはbefore / afterのNormalizedDocumentとsource diffからAuthoringImpactを生成する。LDL外のHost metadataやbinary store操作は分類しない。

| Subject / change | Required Capability |
| --- | --- |
| EntityType / RelationType create、update、delete、rename | `schema:write` |
| EntityType / RelationType Column、Constraint | `schema:write` |
| RelationType endpoint、cardinality、traversal、projection、render、export | `schema:write` |
| Layer create、update、delete、rename、order | `schema:write` |
| Entity create、update、delete、rename、Layer移動、type変更 | `graph:write` |
| Relation create、update、delete、rename、type / endpoint変更 | `graph:write` |
| Entity / Relation rowとcell | `graph:write` |
| Query / Query parameter | `query:write` |
| View / View table column / View export | `view:write` |
| Reference | `reference:write` |
| EntityType image binding | `schema:write`。asset bytesのstage / persistは別HostOperationImpact |
| format、module move、意味不変なcomment / trivia変更 | `source:maintain` |
| Project identity migration、Project-level settings | `project:configure` |
| Pack install / update / remove / repairのresulting definition | 結果diffが要求する全capability。transaction自体の`package:manage`は別HostOperationImpact |

reservation、move、owner-scoped child、imported Pack bindingは対象subject kindへ従う。operation名、source path、UI画面名から分類してはならない。

### 4.2 AuthoringImpact

```text
AuthoringImpact
  base_definition_hash
  resulting_definition_hash
  semantic_diff_hash
  source_diff_hash
  entries[]
    capability
    action: create | update | delete | rename | move | bind | unbind | maintain
    subject_kind
    subject_address?
    owner_address?
    changed_field_paths[]
    before_refs[]
    after_refs[]
    source_refs[]
  required_capabilities[]
  impact_digest
```

`required_capabilities`はentriesの重複なしordered unionである。entryはStableAddress、次にcapability、action、field path順でcanonicalizeする。新規subjectでStableAddressがresulting documentから確定できない変更を有効previewとして扱わない。

semantic diffが空でもsource bytesが変わる場合は`source:maintain`を要求する。format済み結果がbyte-identicalならimpactなしとする。

一つのbatch、fragment、patch、import、restoreが複数capabilityを要求する場合、全capabilityを満たさなければ全体を拒否する。許可部分だけをpartial commitしてはならない。

rename、delete、type / cardinality変更、Pack migration等がinstance、row、Relationを派生変更する場合、AuthoringImpactは明示変更だけでなくresulting documentの全意味差分を含める。Schema変更に伴うinstance削除やdata migrationを`schema:write`だけの副作用として隠さず、実際にGraphが変わるなら`graph:write`も要求する。自動修復不能な不整合は権限判定前にvalidation errorとする。

### 4.3 Constraint評価用facts

EngineはAccessがLDLを再解釈しなくて済むよう、Graph変更entryへ次を付ける。

```text
GraphAuthoringFacts
  entity_type_addresses[]
  relation_type_addresses[]
  layer_addresses[]
  column_addresses[]
  endpoint_entity_addresses[]
  action_flags[]
```

AccessはこのfactとPolicy constraintを比較するだけであり、source textやdisplay nameを読まない。

### 4.4 HostOperationImpact

LDL before / afterを持たないwriteは、owner protocolがclosed operationごとのrequired capabilityをschemaとして定義する。

```text
HostOperationImpact
  operation_kind
  required_authoring_capabilities[]
  resource_scope
  action
  resource_refs[]
  impact_digest
```

標準mapping:

| Host operation | Required Capability |
| --- | --- |
| asset stage / persist / delete | `asset:write` |
| Pack / Template install transaction | `package:manage` |
| backend / package policy、Project Host settings | `project:configure` |

HostOperationImpactはclient request fieldとして自己申告させず、server側がnegotiated protocol versionとdecode済みclosed operationから生成する。Runtime handlerが自由文字列からcapabilityを決めず、generated protocol metadataとtyped operationだけを使う。未知のoperation kind / protocol versionはdenyする。Registry操作のようにHostOperationImpactとAuthoringImpactの両方がある場合、Accessはrequired capabilityの和集合を原子的に評価する。

`access:manage`はAuthoring CapabilityではないためHostOperationImpactへ含めない。AuthoringPolicyを変更するServer Application operationは、変更後のPolicyやclient自己申告grantではなく、変更前のResource Access decisionで別途認可する。`project:configure`、`schema:write`、Project `owner`という表示名だけからPolicy管理を暗黙許可しない。

## 5. PolicyとGrant

### 5.1 AuthoringPolicy

```text
AuthoringPolicy
  policy_id
  policy_version
  policy_digest
  display_name
  capability_rules
    capability
    effect: allow | deny | inherit
  graph_constraints?
  approval_rules[]
  source: server_instance | organization | workspace | project | host_application
```

PolicyはInstance、Organization、Workspace、Projectの階層へ設定できる。上位denyやconstraintを下位allowで拡張してはならない。host application policyもServer policyを拡張せず、積集合として適用する。

### 5.2 GraphAuthoringConstraint

```text
GraphAuthoringConstraint
  allowed_entity_type_addresses[]?
  allowed_relation_type_addresses[]?
  allowed_layer_addresses[]?
  allowed_entity_column_addresses[]?
  allowed_relation_column_addresses[]?
  allowed_actions[]?
```

field省略はその次元を制限しない。明示的な空listは何も許可しない。すべてStableAddressで指定し、display name、local IDだけでpolicyをbindしない。

標準actionは`create`、`update`、`delete`、`rename`、`retype`、`move_layer`、`rebind_endpoint`、`upsert_row`、`delete_row`である。未知actionはdenyする。

Policy内のStableAddressはeffective Project / installed Pack closureの対象kindへ解決できなければならない。Schema変更、Pack update、identity migrationでconstraint参照が削除・移動・kind変更される場合、Runtimeはresulting definitionに対してPolicy compatibilityを検証する。

- address moveで一意に移行できる場合: authorized policy migration planを同じHost transactionへ含める。
- 削除または互換性のない変更: `authoring.policy_incompatible`でSchema commitを拒否する。
- Policyを先に緩めて一時的なunrestricted windowを作らず、definitionとPolicy migrationを同じpublication boundaryで公開する。
- Pack updateはresolved address migrationと全applicable Policyを検証する。

### 5.3 AuthoringGrantSnapshot

```text
AuthoringGrantSnapshot
  actor_ref
  host_document_id
  organization_scope?
  granted_capabilities[]
  graph_constraints
  policy_refs[]
  membership_version
  agent_delegation_digest?
  entitlement_digest?
  issued_at
  expires_at?
  access_fingerprint
```

GrantSnapshotはHostが解決した不変入力であり、client requestから自己申告させない。credential、token、secret、license keyを含めない。

client向けCapabilityManifestには完全なGrantSnapshotではなく次のsummaryだけを返す。

```text
AuthoringGrantSummary
  granted_capabilities[]
  constrained_capabilities[]
  policy_etag
  access_fingerprint
  expires_at?
```

具体的なdeny policy、他Actorのgrant、秘匿Type / Column address listをsummaryへ含めない。候補Type等は通常のAccess適用済みschema read operationから取得する。

effective grantは次の積集合である。

```text
role grants
  ∩ Instance / Organization / Workspace / Project policies
  ∩ agent delegated scope
  ∩ EntitlementSnapshot
  ∩ host application constraints
  ∩ storage write capability
```

### 5.4 Preset

PresetはPolicy生成の便宜機能であり、Access semanticsを分岐させるmodeではない。

| Preset | 標準grant |
| --- | --- |
| `full_authoring` | 全Authoring Capability |
| `fixed_semantic_model` | `graph:write`、optional `query:write` / `view:write`。`schema:write`、`source:maintain`、`package:manage`なし |
| `data_entry` | constraint付き`graph:write`のみ |
| `view_consumer` | write capabilityなし |

Preset適用後は完全なAuthoringPolicyへ展開し、decisionやAuditへpreset名だけを保存しない。

### 5.5 Meta-schema capability review

EngineのAuthoringImpactはLDL上のSchema Definition変更とGraph instance変更を正確に分類するが、productがGraph instanceを独自Schemaとして解釈するかまでは判定しない。固定semantic modelを提供するHostは、End Userへ公開するEntityType、RelationType、Column、operation、import経路をproduct capabilityとしてreviewしなければならない。

MetaEntityType、MetaRelationType、ExtensionAttribute等のinstanceからEnd User固有の型、field、関係、constraint、semantic ruleを生成または解釈する場合、Engine operationが`graph:write`であってもlicense上はGeneral-Purpose Schema Authoringである。`fixed_semantic_model` presetだけでこれを許可してはならない。

AuthoringPolicyはこのproduct capability reviewの結果をconstraintへ反映できるが、Engine、Access、Runtimeがbusiness offeringのlicense区分を自動判定してはならない。

## 6. Decision Contract

```text
EvaluateAuthoringInput
  request_intent: preview | propose | apply | publish
  base_revision_digest
  authoring_impact?
  host_operation_impacts[]
  grant_snapshot

AuthoringDecision
  outcome: allow | deny | approval_required
  evaluation_digest
  authoring_impact_digest?
  host_operation_impact_digests[]
  access_fingerprint
  required_capabilities[]
  missing_capabilities[]
  constraint_violations[]
  approval_rule_refs[]
  decision_digest
  diagnostics[]
```

`base_revision_digest`はRuntimeがcanonicalなcurrent `CommittedRevisionRef`から生成し、client自己申告値と不一致ならAccess評価前に拒否する。`evaluation_digest`はこのrevision digest、完全なGrantSnapshot（Actor、access fingerprint、Policy refs / membership version、delegation digest / expiryを含む）、request intent、AuthoringImpact digest、canonical orderのHostOperationImpact digestから生成する。Accessは両impactのrequired capabilityを合成し、batch全体へ一つのdecisionを返す。partial allowを返さない。`approval_required`はgrantを増やさず、必要capabilityを持つapproverによる新しいdecisionを要求する。

wire互換性: Access Protocol v1の`EvaluateAuthoringInput.base_revision_digest`は必須fieldである。wire clientは更新済みschemaからbindingを再生成し、Runtimeが返したcurrent revisionを基に評価を要求する。LayerDraw Runtimeのin-process `Authorize`は移行期間中、空の値だけをtrusted current revisionから補完するが、異なる非空値を受理しない。

stable failure code:

| Code | Meaning |
| --- | --- |
| `authoring.capability_denied` | required capabilityが無い |
| `authoring.constraint_denied` | Type、RelationType、Layer、Column、action constraint違反 |
| `authoring.policy_changed` | preview後にPolicy / membership / delegationが変わった |
| `authoring.policy_incompatible` | resulting SchemaでPolicy constraint address / actionを解決できない |
| `authoring.impact_changed` | previewとcommitのimpact digestが異なる |
| `authoring.evaluation_changed` | HostOperationImpactまたは合成evaluation digestが変わった |
| `authoring.approval_required` | authorized approverのdecisionが必要 |
| `authoring.approver_insufficient` | approverもrequired capabilityを満たさない |
| `authoring.untrusted_policy_input` | client自己申告policy / grantを受け取った |
| `authoring.external_change_quarantined` | restricted projectに未認可の外部変更を検出した |

## 7. Publication Enforcement

### 7.1 Preview

```text
request
  -> Engine Workbench preview
  -> AuthoringImpact
  -> Access EvaluateAuthoring(preview | propose)
  -> preview result + decision
```

WorkbenchPreviewResultは`authoring_impact`、`required_capabilities`、`impact_digest`を必須とする。host-backed previewはさらにAuthoringDecisionを返す。portable Engine previewはActorを知らないためimpactだけを返し、allowを主張しない。

### 7.2 Document commit

```text
Runtime commit
  -> current head / policy / membershipを固定
  -> Engineで再preview
  -> AuthoringImpactとHostOperationImpactからevaluation digestを再計算
  -> resulting definitionに対するPolicy compatibilityを検証
  -> Accessでapply / publish decisionを再評価
  -> allow時だけDocumentStoreへstage / publish
```

preview tokenはbase revision、evaluation digest、access fingerprint、policy / membership versionへ束縛する。commit時に1つでも変われば再previewを要求する。preview時allowをcommit時までcacheして使わない。

agentのpreview proofは`propose` intentへ束縛する。commitはそのproposal proofを検証した後、current grantで`apply`を独立再評価し、storage / external staging後かつpublication直前に`publish`をもう一度再評価する。`propose`だけのdelegationがpreview proofを取得してもapply permitにはならない。

Runtimeはdefinitionを先にpublishしてからAccessを確認してはならない。state、asset、history、audit、realtime outboxも同じdecision digestとoperation IDへ束縛する。

`created_at`、`updated_at`、provenance等のsystem-managed state deltaは、許可済みprimary operationの派生結果としてRuntimeが生成する。clientがsystem fieldを直接指定したり、service actorのcredentialを使ってAuthoring decisionを迂回したりしてはならない。

### 7.3 Host metadata commit

Project settings、backend / package policy等のHost metadataはServer Applicationまたはlocal Host Applicationがpublication ownerとなる。

1. current metadata ETag、Actor、membership、Policyを固定する。
2. owner protocolからHostOperationImpactを生成する。
3. Access decisionを評価する。
4. metadata ETagとaccess fingerprintをpreconditionにconditional writeする。
5. Auditとcapability changed eventを同じoperation IDへ束縛する。

Server ApplicationはOrganization / Workspace / Project metadataをRuntimeへ押し込まず、自身のapplication transactionで強制する。metadata writeとDefinition writeを一つのintentで行う場合はsagaまたはtransaction recordへ同じevaluation digestを固定し、一方だけを通常resourceとして公開しない。

### 7.4 全書込経路

正本Definitionを変更する次の経路は、必ずbefore / afterをEngineへ渡し、AuthoringImpactとAccess decisionを通す。

- semantic operation batch
- scoped LDL fragment
- source patch
- source tree / `.ldl` import
- `.layerdraw` importまたは既存Projectへのreplace
- external format import
- Pack install、update、remove、repair
- Template適用
- history restore
- reconcile / external head採用
- realtime checkpoint / commit
- MCP / SDK / HTTP / Wails / VSCodeからのwrite
- autosaveによるCommitted Revision公開

asset、Project Host settings、Package transaction等のLDL外authoring writeはtyped HostOperationImpactとAccess decisionを通す。一つのuser intentが両方を含む場合は同じevaluation digestとpublication transactionへ束縛する。AuthoringPolicy管理は変更前snapshotに対するResource Access decisionを通す。transport、command名、binary、frameworkごとの例外を作らない。

### 7.5 RegistryとTemplate

`package:manage`だけではSchema変更を許可しない。Registry planのresulting source treeをEngineが分類し、`schema:write`、`query:write`、`view:write`等も要求する。

固定semantic modelの新規Project作成は次を原子的に行う。

1. authorized publisher / administratorがTemplateとAuthoringPolicyを対応付ける。
2. HostがProject IDとPolicy bindingを先にstageする。
3. schema capabilityを持つservice actorが検証済みTemplate / Packを適用する。
4. definition publicationとPolicy bindingを同じbootstrap transactionで公開する。
5. tenant userへ`fixed_semantic_model`等のgrantを発行する。

tenant userへ一時的な`schema:write`を与えて初期化してはならない。Marketplace / Published AppはPublic Core外のlistingやcommerceを持てるが、Project bootstrapはこの契約を使う。

### 7.6 Out-of-band change

restricted ProjectのDocumentStoreをRuntime以外が変更した場合、Runtimeはexternal headを自動採用しない。

- ActorとPolicyを解決できる変更: reconcile previewとしてimpact判定する。
- Actorまたはauthorizationを証明できない変更: quarantineし、`authoring.external_change_quarantined`を返す。
- authorized schema administrator: review後に新revisionとして採用できる。

Git repositoryやBYOSを利用者が直接writeできる構成では、LayerDraw内のAuthoring Policyはworkflow guardでありfilesystem DRMではない。強制保証を表示する場合はbranch protection、review、storage credential分離等をHost要件にする。

### 7.7 Realtime / multi-actor commit

単一Actorの通常requestは、batch全体がそのActorのgrantを満たす場合だけcommitする。複数Actor / AI agentのWorking Documentは、checkpoint実行者やautosave service actorのgrantへ全変更を付け替えない。

```text
AuthorizedContribution
  contribution_id
  actor_ref
  agent_delegation_digest?
  base_working_generation
  operation_digest
  authoring_impact_digest
  decision_digest
  access_fingerprint
  policy_versions[]
```

- working changeをauthoritative stateへ受理する前に、そのcontribution単位でpreview / decisionを行う。
- checkpoint時はRuntimeがcurrent headと全contributionからfinal AuthoringImpactを再計算し、各impact entryを原因contributionへ決定論的に対応付ける。
- cascade / mergeにより生じたentryは原因contributionのActorが必要capabilityを持つ場合だけcoveredとする。原因を一意に決められないentryはrebase / reviewを要求する。
- 全contributionのPolicy、membership、agent delegationをcurrent versionで再評価し、一件でもdeny、stale、uncoveredならcheckpoint全体をpublishしない。
- checkpoint実行者とautosave service actorはpublication triggerであり、contributionのAuthoring Capabilityを代替しない。
- SemanticOperationLogとAuditは最終revisionだけでなくcontribution ID、Actor、decision digestの対応を保持する。

## 8. SDK

### 8.1 Viewer

Viewerは正本writeを持たないためAuthoring Capabilityを必要としない。

### 8.2 Browser Editor

Browser Editorはconnected RuntimeのCapabilityManifestとAuthoringGrantSummaryを使ってcontrolを表示する。authoritativeなfixed-schema enforcementを要求する場合、次のいずれかを必須とする。

- remote `layerdraw-server`
- local `layerdraw-host`
- Host applicationが所有するauthoritative Runtime endpoint

Engine WASMだけのportable editorはAuthoringImpactを返せるが、Actor / Policyを解決できないためsecurity enforcementを主張しない。host callbackへ保存する場合、DocumentProviderはevaluation digestとauthoring proofを受け取り、trusted host側で再評価しなければならない。

### 8.3 Server SDK

Server SDKはRuntime / Accessを含む`layerdraw-host`または`layerdraw-server`へ接続し、AuthoringPolicy binding、Actor context、commit enforcementを利用する。Next.js / Mastra等のhandlerでsubject kindを独自判定しない。

### 8.4 Host injection

```text
AuthoringAccessClient
  get_effective_grant(document_id)
  evaluate_preview(preview_id, evaluation_digest)

DocumentProvider.write_with_precondition
  inputへevaluation_digest、decision_digest、access_fingerprintを追加
```

AuthoringAccessClientはtrusted LayerDraw Access / Runtime endpointへのtransportであり、TS製policy evaluatorではない。clientからgrant capability setを自由入力させない。

## 9. MCP / AI Agent

- `layerdraw.get_capabilities`はeffective AuthoringGrantSummaryを返す。
- schema write不可の場合もSchema readは可能なため、AIは既存型を理解してinstanceを操作できる。
- `apply_operations`は各operation名ではなくAuthoringImpact全体で判定する。
- `preview_operations`は不足capabilityとimpactを返せる。
- `agent:propose`があればdeny対象をproposalとして保存できるがapplyしない。
- tool advertisementを減らしてもgeneric source patch / import toolからの迂回をRuntimeが拒否する。
- delegated agent scopeは委任元Actorのgrantを拡張しない。
- Desktopのagent delegationはdocument / local scope、authoring capability、read / export / propose / apply、expiryへ束縛して0600のHost metadataへ永続化する。失効済みgenerationも永続化し、再起動で復活させない。
- delegated session identityはHostがsessionへ束縛し、preview、commit、SaveRuntime、autosave、asset stagingの各入口でcurrent delegationを解決する。callerがActor / grant / delegation digestを自己申告する別経路を設けない。
- Search、Query、Review、export、MCPのresult providerはraw recordを直接公開せず、AccessのReadBoundaryへ接続する。subject filteringとfield redactionを行ったcopyだけを返し、read / export deny時はraw provider自体を呼ばない。

固定semantic modelでAIへ推奨するworkflow:

```text
get_capabilities
  -> list / search / inspect schema
  -> existing Type / Layer内でgraph operationを生成
  -> preview_operations
  -> constraint violationを修正
  -> apply_operations
```

## 10. UI

- Schema Editor、Pack管理、Layer定義controlは`schema:write` / `package:manage`に従って非表示またはdisabledにする。
- Graph Editorは許可Type、RelationType、Layer、Columnだけを候補表示する。
- capability不足とconstraint違反を同じ「編集失敗」に丸めず、理由を区別する。
- fixed semantic modelであることをProject statusとして表示できる。
- policy変更はResource Access管理操作とし、変更前snapshotで`access:manage`を要求する。`project:configure`へ包含しない。
- UIがdisabledでもAPI / MCP / import側のpublication owner enforcementを省略しない。

## 11. Audit / Realtime

Auditは少なくとも次を保持する。

- Actor / agent / approver
- AuthoringImpact digest
- HostOperationImpact digest
- evaluation digest
- Required Capability
- Policy refs / access fingerprint / decision digest
- allow / deny / approval result
- committed revisionまたはproposal ID
- transport source

deny eventへsecret、redacted value、source全文を保存しない。

Realtime participantはjoin時のAuthoringGrantSummaryを持ち、policy change eventで再取得する。working change受信時に早期判定し、checkpoint / commit時に必ず再判定する。schema write不可Actorのschema changeを他participantへauthoritative working stateとしてbroadcastしない。

## 12. Delivery別の意味

| Delivery | Enforcement |
| --- | --- |
| SaaS | Server ApplicationがPolicyを永続化し、Runtime / Server ApplicationとAccessが各publicationで強制する |
| Self-host | SaaSと同じPublic Server能力。管理者がPolicyを設定し、各publication ownerが強制する |
| Desktop | OS / Host adapterが解決したstable local Actorをlocal ownerとして既定full authoringにする。organization membershipは捏造しない。local agentは永続delegationとRuntime / Accessの再評価に従い、server project接続時はHost decisionに従う |
| VSCode | local fileはOS権限が正。`layerdraw-host`またはserver projectではPolicyを強制する |
| SDK | Viewerはreadonly。Browser EditorはRuntime接続時に強制。Server SDKはRuntime / Accessを使用する |
| MCP Apps | connected hostのgrantを表示し、全applyをhostへ委譲する |
| Marketplace Integration | provider backend / LayerDraw ServerがProject Policyを強制する |

同じAuthoringImpact、HostOperationImpact、AuthoringDecision schemaを使い、deliveryごとにSchema / Graph分類やHost operation mappingを変えない。

## 13. Conformance

1. EntityType変更は`schema:write`、Entity row変更は`graph:write`だけを要求する。
2. RelationType projection変更とLayer変更をSchema変更として分類する。
3. type変更、Layer移動、endpoint変更へGraph constraintを適用する。
4. source patch、import、Registry、restore、reconcileからSchema制限を迂回できない。
5. mixed-capability batchをpartial commitしない。
6. preview後のPolicy / membership変更でcommitを拒否する。
7. `agent:propose`だけのagentがapplyできない。
8. approverがRequired Capabilityを持たない場合に拒否する。
9. UI、MCP、SDK、HTTP、Wails、realtimeが同じimpact / decisionを得る。
10. portable `.layerdraw` importでgrantが移送されない。
11. fixed semantic model bootstrapでtenant userへschema capabilityを付与しない。
12. untrusted external headをquarantineする。
13. asset、Project設定、Package transactionが対応するHostOperationImpactなしで実行できない。
14. AuthoringPolicy変更を変更後Policyや`project:configure`で自己認可できない。
15. Registry transactionの`package:manage`とresulting definition capabilityを同じevaluation digestへ束縛する。
16. multi-actor checkpointでservice actorまたはcheckpoint実行者のgrantへcontributionを付け替えない。
17. Schema変更に伴うinstance / rowの派生変更へ`graph:write`も要求する。
18. fixed semantic modelのproduct capability reviewで、instanceを任意Schemaとして解釈するMeta-modelを検出し、preset名だけで許可しない。
19. evaluation digestがbase revision、Policy refs / membership、agent delegationのいずれかの変更で変わり、stale proofを拒否する。
20. Desktop再起動後もlive delegationだけが復元され、expired / revoked delegationはapply、autosave、asset、MCP経路で復活しない。
21. Search、Query、Review、export、MCPの全resultが同じAccess read projectionを通り、denied requestはunredacted providerを呼ばない。
