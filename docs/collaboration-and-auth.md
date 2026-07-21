# LayerDraw 認証・権限・共同編集方針

Realtime room、Working Document / Committed Revision、conflict、CRDT / OT / command-log adapter、commit / broadcast順序は[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 5章を規範とする。

## 1. 基本方針

LayerDraw の認証、権限、共有は draw.io と同様に、保存先またはホストアプリへ委譲する設計を基本にする。

Local / BYOSでは`.ldl` source treeまたは`.layerdraw` artifactの保存先へ認証とACLを委譲する。LayerDraw Serverでは、単一Instance内の複数Organization、Workspace、Project、membership、Team、Service Account、共有権限をPublic Server applicationの責務として持つ。

全モード共通で独自Identity Providerを持つのではなく、ActorはOS、保存先、OIDC、reverse proxy、host application等から解決する。Serverは解決済みActorをOrganization membershipへmapし、共同編集roomとAI / MCP agent scopeを含むAccess decisionを行う。

要件上の結論として、portable definitionの境界はファイル / 保存先、Server applicationの管理境界はOrganization / Workspace / Projectとする。Organization、Workspace、membership、Share、AuditはSelf-hostを含むPublic Server要件であり、managed service固有の商用・流通機能とは分離する。

## 2. 利用モード

### 2.1 Local mode

ローカル端末で `.layerdraw` ファイルを開いて編集するモード。

- サインイン不要
- ファイル所有権は OS / ファイルシステムに従う
- 共同編集なし
- MCP はローカルファイルへのアクセス権限内で動作する

### 2.2 Bring-your-own-storage mode

Google Drive、OneDrive、GitHub、GitLab、Nextcloud、R2、S3、MinIO などの外部保存先に `.layerdraw` ファイルを置くモード。

- 認証は保存先に委譲する
- ファイル権限は保存先の ACL / sharing に従う
- LayerDraw は保存先 adapter を通じて読み書きする
- 共同編集できるかは保存先 adapter と realtime adapter の能力に依存する

### 2.3 Hosted / self-hosted mode

LayerDraw サーバーを組織内またはクラウド上で運用するモード。

- 認証は OIDC / SSO 連携を基本にする
- canonical source tree revision、asset、artifactはDocumentStore / ArtifactStoreに保存する
- Organization / Workspace / membership / Project / ACL / audit metadataはdurable metadata repositoryに保存する
- Server所有storageではLayerDraw ACLを使い、外部保存先では保存先ACLとLayerDraw Access decisionの積集合を使う
- 1つのServer Instanceで複数Organizationを分離し、各OrganizationにWorkspace、Member、Team、Service Accountを持てる

## 3. テナントの考え方

Local / BYOSのportable単位は`.layerdraw`ファイル、LayerDraw Serverのtenant / security単位はOrganizationである。両者を同じ概念にしない。

```text
Storage Provider / Host App
  -> Folder / Space / Repository
    -> project-local .ldl source tree or project.layerdraw
```

```text
LayerDraw Server Instance
  -> Organization
    -> Workspace
      -> Project
        -> Document / Revision / Realtime Room
```

要件上の決定:

- Local mode では tenant を持たない
- Bring-your-own-storage mode では、保存先の folder / space / repository を実質的な境界として扱う
- LayerDraw ServerではOrganization / Workspace / Project階層を必須とし、単一組織利用ではdefault Organization / Workspaceをbootstrapする
- 1つのServerで複数Organizationを管理でき、部署、子会社、顧客ごとにServerを複製することを要求しない
- Organizationはmembership、Team / Service Account、policy、storage / registry binding、audit、search indexの分離境界とする
- Workspaceは1つのOrganizationに属し、Projectの分類、権限継承、共同管理を行う
- User Actorは複数Organizationに所属できるが、Team / Service Accountは1つのOrganizationにscopeする
- Projectのportable definitionとStableAddressにはOrganization / Workspaceを含めず、所属変更をHost metadata operationとして扱う
- いずれのモードでも、共同編集の同期境界は tenant ではなく document / room とする

## 4. ユーザーと Actor

LayerDraw の操作主体は Actor として扱う。

Actor 種別:

- `user`: 人間のユーザー
- `agent`: AI / MCP agent
- `service_account`: 外部連携や自動処理
- `anonymous`: ローカル単体利用

AI / MCP agent は、人間ユーザーの委任または service account として扱う。

実装上、UI は `GET /api/session` を認証状態の source of truth とする。この API は認証プロバイダ設定有無、ログイン済み Actor、外部ログイン / ログアウト URL を返す。ローカル開発用 CLI は既定で `dev` cookie auth を使い、`/auth/dev/login` と `/auth/dev/logout` で署名付き Cookie session を発行 / 破棄する。self-host で外部認証に委譲する場合は `LAYERDRAW_AUTH_MODE=header` で trusted header から Actor を解決し、必要に応じて `LAYERDRAW_LOGIN_URL` / `LAYERDRAW_LOGOUT_URL` を認証プロキシ、IdP、または host app のエンドポイントへ向ける。CORS は `LAYERDRAW_CORS_ORIGINS` で明示した origin に限定し、credential 付き request を任意 origin へ開放しない。Realtime WebSocket も同じ Actor / Project 権限を検証し、query parameter で Actor を渡す fallback は `LAYERDRAW_ALLOW_REALTIME_QUERY_AUTH=1` を明示した開発検証時だけ使う。

## 5. 権限モデル

概念ロール:

- Instance: `instance_admin`
- Organization: `organization_owner` / `organization_admin` / `organization_member`
- Workspace: `workspace_admin` / `workspace_member`
- Project: `owner` / `editor` / `viewer`

原則:

- read 権限がなければ `.layerdraw` を開けない
- write 権限がなければ DSL 更新、保存、共同編集に参加できない
- render は read 権限で許可する
- export は read 権限で許可する
- destructive operation は write 権限に加えて明示確認を要求できる
- Project `write` roleはAuthoring Capability setへ展開して評価し、Schema定義とGraph instanceを一つの包括permissionで許可しない
- 上位roleは下位resourceへの既定権限を与えられるが、Project ACLまたはpolicyによる明示的な制限を無視しない
- すべてのmetadata query、cache、search、history、asset、realtime roomをOrganizationでscopeし、cross-organization漏えいを禁止する

保存先の ACL がある場合、effective accessは保存先ACLとLayerDraw Access decisionの積集合とし、どちらか一方のgrantだけで他方のdenyを昇格させない。LayerDraw Server自身がstorageを所有する場合はLayerDraw ACLを正とする。

### 5.1 Authoring Capability

write capabilityは次へ分離する。

- `schema:write`
- `graph:write`
- `query:write`
- `view:write`
- `reference:write`
- `asset:write`
- `source:maintain`
- `project:configure`
- `package:manage`

roleはこれらのgrant setとconstraintへ解決する。Project `owner` / Organization adminが既定で全grantを持つことはできるが、上位AuthoringPolicyのdenyを無視しない。固定semantic modelではSchema administratorだけが`schema:write`を持ち、editorはconstraint付き`graph:write`と必要に応じて`query:write` / `view:write`を持つ。

Authoring Policyと全書込経路の強制契約は[authoring-access-control.md](authoring-access-control.md)を規範とする。

AuthoringPolicyのcreate / update / bind / unbindはAuthoring Capabilityではなく、Resource Accessの`access:manage`を要求する。変更前のAccess snapshotで判定し、`project:configure`、`schema:write`、変更後Policyから自己昇格させない。agentへは明示委任しても委任元の`access:manage`を超えて付与せず、標準MCP操作面ではPolicy変更toolを公開しない。

## 6. MCP / AI agent 権限

MCP は LayerDraw の強力な操作面になるため、scope を分ける。

規範scope:

- `project:read`
- `dsl:read`
- `schema:write`
- `graph:write`
- `query:write`
- `view:read`
- `view:write`
- `reference:write`
- `asset:write`
- `source:maintain`
- `project:configure`
- `package:manage`
- `render:run`
- `export:write`
- `agent:propose`
- `agent:apply`

AI agent は原則として、まず提案を作る。適用はユーザー承認または変更内容が要求する全Authoring Capabilityを必要とする。`agent:apply`や旧`dsl:write`だけでSchema / Graph writeを許可しない。

## 7. 共同編集 UX

共同編集は draw.io と Figma の体験に寄せる。

具体的には、draw.io のように選択中オブジェクトへ参加者名と参加者色を表示し、さらに Figma のように他ユーザーのカーソルをリアルタイムに表示する。共同編集の可視化対象は座標上の図形だけではなく、LayerDraw の Entity、Relation、View、Layer 選択にも対応させる。

表示する presence:

- 参加者名
- 参加者ごとの色
- カーソル位置
- 選択中 Entity / Relation / View
- 編集中フィールド
- 最終操作時刻

Diagram 上の表示:

- 他ユーザーのカーソルを表示する
- 他ユーザーの選択対象を色付きアウトラインで表示する
- 選択中オブジェクトの近くにユーザー名を表示する
- 同じ Entity / Relation を複数人が見ている場合は参加者色を並べて表示する
- Layer / View 切り替え中でも、同じ document 内の参加者として presence を維持する
- ほかの参加者が別 View を見ている場合は、参加者リスト上で現在の View 名を表示できる
- 同じ Entity を複数 View で表示している場合でも、選択中 Entity の identity は共通に扱う

設定:

- 自分のカーソル共有を on / off できる
- 他ユーザーのカーソル表示を on / off できる
- presence 表示を簡略化できる

編集競合の扱い:

- 同一 Entity / Relation を複数人が選択すること自体は許可する
- transient な Working Document と、全検証を通過した immutable な Committed Revision を分離する
- GUI / MCP / SDK はHost Document IDでscopeされたStableAddressを対象とするsemantic operationを送る
- operation batchはbase revisionとStableAddress keyed own-subject semantic hashを検査し、project revision単位で原子的にcommitする
- command log、CRDT、OT は同期実装の選択肢とし、同じ validation、conflict、commit 契約を守る
- stale revision、same-field concurrent update、delete-versus-update、duplicate ID を黙って上書きしない
- 競合が自動解決できない場合は、対象 Entity / Relation / View と参加者名を示して解決 UI を出す
- 共同編集 presence はあくまで操作状況の可視化であり、排他ロックを基本動作にはしない

## 8. 共同編集データ

永続化するデータ:

- canonical Committed Revision source tree manifest と変更 source blob
- semantic operation log
- document revisionとnormalized definition hash / own-subject / subtree semantic hash
- update metadata
- audit log

永続化しない一時データ:

- カーソル位置
- 選択中オブジェクト
- focus 状態
- typing 状態
- transport 固有の一時 Working Document state

一時データは RealtimeRoom adapter で扱う。

## 9. 同期方式

同期transportはWebSocketによるroom単位のpush、競合検出はdocument revisionとown-subject semantic hashによる楽観的競合制御を共通境界にする。revision付きcommand log、CRDT、OTは`RealtimeRoom` adapterの実装として選択できるが、LayerDraw Engineの意味モデルには埋め込まない。

対応するもの:

- presence snapshot / update / leave
- cursor と selection の一時同期
- revision 付き semantic operation batch
- Working Document の diagnostics 同期
- stale revision と semantic conflict の structured reject
- accepted operation と canonical checkpoint の room broadcast
- operation log の永続化と compaction
- autosave + polling / push fallback

例:

```text
create_subject
update_subject_field
delete_subject
upsert_row
delete_row
create_relation
update_relation_endpoint
rename_subject
migrate_project_identity
move_entity_to_layer
```

raw text 共同編集ではキー入力ごとの全体 format を行わない。構文として完全な変更ノード、明示的な format、または checkpoint の境界で canonical source を生成し、その変更を revision 付き operation として全参加者へ配信する。通常の query、View、render、export、read-only MCP operation は最新の Committed Revision を対象にする。

working changeはEngineのAuthoringImpactで早期判定し、checkpoint / commit時に最新Policy、membership、agent delegationで再判定する。schema write不可participantのSchema変更をaccepted working operationとしてbroadcastしない。multi-actor Working Documentでは各changeをorigin Actor / agent、impact、decisionへ束縛し、checkpoint実行者やautosave service actorのgrantへ付け替えない。Policy変更時は`capability_changed`を通知し、AuthoringGrantSummaryを再取得する。

## 10. draw.io と同じにできる範囲

満たせる範囲:

- サインイン不要のローカル利用
- 保存先に認証と共有を委譲するモデル
- 外部ストレージ連携
- ファイル単位の共有
- 共同編集中のユーザー名、色、カーソル、選択表示
- 自動保存ベースの共同編集
- 選択中オブジェクトへの参加者名と色の表示
- ファイル単位の owner / editor / viewer 相当の共有

LayerDraw 固有で追加が必要な範囲:

- DSL / Graph の semantic validation
- AI / MCP agent の scope
- `.layerdraw` zip パッケージの保存整合性
- View / Projection / Renderer の再生成
- Entity / Relation 単位の presence 表示
- View / Layer をまたいだ同一 Entity の選択状態表現
- AI / MCP agent を共同編集 participant として扱う場合の表示と権限制御
- 単一Server Instance内のOrganization isolation
- Organization / Workspace / Team / Service Account管理
- Organization / Project scopeのAudit

満たせない、または別途判断が必要な範囲:

- draw.io が連携先ごとに持つ細かい保存先固有 UI の完全再現
- managed service固有の商用・流通機能
- すべての外部ストレージで同一品質のリアルタイム共同編集を保証すること
- テキスト DSL の同時編集を Google Docs 相当に扱うこと

## 11. Public Core境界外のプロダクト判断

- managed service固有の商用・流通機能
- MCP token の発行 UI
- AI agent の destructive operation 承認フロー

同期方式はRealtime strategy adapterとして選択可能にし、共通のvalidation、conflict、commit契約を変えない。text DSLの共同編集はsemantic operationまたはrevision-protected source patchへ収束させる。
