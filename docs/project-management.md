# LayerDraw プロジェクト管理方針

## 1. 基本方針

LayerDraw のportable Projectはファイル中心にする。LayerDraw Serverはファイル管理に加えて、複数Organization / Workspace上のProject directoryを提供する。

`Project`は1つの論理LayerDraw documentを指す。ローカルではentry moduleを起点とする`.ldl` source treeまたはimportした`.layerdraw`を開き、サーバー保存時は1つのWorkspaceに所属する1 document / 1 realtime room / 1 revision streamとして扱う。`.layerdraw`は共有・配布・import / export用containerであり、Organization / Workspace所属はHost metadataとしてportable definitionへ含めない。

ログインは LayerDraw の起動条件ではない。ログインが必要になるのは、サーバー保存、共同編集、履歴、共有、AI / MCP token 発行など、操作主体と権限をサーバー側で判定する場合だけである。

## 2. モード

### 2.1 Local Project

- サインイン不要
- `.layerdraw` を import / export できる
- ブラウザ上で新規作成、編集、保存できる
- 権限は OS / ファイルシステム / ブラウザの権限に従う
- 共同編集、共有、サーバー履歴は使わない

### 2.2 Server Project

- サーバー接続後に利用できる
- Organization / Workspace / Project metadata はmetadata repositoryへ、canonical source tree revisionとasset / artifactはDocumentStore / ArtifactStoreへ保存する
- revision を持つ
- 履歴、共有、共同編集、presence の単位になる
- write 操作は Actor と権限を確認する
- 必ず1つのOrganization / Workspaceに所属する
- 単一Server Instanceで複数Organizationを分離できる

### 2.3 External Storage Project

- Google Drive、OneDrive、GitHub、S3 / R2 / MinIO、Nextcloud などを将来 adapter として扱う
- 認証と ACL は保存先に委譲する
- LayerDraw はファイル読み書き、package validation、render、presence を担当する

## 3. Project Hub

アプリの入口として Project Hub を持つ。

Project Hub に置くもの:

- 現在のドキュメント情報
- ローカル新規作成
- サンプル作成
- `.layerdraw` import
- `.layerdraw` export
- サーバー接続
- サーバー保存
- サーバープロジェクト名の変更
- サーバープロジェクトの複製
- サーバープロジェクトの削除
- サーバープロジェクト一覧
- サーバープロジェクトを開く
- revision 表示
- 共有と履歴
- Server Instance / Organization / Workspaceの現在位置と切替
- Organization membershipに基づくWorkspace / Project一覧

未ログイン状態でも Project Hub は開ける。ただし、サーバープロジェクト、共有、共同編集は無効または接続要求を表示する。

## 4. 認証

ログイン方式は独自ユーザー管理ではなく、外部認証への委譲を基本にする。

想定:

- OIDC / SSO
- リバースプロキシ認証
- GitHub OAuth
- Google Workspace
- Microsoft Entra ID
- Keycloak

Local Project では `anonymous` Actor として扱う。

Server Project では `user`、`agent`、`service_account` を Actor として扱い、Instance、Organization、Workspace、Projectの各scopeでmembershipとroleを判定する。Projectには`owner` / `editor` / `viewer`を使う。

UI は認証状態をヘッダーと Project Hub に表示する。認証プロバイダ設定済みの環境ではログイン / ログアウト操作を提供する。認証プロバイダ未設定の環境ではログインフォームを表示せず、サーバープロジェクト操作を disabled にする。未ログイン状態でもローカル編集と `.layerdraw` import / export は使えるが、サーバープロジェクト一覧、保存、共同編集、共有、履歴などの操作はログイン済み Actor を要求する。

現在の Go server 実装では、`NewServer` の library default は `DisabledAuthProvider` として health 以外の Project 操作を拒否する。一方、`cmd/layerdraw-server` はローカルホスト利用を成立させるため、`LAYERDRAW_AUTH_MODE` 未指定時に `dev` cookie auth を使う。`dev` mode は `/auth/dev/login` で署名付き Cookie session を発行し、`/auth/dev/logout` で破棄する。self-host で外部認証に委譲する場合は `LAYERDRAW_AUTH_MODE=header` を指定し、認証済みリバースプロキシまたはホストアプリから `X-LayerDraw-Actor-ID` などの trusted header を渡す。`GET /api/session` は認証プロバイダ設定有無、ログイン済み Actor、ログイン / ログアウト URL を返し、GUI のログイン / ログアウト導線はその URL へ遷移する。フロントエンドから cookie / session を送るため API request は credentials を含めるが、CORS は `LAYERDRAW_CORS_ORIGINS` またはローカル開発 origin に限定する。ローカル開発では `http://localhost:<port>` と `http://127.0.0.1:<port>` の両方を許可し、どちらの URL で開いても Project Hub とログイン導線が同じ状態になるようにする。Realtime WebSocket の query 認証 fallback は `LAYERDRAW_ALLOW_REALTIME_QUERY_AUTH=1` を明示した開発検証時だけ有効にする。OIDC、better-auth adapter 本体は未実装であり、現時点では外部 IdP / host app / reverse proxy と接続する adapter 境界として扱う。

## 5. 対応するもの

- ログインなしでローカル編集できる
- 認証プロバイダ設定済みの環境では Project Hub からログイン / ログアウトできる
- 認証プロバイダ未設定の環境ではログインフォームを表示しない
- Project Hub からローカル新規作成、サンプル、import を実行できる
- サーバー接続後にプロジェクト一覧を表示できる
- サーバー保存時に revision を更新できる
- サーバープロジェクト名変更は`project:configure`を要求し、metadata と DSL の project 名を同時に更新する
- サーバープロジェクト複製はread権限だけで既存grantを持ち出さない。新規Project作成権限、Template / policy適用権限を検証し、元ProjectのAuthoringPolicyを継承する場合もHostが明示bindingした上で実行Actorのgrantを新Project scopeで再評価する
- サーバープロジェクト削除は owner のみ許可し、削除後の現在ドキュメントはローカル編集として残す
- revision mismatch は保存失敗として扱う
- 共同編集 room は Project / Document 単位に作る
- Instance AdminがOrganizationを作成、更新、archiveできる
- Organization Owner / AdminがMember、Team、Service Account、Workspace、Organization policyを管理できる
- Organization / Workspace / ProjectへAuthoringPolicyを設定し、Schema administratorとGraph instance editorを分離できる
- Workspace AdminがWorkspace membershipとProjectを管理できる
- Userは複数Organizationへ所属できる
- Team / Service AccountはOrganizationをまたがない
- Project metadata、search、history、asset、realtime、auditをOrganization scopeで分離する
- fixed semantic modelのProject作成ではPolicy bindingを先にstageし、schema capabilityを持つservice actorによるTemplate適用と同じbootstrap transactionで公開する

## 6. Public Coreに含めないもの

- managed service固有の商用・流通機能
- 任意コードでauthorization ruleを実行する汎用policy engine

AuthoringPolicyは任意コードではなく、closed capability、StableAddress constraint、approval ruleとしてPublic Coreに含める。

SCIM、外部IdP group sync、外部storage adapterはPublic Serverのport / adapterとして追加できるが、Organization / Workspace / Projectの意味を変更しない。
