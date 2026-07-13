# LayerDraw 要件定義

## 1. 概要

LayerDraw は、業務・データ・アプリケーション・インフラ・ネットワークを 1 つの意味付きグラフとして管理し、必要な切り口で 2D / 2.5D の構成図へ投影するためのエディタ / レンダラである。

LayerDrawはdraw.ioのような自由作図ツールではない。図形や座標を直接マスタにするのではなく、EntityType、RelationType、Layer、Entity、Relation、Query、ViewRecipeを定義し、ViewData materializationとRenderer / Exporterが用途に応じた成果物を生成する。

一言で表すと、LayerDraw は「図を書くツール」ではなく「図にできる構造を管理するツール」である。

## 2. 目的

### 2.1 解決したい課題

一般的な構成図は、用途ごとに別々のファイルとして作成されることが多い。

- 業務フロー図
- データフロー図
- アプリケーション構成図
- インフラ構成図
- ネットワーク構成図
- 障害影響図
- データリネージ図

これらを draw.io などで個別に管理すると、次の問題が発生する。

- 同じシステムやコンポーネントが複数の図に重複して登場する
- 1 つの変更を複数の図へ手作業で反映する必要がある
- 業務、アプリ、DB、VM、ネットワークなどのレイヤ横断関係が見えにくい
- 線やノードが増えると図が破綻しやすい
- AI が XML、座標、接続点、折れ線などを安全に編集しにくい

LayerDraw は、図を個別成果物として直接編集するのではなく、構造マスタから必要な図を生成することでこれらを解決する。

### 2.2 基本方針

NG:

```text
図形 + 座標 + 線 = ドキュメント
```

OK:

```text
意味付きグラフ + View 定義 = ドキュメント生成
```

たとえば次のような関係を 1 つの構造データとして保持する。

```text
受注業務
  -> 受注API
  -> 受注DB
  -> app-prod-01
  -> prod-subnet
```

この構造から、用途に応じて業務ビュー、アプリケーションビュー、インフラビュー、ネットワークビュー、障害影響ビュー、データリネージビューなどを生成する。

## 3. 想定ユーザー

### 3.1 主なユーザー

- 業務システムの設計者
- ソリューションアーキテクト
- インフラ / ネットワークエンジニア
- データ基盤 / データ連携の設計者
- SRE / 運用設計担当
- 提案資料や設計書を作成するエンジニア
- AI を使ってシステム構造を整理、更新したいユーザー

### 3.2 利用シーン

- 提案資料用の概要構成図を生成する
- 設計書用の詳細構成図を生成する
- 障害発生時に影響範囲図を生成する
- 業務からアプリ、DB、インフラ、ネットワークまでの依存関係を追跡する
- データの発生源、加工、保存、利用先をデータリネージとして可視化する
- 既存システムの棚卸し結果から構成図を生成する
- AI に構造を追加、更新、抽出させる

## 4. スコープ

### 4.1 対応するもの

LayerDraw は、中核である「意味付きグラフからビューを生成し、GUI / DSL / AI / ローカル API の全経路から扱える」製品体験を実装する。

#### 構造マスタ

- Entity を定義できる
- Relation を定義できる
- Layer を定義できる
- Entity を Layer に所属させられる
- Entity 同士の包含関係を表現できる
- Entity / Relation に種別、名前、説明、メタデータを持たせられる

#### ビュー定義

- View を定義できる
- View ごとに表示対象の Layer を選択できる
- View ごとに表示対象の Entity / Relation をフィルタできる
- View ごとに抽象度を指定できる
- Entity の展開 / 折りたたみを指定できる

#### レンダリング

- 構造マスタと View 定義から 2D 図を生成できる
- Layer を奥行きまたは帯として表現する 2.5D 図を生成できる
- ノード、グループ、関係線を自動レイアウトできる
- 同一 Layer 内の Relation と Layer 横断の Relation を区別して表示できる
- 生成結果を SVG または PNG として出力できる
- ローカルホスト可能な Web アプリとしてブラウザ上でプレビューできる
- browserではTS Renderがlayout / renderingを行い、LDL解釈が必要なclient提供形態はGo Engine WASM Workerを使える構造にする

#### 編集体験

- GUI 中心で Entity / Relation / Layer / View を編集できる
- draw.io に近い直感的な編集体験を持つ
- 配置できる図形は自由図形ではなく、プリセットされた Entity 種別を中心にする
- 生成された図をプレビューできる
- View を切り替えて複数の図を確認できる
- 編集対象 Layer または Layer 間関係を選択できる
- ノードのグループ、表示順、抽象度など、意味を壊さない範囲の表示調整ができる
- 主画面は GUI 編集を中心とし、DSL / JSON の直接編集や常時表示は行わない
- DSL / JSON は import / export、AI / MCP、検証、差分確認の補助経路として扱う

#### 認証 / 権限

- 認証、共有、テナント境界は draw.io と同じく「ファイルと保存先を中心にしたモデル」を基準にする
- draw.io と同様、認証・権限・共有は保存先または連携先プラットフォームへ委譲する設計を基本にする
- ローカル単体利用ではサインイン不要で使える
- 認証プロバイダ設定済みの環境では、ヘッダーと Project Hub からログイン / ログアウトできる
- UI は `GET /api/session` を使い、認証プロバイダ設定有無、ログイン済み Actor、ログイン / ログアウト URL を判定する
- Cookie / session 連携のため API request は credentials を含めるが、許可 origin は明示設定またはローカル開発 origin に限定する
- 認証プロバイダ未設定の環境では、ログインフォームを表示せず、サーバープロジェクト操作を disabled にする
- 未ログイン状態ではローカル編集、`.layerdraw` import / export、SVG export は使える
- サーバープロジェクト操作はログイン済み Actor を必要とする
- Google Drive、OneDrive、GitHub、GitLab、Confluence、Nextcloud などのような外部保存先 / ホストアプリ連携を将来想定する
- hosted / self-hosted では OIDC などの外部 IdP と連携できるようにする
- Local / BYOSの`.layerdraw`アクセス権は保存先のファイル権限を正とする。LayerDraw Serverが外部保存先を使う場合は保存先ACLとLayerDraw Access decisionの積集合、Server所有storageではLayerDraw ACLを正とする
- Local / BYOSで利用者が正本fileを直接writeできる場合、Authoring Policyはアプリ内workflow guardでありOS権限を超えるDRMを主張しない。fixed-schemaの強制保証にはRuntimeを唯一のpublication pathとするか、branch protection / storage credential分離を要求する
- Local / Desktopのファイル利用ではOrganization / Workspaceを要求しない
- `layerdraw-server`ではInstance / Organization / Workspace / ProjectのHost metadata階層を標準とし、単一Serverで複数Organizationを分離して管理できる
- 単一組織のSelf-hostでも別モデルを作らず、bootstrap時にdefault Organization / Workspaceを作成する
- Organizationはtenant、security、membership、policy、storage / registry binding、audit scopeの境界、WorkspaceはOrganization内のProject整理・共同管理単位とする
- Userは複数Organizationへ所属でき、TeamとService AccountはOrganizationにscopeする
- Local mode と bring-your-own-storage mode では、LayerDraw 内部に独自のユーザー DB やテナント DB を必須にしない
- self-hosted では、OIDC、リバースプロキシ認証、社内 IdP、または保存先 ACL のいずれかに接続できる adapter 構造にする
- MCP / AI agent はユーザー委任または service account として扱い、scope 付き token で権限を制御する
- Schema定義とGraph instance編集を別のAuthoring Capabilityとして制御し、`schema:write`を持たないActorでも既存型の範囲でEntity、Relation、rowを編集できるようにする
- Authoring PolicyはHost metadataとしてInstance / Organization / Workspace / Projectへ設定し、`.ldl`、`.layerdraw`、`.ldpack`へgrantを保存しない
- semantic operation、fragment、source patch、import、Pack / Template、restore、reconcile、realtime、MCP、SDKの全書込経路で同じAuthoring Access decisionを強制する
- GUIの非表示やMCP tool制限だけをsecurity boundaryにせず、RuntimeまたはServer / Host Applicationが各resourceのpublication直前にPolicyとimpactを再評価する
- この方針で、ローカル利用、外部保存先連携、self-hosted に必要な認証 / 共有要件は満たす
- Server Instance管理、Organization / Workspace管理、User / Team / Service Account、監査証跡、共有policyはSelf-hostを含むPublic Serverの要件として扱う

#### プロジェクト管理

- Projectは1つの論理LayerDraw documentを指し、正本definitionはentry moduleを起点とする`.ldl` source treeとする。`.layerdraw`はProjectを共有・配布・import / exportするportable containerである
- ローカルでは `.layerdraw` ファイルを import / export して管理する
- サーバー保存時は Project metadata、revision、history、package を管理する
- Project Hub から新規作成、サンプル作成、import、サーバー接続、サーバー保存、サーバープロジェクト一覧、プロジェクトを開く操作ができる
- 未ログインでも Project Hub は使える
- Project Hub は現在の認証状態を表示する
- Project Hub は認証プロバイダ設定済みの環境でのみログイン / ログアウト操作を提供する
- ログインが必要なのはサーバー保存、共同編集、共有、履歴、AI / MCP token 発行など、Actor と権限を判定する操作に限る
- Projectの同期境界はHost Document ID / realtime roomとし、Organizationやportable container fileを同期単位にしない
- Server Projectは必ず1つのOrganizationとWorkspaceに所属する。所属情報はHost metadataであり、`.ldl`、Project StableAddress、portable `.layerdraw` definitionへ含めない
- ProjectをWorkspace間で移動してもdefinition identityを変更しない。Organization間移動はACL、storage binding、registry policy、audit scopeを検証するHost operationとして扱う

#### 共同編集

- draw.io と同様、同一ドキュメントを複数ユーザーで編集できる共同編集体験を目指す
- 参加者ごとに名前と色を割り当てる
- 他ユーザーのカーソルを表示できる
- 他ユーザーが選択中の Entity / Relation / View を色付きアウトラインと名前で表示できる
- Figma のように、他ユーザーのカーソル位置、選択範囲、編集中対象がリアルタイムに分かる
- draw.io の「選択中オブジェクトに参加者名と色が出る」体験を基本にし、Figma のようなリアルタイムカーソル表示を追加する
- 共同編集の単位は `.layerdraw` document / realtime room とし、テナント単位ではなく開いているドキュメント単位で同期する
- 変更は自動保存され、他ユーザーへ反映される
- カーソル共有とリモートカーソル表示は設定で切り替えられるようにする
- カーソル、選択中オブジェクト、focus、typing 状態は一時 presence として扱い、永続データにはしない
- 編集中の Working Document と、全検証を通過した immutable な Committed Revision を分離する
- GUI / MCP / SDKの更新はHost Document IDでscopeされたStableAddressを対象とするsemantic operationとして扱う
- operation batchはbase revisionとStableAddress keyed own-subject semantic hashを検査し、project revision単位で原子的にcommitする
- 同期実装は revision 付き command log、CRDT、OT などから選べるが、同じ validation、conflict、commit 契約を守る
- 通常の query、View、render、export は最新の Committed Revision を対象にする
- raw text 共同編集ではキー入力ごとの全体 format を行わず、完全な変更ノード、明示 format、checkpoint の境界で canonical source を生成する
- WebSocket room、presence snapshot / update、参加者カーソル、選択対象、revision 付き semantic operation / checkpoint update を実装する
- stale revision、same-field concurrent update、delete-versus-update、duplicate ID は黙って上書きせず structured conflict にする

#### AI 利用

- AI が Entity を追加できる形式にする
- AI が Relation を追加できる形式にする
- AI が View を作成、更新できる形式にする
- AI が「この業務から影響範囲を出す」「この Layer だけ切り出す」「このシステムを展開する」といった構造操作を行える形式にする
- AIがFTS / Vector / Hybrid Searchで探索起点を発見し、必要な周辺subgraphだけを段階取得できる形式にする
- AIが明示した部分グラフへPageRank、K-Core、Louvain、SCC、WCCを適用できる形式にする

### 4.2 対応しないもの

- draw.io のような完全自由作図
- ホワイトボード機能
- ピクセル単位の座標編集
- 線の接続点や折れ線の手動調整
- draw.io 互換の XML 編集
- Mermaid の完全互換
- 美麗なアイソメ図専用の作図機能
- CAD やゲームエンジンのような本格 3D モデリング
- temporal graph semantics。valid-time / transaction-time、時点指定Query、期間・window traversal、temporal relation / cardinality、bitemporal model
- 任意コードでauthorization ruleを実行する汎用policy engine
- managed service固有の商用・流通機能

## 5. 主要機能

### 5.1 Entity 管理

Entity は、LayerDraw における意味のある対象を表す。単なる四角形ではなく、業務、アプリケーション、API、DB、VM、Subnet などの構成要素そのものを表す。

例:

```text
Entity: 受注管理システム
  contains:
    - 受注画面
    - 受注API
    - 受注DB
    - app-prod-01
    - db-prod-01
    - app-subnet
```

Entity は View によって異なる粒度で表示される。

- 業務ビューでは 1 ノードとして表示する
- アプリケーションビューでは API 群として表示する
- インフラビューでは VM / DB として表示する
- ネットワークビューでは Subnet / Firewall / Route として表示する

Entity の包含や階層は Entity 自身の `parent` / `children` field ではなく、containment semantic kind を持つ typed Relation で表現する。これにより query、validation、View変換、exportが同じRelation factを使う。

例:

```ldl
entities system @application {
  order_system "受注管理システム"
}

entities api @application {
  order_api "受注API"
}

relations contains {
  order_system_contains_order_api: order_system -> order_api
}
```

階層関係の制約:

- Relationの両endpoint Entityは存在しなければならない
- RelationTypeのendpoint type / Layer制約を満たさなければならない
- self relation、cardinality、duplicateはRelationTypeの宣言に従う
- containment / hierarchy cycleはRelationTypeまたはView contractが禁止する場合にvalidation errorにする
- Entityへ別の`parent` / `children`正本を持たせない

### 5.2 Relation 管理

Relation は Entity 同士の意味付き関係を表す。

Relation の例:

- `uses`: 利用する
- `calls`: 呼び出す
- `reads`: 読み取る
- `writes`: 書き込む
- `runs_on`: 上で動作する
- `deployed_to`: 配置される
- `connected_to`: 接続される
- `depends_on`: 依存する
- `contains`: 含む
- `impacts`: 影響する

Relation は線そのものではなく、構造上の意味を持つ。Renderer は Relation の種類と View の目的に応じて線の表示方法を決める。
Relation の種類は単なるラベルではなく RelationType として定義する。RelationType は semantic kind、endpoint 制約、cardinality、表示ラベル、属性列、View 変換ルール、render hints、export schema を持つ。
View、Composed Diagram、Matrix、Flow、Impact、Excel relation sheet は RelationType の意味と projection rule に依存するため、RelationType System は ViewData 実装の前提である。

### 5.3 Layer 管理

Layer は構造上の階層または関心領域を表す。

標準想定 Layer:

- 業務 Layer
- データ Layer
- アプリケーション Layer
- VM / OS Layer
- ハードウェア Layer
- ネットワーク Layer

Layer は 2.5D 表現における奥行き方向として扱う。

### 5.4 View 管理

View は、構造マスタをどの切り口で図にするかを定義する、ユーザー向けの概念である。

View は「静的な図面ファイル」ではなく、「この目的のために、どの Layer / Entity / Relation を、どの抽象度で見るか」という表示レシピを表す。

View は従来 Excel sheet や個別図面で手動管理していた成果物に相当する。LayerDraw では、業務フロー、データフロー、アプリケーション構成、OS / VM、ハードウェア、ネットワーク、本番 / DR環境、地域、責任分界などの切り口を View として保存する。View は表示対象のコピーではなく、正本グラフに対する抽出・投影レシピであるため、Entity / Relation / Attribute の更新は関連 View の出力へ自動的に反映される。

標準想定 View:

- 業務ビュー
- アプリケーションビュー
- インフラビュー
- ネットワークビュー
- 障害影響ビュー
- データリネージビュー

Queryは表示対象Layer、EntityType、RelationType、root、typed predicate、traversal方向・深度・cycle policyを指定する。ViewRecipeはcategory、intent、Query / Diff source、typed shape、RelationType projection override、export recipeを指定する。抽出条件をViewへ重複保存しない。

FTS / Vector / Hybrid SearchはQuery rootを発見するためのProject Searchであり、SearchHitの順位やscoreをView selectionとして保存しない。Viewへ使う場合は確認済みStableAddressまたは一般化したtyped predicateをQueryへ明示する。Graph Analysisも派生結果であり、Master GraphやView selectionを暗黙更新しない。

Query / View定義はproject-local `.ldl` source treeに保存し、`.layerdraw`へそのsource treeを格納する。ViewData / RenderDataはViewを実行した中間表現であり、正本として保存しない。`.layerdraw` packageには必要に応じてViewごとのpreview / SVG / PNG / CSV / PDFなどのartifactを同梱できる。

`abstraction` は Renderer の表示密度として効かせる。

- `summary`: ノード名中心のコンパクト表示。提案資料や俯瞰用に、型・Layer・ID・説明などの詳細行を省略する
- `normal`: 標準表示。ノード名に加えて Entity 種別と Layer 名を表示する
- `detail`: 詳細表示。Entity 種別、Layer 名、Entity ID、説明を表示し、設計書やレビュー用に情報量を増やす

編集中の一時的な展開 / 折りたたみ状態はUI stateであり、Master Entityへ保存しない。永続的な階層表現はcontainment Relation、RelationType projection、typed tree / diagram shapeからViewData occurrenceとして決定論的に生成する。

### 5.5 ViewData materialization

ViewData materializationは、Queryが選択した意味付きグラフをViewRecipeのtyped shapeへ変換する内部処理である。

ViewDataは基本的にユーザーへ直接露出しない。ユーザーはQuery条件とViewRecipeを編集し、LayerDraw内部でrenderer / exporter共通のsemantic ViewDataを生成する。

この文脈での理解は以下の通り。

- Query: Master Graphから対象Entity / Relation / rowを選択するrecipe
- ViewRecipe: 「何のために、どのtyped shapeで見るか」を指定するrecipe
- ViewData materializer: 選択結果をrenderer非依存のtyped ViewDataへ変換する処理
- Renderer: ViewDataからRenderDataを生成する処理
- Export Planner / Serializer: ViewDataとExportRecipeからExportPlanを生成し、ExportArtifactとSource Manifestへserializeする処理

ViewData materializerは以下を担当する。

- Query結果とsource referencesを受け取る
- RelationType projectionとView overrideを決定論的にmergeする
- Relationをedge、nested occurrence、overlay、badge、support itemへ変換する
- table、matrix、tree、flow、context、diff固有のtyped ViewDataを生成する
- 集約後も元Entity / Relation / row / columnのStableAddress source refsを保持する

### 5.6 Renderer

RendererはViewDataからRenderDataを生成して実際の図として描画する。Exporterは同じViewDataから媒体固有artifactを生成する。

Renderer は以下を担当する。

- ノード配置
- グループ表示
- Layer 表示
- Relation の線描画
- ラベル表示
- SVG / PNG 出力
- 図の視認性を保つための自動レイアウト

Renderer はユーザーや AI に座標編集を強要しない。見た目の整形は Renderer の責務とする。

### 5.7 AI 操作

AI統合ではLayerDraw DSLを正本として扱う。AIは正確なsymbol lookupにSemanticIndex、曖昧な自然言語要求にFTS / Vector / Hybrid Searchを使い、必要なdeclaration / rows / neighborsだけをscoped LDLまたはbounded subgraphとして読み、MCP経由で検証・更新・ViewData生成を行う。全文読取を標準にしない。

AIに自由なJSON graphや図形座標を直接編集させない。通常更新はHost Document IDでscopeされたStableAddressを対象にしたsemantic operation、大量作成はownerと許可kindを固定したscoped LDL fragmentをMCP / APIへ渡し、Go Workbenchが対象CST nodeだけをcanonical sourceへ反映する。source patchはbase revision、own-subject hash、source rangeを要求する明示的な低レベル経路として扱う。

AIのapply権限はAuthoring Capabilityを拡張しない。固定semantic modelではAIがSchemaを読み、許可されたEntityType、RelationType、Layer、Columnの範囲でGraph instanceを編集する。Schema変更案は`agent:propose`でproposal化できるが、`schema:write`を持つActorの承認なしにcommitしない。

AI に任せたい操作:

- Layer を追加、更新、削除する
- Entity を追加する
- Relation を追加する
- View を作る
- 指定業務から影響範囲を抽出する
- 指定 Layer だけを切り出す
- 指定システムのcontainment RelationとView projectionを確認し、適切な階層Viewを提案する
- 構造マスタの不足や矛盾を指摘する
- DSL の差分を提案する
- 構造操作 JSON を提案し、MCP / API 経由で適用する
- MCP 経由で構造、View、ViewData、検証結果を取得する
- MCP 経由でProject Search、bounded subgraph inspection、Graph Analysisを実行する
- MCP 経由で DSL 更新、検証、プレビュー生成を実行する

AI に任せたくない操作:

- 箱を 20px 右へ動かす
- 矢印の折れ線を手動調整する
- 接続点を指定する
- draw.io XML を直接書き換える
- 図形座標を直接メンテナンスする
- `.layerdraw` zip パッケージを直接バイナリ編集する

### 5.8 Authoring Access

Authoring AccessはDefinition変更後の意味をGo EngineがAuthoringImpactへ分類し、versioned owner protocolがLDL外writeをHostOperationImpactへ固定する。Go AccessがActor / role / agent delegation / policyと両impactを照合し、document / asset / package適用はGo Runtime、Host metadataはServer Applicationまたはlocal Host Applicationがpublication直前に原子的に強制する。

標準capability:

- `schema:write`: EntityType、RelationType、Column、Constraint、Layer
- `graph:write`: Entity、Relation、row、type / Layer / endpoint binding
- `query:write`
- `view:write`
- `reference:write`
- `asset:write`
- `source:maintain`
- `project:configure`
- `package:manage`

`project:write`や`dsl:write`を包括的な迂回grantとして使わない。Graph writeには許可EntityType、RelationType、Layer、Column、actionのconstraintを設定できる。mixed-capability batchは必要capabilityを全て満たす場合だけ原子的にcommitする。

AuthoringPolicy自体のcreate / update / bindはAuthoring CapabilityではなくResource Accessの`access:manage`で認可する。変更後Policy、client自己申告grant、`project:configure`による自己昇格を禁止する。

Authoring Policyの規範は[authoring-access-control.md](authoring-access-control.md)に従う。

## 6. 画面構成

画面構成は、GUI による構造編集、ビュー定義、プレビューを中心にする。テキスト編集は主操作ではなく、AI 連携、差分確認、インポート / エクスポートのための補助機能として扱う。

### 6.1 構造エディタ

- Entity 一覧
- Relation 一覧
- Layer 一覧
- 選択中 Entity の詳細
- 選択中 Relation の詳細
- プリセット Entity 種別からの追加
- Relation 種別を選んだ接続作成
- 編集対象 Layer の切り替え
- Layer 間 Relation の編集
- 2D 無限キャンバスでの Entity 配置と Relation 作成
- Three.js によるレイヤー積層ビュー
- effective AuthoringGrantに従いSchema controlとGraph controlを分離し、fixed semantic modelでは許可Type / RelationType / Layer / Columnだけを編集候補にする
- capability不足とconstraint違反を区別し、Projectがfixed semantic modelであることを表示できる

### 6.2 Search / Analysis Workbench

- 自然言語またはkeyword入力
- lexical / semantic / hybrid mode
- Layer、Type、subject kind filter
- score signal、matched field、bounded snippet
- SearchHitから周辺subgraphを展開
- 選択hitをQuery rootへ固定
- QueryResultまたは明示selectionに対するPageRank、K-Core、Louvain、SCC、WCC
- 分析結果を正本へ採用する場合のpreview / approve

### 6.3 View エディタ

- View 一覧
- 表示 Layer の選択
- Entity 種別フィルタ
- Relation 種別フィルタ
- 起点 Entity の指定
- 展開深度の指定
- 抽象度の指定

### 6.4 Diagram プレビュー

- 選択中 View の図を表示
- 2D / 2.5D 表示の切り替え
- Entity の展開 / 折りたたみ
- Layer の表示 / 非表示
- SVG / PNG エクスポート
- 共同編集時は他ユーザーのカーソル、選択範囲、名前、色を表示する

### 6.5 検証ビュー

- 参照切れ Entity の検出
- 未使用 Entity の検出
- 循環依存の検出
- Layer 未設定 Entity の検出
- View で表示できない Relation の検出

### 6.6 2D / 2.5D可視化

2D 無限キャンバスとThree.jsによる2.5D可視化を標準の構造確認面として扱う。

将来像:

- Layer が薄い平面として奥方向に重なる
- 各 Layer 上に Entity が配置される
- Layer 内 Entity 間の Edge と Layer 間 Edge を同時に可視化する
- 3D ビュー上でドラッグ、回転、ズームしながら構造を確認できる

## 7. データ

### 7.1 データモデル

LayerDraw の中心データは以下で構成する。

- `Project`
- `EntityType`
- `RelationType`
- `Layer`
- `Entity`
- `Relation`
- `Query`
- `ViewRecipe`
- `Reference`

ViewData / RenderDataは永続化する正本ではなく、Master GraphとViewRecipeから決定論的に生成する中間表現として扱う。

### 7.2 Entity

Entity は以下の情報を持つ。

- `id`
- `type`
- `display_name`
- `description`
- `layer`
- `tags`
- `annotations`
- stable row IDを持つ属性行

### 7.3 Relation

Relation は以下の情報を持つ。

- `id`
- `type`
- `from`
- `to`
- `display_name`
- `description`
- `tags`
- `annotations`
- stable row IDを持つ属性行

Relationの方向は`from -> to`であり、別の`direction` fieldを持たない。逆向きの読み方や探索はRelationTypeのlabel / traversal contractで表現する。

### 7.4 Layer

Layer は以下の情報を持つ。

- `id`
- `display_name`
- `order`
- `description`
- `tags`
- `annotations`

### 7.5 View

View は以下の情報を持つ。

- `id`
- `display_name`
- `category`
- `intent`
- exactly one `source` (`query`または`diff`)
- exactly one typed `shape` (`diagram`、`table`、`matrix`、`tree`、`flow`、`context`、`diff`)
- optional RelationType projection overrides
- zero or more export recipes

### 7.6 保存形式

保存形式は、AI と人間の両方が扱いやすく、かつ JSON へ確実に変換できる独自 DSL を第一候補にする。

方針:

- ユーザー編集形式は LayerDraw 独自 DSL を基本にする
- 独自DSLは意味を失わずNormalizedDocumentへ変換でき、source上のコメントとtriviaはlossless CSTで保持できること
- AI には JSON よりも制約の強い DSL を主に書かせる
- 既存のコーディングエージェントが扱いやすいよう、DSL はテキスト差分に強い形式にする
- MCP toolsにより、module / symbol探索、scoped LDL取得、rows / neighbors取得、検証、semantic operation、scoped LDL fragment、プレビュー生成を提供する
- normalized JSON全体の取得はdebug / interoperability用に分離し、通常のAI contextには使わない
- Go Engine内部はlossless CST、typed AST、JSON互換NormalizedDocumentを扱う
- protocol / generated artifactにはmachine-readable schemaとgenerated bindingを用意する
- Mermaid のように、短い記述から図を生成できる体験を目指す

理由:

- JSON は汎用的すぎて AI が自由に書けてしまい、構文や意味のばらつきが出やすい
- 独自DSLのsource textだけでは外部連携や局所索引がしにくい
- そのため、Go Engineがsourceをtyped AST、NormalizedDocument、SemanticIndexへcompileする
- MCP 経由で取得・更新すれば、AI がファイル全体や zip パッケージを壊すリスクを下げられる

import / export:

- `.layerdraw` ファイルとしてプロジェクトを export / import できる
- `.layerdraw` は zip パッケージ形式にする
- Word / Excel ファイルのように、拡張子は `.layerdraw` だが中身は複数ファイルを含むアーカイブとして扱う
- パッケージ内には DSL、正規化 JSON、メタデータ、アセット、プレビューを格納できる
- Renderer の出力は SVG を第一候補とする
- 画像用途として PNG エクスポートを提供する

パッケージ構成案:

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
  assets/
  previews/
  exports/
```

### 7.7 正規化 JSON 表現のサンプルイメージ

独自 DSL は、EntityType / RelationType schema、stable row ID、typed Query / Viewを失わないJSON互換表現へ変換される。以下は概念例であり、正確なgenerated schemaはLDL syntaxと独立してversion管理する。

```json
{
  "project_address": "ldl:project:order_platform",
  "entity_types": [
    {
      "id": "application_service",
      "address": "ldl:project:order_platform:entity-type:application_service",
      "display_name": "Application Service",
      "columns": [
        {
          "id": "environment",
          "address": "ldl:project:order_platform:entity-type:application_service:column:environment",
          "value_type": "enum",
          "options": ["prod", "stg"]
        }
      ]
    }
  ],
  "relation_types": [
    {
      "id": "contains",
      "address": "ldl:project:order_platform:relation-type:contains",
      "semantic_kind": "containment",
      "from": { "role": "container" },
      "to": { "role": "contained" },
      "projection": { "diagram": { "mode": "nest", "parent_endpoint": "from", "child_endpoint": "to" } }
    }
  ],
  "layers": [
    {
      "id": "application",
      "address": "ldl:project:order_platform:layer:application",
      "display_name": "Application",
      "order": 20
    }
  ],
  "entities": [
    {
      "id": "platform",
      "address": "ldl:project:order_platform:entity:platform",
      "type_address": "ldl:project:order_platform:entity-type:application_service",
      "display_name": "受注プラットフォーム",
      "layer_address": "ldl:project:order_platform:layer:application",
      "attribute_rows": []
    },
    {
      "id": "order_system",
      "address": "ldl:project:order_platform:entity:order_system",
      "type_address": "ldl:project:order_platform:entity-type:application_service",
      "display_name": "受注管理システム",
      "layer_address": "ldl:project:order_platform:layer:application",
      "attribute_rows": [
        {
          "id": "production",
          "address": "ldl:project:order_platform:entity:order_system:row:production",
          "values": {
            "ldl:project:order_platform:entity-type:application_service:column:environment": "prod"
          }
        }
      ]
    }
  ],
  "relations": [
    {
      "id": "platform_contains_order_system",
      "address": "ldl:project:order_platform:relation:platform_contains_order_system",
      "type_address": "ldl:project:order_platform:relation-type:contains",
      "from_address": "ldl:project:order_platform:entity:platform",
      "to_address": "ldl:project:order_platform:entity:order_system",
      "attribute_rows": []
    }
  ],
  "queries": [
    {
      "id": "order_impact_scope",
      "address": "ldl:project:order_platform:query:order_impact_scope",
      "select": {
        "layer_addresses": ["ldl:project:order_platform:layer:application"],
        "root_addresses": ["ldl:project:order_platform:entity:order_system"]
      },
      "traverse": { "direction": "both", "max_depth": 4, "cycle_policy": "visit_once" }
    }
  ],
  "views": [
    {
      "id": "order_impact",
      "address": "ldl:project:order_platform:view:order_impact",
      "display_name": "受注業務 影響範囲",
      "category": "impact",
      "source": {
        "kind": "query",
        "query_address": "ldl:project:order_platform:query:order_impact_scope",
        "arguments": {}
      },
      "shape": { "kind": "diagram", "layout": "layered", "composed": true },
      "exports": []
    }
  ]
}
```

`id`は表示・source mapping用のlocal ID、`address`はsemantic identityである。normalized model内の型、Layer、endpoint、Query/View、row/column参照はlocal ID文字列ではなくStableAddressを使う。host境界を越える場合は、このmodel全体をhost-owned Document IDでscopeする。

## 8. 非機能要件

### 8.1 AI フレンドリー

- 構造データはテキストで差分管理しやすいこと
- すべてのaddressable subjectはauthored local IDを持ち、origin・kind・owner chainからStableAddressを決定論的に構成できること
- committed IDの削除は`reserved`、renameは`moves`としてLDLへ永続化し、Git管理されたdefinition単体でも再利用防止とidentity migrationを再現できること
- AI が座標ではなく構造を編集できること
- スキーマ検証により AI の出力ミスを検出できること
- 変更差分をレビューしやすいこと

### 8.2 Git フレンドリー

- プロジェクトデータは Git で管理できること
- 生成画像と構造マスタを分離できること
- 手動編集と AI 編集の差分をレビューできること

### 8.3 レンダリング品質

- ノード数が増えても視認性が破綻しにくいこと
- Layer 内関係と Layer 横断関係を区別できること
- 自動レイアウトの結果が毎回大きく揺れないこと
- 同じ入力から同じ出力を再現できること

### 8.4 拡張性

- Entity 種別を追加できること
- Relation 種別を追加できること
- View 種別を追加できること
- Renderer を差し替えられること
- 将来的に CLI、Web UI、API へ展開できること

### 8.5 パフォーマンス

大規模グラフを前提とし、全文読込や全件描画を通常操作に要求しない。

- list、Search、scoped read、subgraph inspection、Analysis resultは件数・byte数・cursorでboundedにする
- FTS / Vector / Hybridで探索起点を見つけ、選択した周辺だけを取得できること
- Search IndexはCommitted Revisionとの差分で更新し、未完成indexを新revisionとして公開しないこと
- MCPはProject全体をcontextへ入れずに検索、確認、編集できること
- rendererはQuery / Viewで選択したViewDataを入力とし、Master Graph全件描画を前提にしないこと
- 実装ごとの上限値はbenchmarkとrelease profileで定義し、言語仕様上の上限として固定しないこと

## 9. 決定事項と残る未決事項

### 9.1 技術選定

決定:

- ローカルホスト可能な Web アプリとして実装する
- フロントエンドは React を第一候補にする
- GUI とブラウザ上のプレビューを中心にする
- LDL意味論はGo Engineだけが実装し、native library、WASM、stdio sidecar、server-linked artifactとして提供する
- self-hosted / local server構成ではGo + Echoをserver shellにする
- React / TypeScriptはComposer、Viewer、layout、RenderData、visual serializer、host adapterを担当し、LDLを解釈しない
- browser editorはGo Engine WASM Workerを使い、server-backed WebはGo serverを権威hostにする
- RendererはTS Render packageの決定的visual rendererとし、ViewDataからSVG / PNG / bundle exportを提供する
- 自動レイアウトはTS Renderのlayered-2d / stacked-2.5d layoutとし、ViewDataの意味を変更しない
- Cloudflare等のdeploymentはGo Engineを実行できるservice / WASM hostへ接続し、TSで意味論を再実装しない
- リアルタイム共同編集は WebSocket presence / cursor / selection / document update として扱う
- revision 付き command log、CRDT、OT、Durable Objects などの同期方式は `RealtimeRoom` adapter 境界で差し替え、共通の semantic operation / commit 契約を守る

残る未決:

- Cloudflare-first にするか、Go + PostgreSQL + MinIO の self-hosted-first にするか
- Cloudflare 構成で WebSocket 共同編集を Durable Objects へ置き換えるか

### 9.2 スキーマ / DSL

決定:

- ユーザー編集形式は JSON 変換可能な LayerDraw 独自 DSL を基本にする
- 内部処理は JSON 互換の正規化データで扱う
- AI には JSON を直接自由に書かせるのではなく、制約された DSL を主に扱わせる
- AI の通常更新は構造操作 payload を優先し、LayerDraw 側で DSL へ正規化する
- 体験としては Mermaid のように、コードまたは AI 指示から図を生成できる方向を目指す
- DSL は `project` / `entity_type` / `relation_type` / `layer` / `entity` / `relation` / `query` / `view` / `reference` の宣言を持つ
- Entity包含はtyped Relationだけで表し、Entityに`parent` / `children` fieldを持たせない
- Viewの階層表示はRelationType projection、typed View shape、ViewData occurrenceとして表し、Master Entityへ表示状態を持たせない
- Go EngineがDSL source treeからNormalizedDocumentを一方向生成して検証する。generated JSONからsourceを復元・修復せず、外部JSON importが必要な場合は明示的なimport / migrationをGo Workbench経由で新しいLDL sourceとして生成する
- normalized schema、Query/ViewData評価、format/hash、diagnostic、semantic operationのLanguage 1詳細規範は[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)に従う

generated schemaはLDL syntax versionと独立したmajor versionを持ち、未知の上位majorを拒否する。major間migrationは明示的な新artifact生成とし、unknown fieldの黙示破棄を行わない。

### 9.3 Query / View / ViewData

決定:

- View はユーザー向け概念として残す
- View は「どの切り口で図を見るか」を表す表示レシピである
- ViewData materializationはユーザーに直接操作させない内部変換として扱う
- Master Graph + ViewRecipeからrenderer非依存のViewDataを生成し、ViewDataからRenderData、またはExportPlanを経由したExportArtifact / Source Manifestを生成する
- View UIはactive View、参照Query、typed shape、RelationType projection override、export recipeを編集対象にする
- Query UIはlayers、EntityType / RelationType、roots、typed predicate、traversal、cycle policyをno-codeで編集する
- Search UIはFTS / Vector / Hybridで起点候補を発見し、確認済みStableAddressをQuery rootへ変換する
- Analysis UIは明示subgraphにPageRank、K-Core、Louvain、SCC、WCCを適用し、派生結果として表示する
- Search scoreやAnalysis resultをView selectionまたはMaster Graphへ暗黙保存しない
- 表示モードは layered-2d と stacked-2.5d を持つ

残る未決:

- 障害影響ビューの探索方向と停止条件
- データリネージビューの表現ルール

### 9.4 編集 UX

決定:

- GUI 中心にする
- draw.io に近い直感的な編集体験を目指す
- ただし自由図形ではなく、プリセット Entity を配置、接続、編集する
- 座標や接続点の細かい調整ではなく、Layer、Entity、Relation、抽象度、グループ、順序を主な編集対象にする
- 2D 無限キャンバスでは選択中 Layer 内の Entity を編集する
- Three.js による 3D 可視化では、縦に立てた Layer 面を奥行き方向に重ね、Entity は 3D オブジェクトではなく Layer 面上のフラットなカードとして表示する
- 2D では同一 Layer 内、3D では Layer 間の Relation 作成を扱う

残る未決:

- 図上の手動操作をどこまで構造データへ反映するか
- 自動レイアウトと手動配置の優先順位
- 手動レイアウト調整を保存する場合のデータモデル
- 3D ビューで許可する編集操作の範囲

### 9.5 インポート / エクスポート

決定:

- `.layerdraw` ファイルの import / export を実装する
- `.layerdraw` は zip パッケージとして扱う
- `.layerdraw` 内の構成は `manifest.json`、`document.ldl`、optional な `schema/`、`layers/`、`views/`、`references/`、生成可能な`document.json`と`layerdraw.index.json`、`layerdraw.resolved.json`、`pack/`、`state/`、`assets/`、`previews/`、`exports/` とする
- `document.ldl`をentry module、project-local LDL source treeを正本definition、`document.json`を正規化生成物として扱う
- サーバーは保存済み package がない場合も現在 DSL から `.layerdraw` package を生成できる
- 外部形式連携は将来対応候補にする
- SVG / PNG エクスポートは図の成果物として扱う

残る未決:

- `.layerdraw` 内の生成済み画像を長期成果物として扱うか、再生成可能なキャッシュとして扱うか
- CSV インポート
- クラウド構成情報からの自動生成
- draw.io / Mermaid へのエクスポート
- PDF 出力

### 9.6 ストレージ / デプロイ

決定:

- ストレージはデプロイ先に依存するため、アプリケーション本体から抽象化する
- `.layerdraw` ファイル本体、プレビュー、画像アセットはオブジェクトストレージ向きのデータとして扱う
- プロジェクト一覧、ユーザー、権限、更新履歴、検索用メタデータは RDB / SQL 向きのデータとして扱う
- Blob storage は S3-compatible storage adapter として設計し、Cloudflare R2、MinIO、AWS S3 などへ差し替えられるようにする
- Metadata storage は SQL repository として設計し、Cloudflare D1、SQLite、PostgreSQL などへ差し替えられるようにする
- Realtime coordination は最もプラットフォーム依存が強いため、RealtimeRoom adapter として分離する
- local / self-hosted development は Go + Echo + file store を基準にする
- R2 / MinIO は S3-compatible object storage、D1 / SQLite / PostgreSQL は SQL repository として将来 adapter 化する

候補:

- Cloudflare-first: Pages + Workers + Durable Objects + R2 + D1
- self-hosted-first: React + Go / Echo + MinIO + PostgreSQL

残る未決:

- 主ターゲットを Cloudflare-first にするか
- 各 server deployment で command log、CRDT、OT のどの `RealtimeRoom` adapter を採用するか
- D1 / SQLite / PostgreSQL 間の SQL 方言差分をどこまで repository 層で吸収するか

### 9.7 認証 / テナント / 共同編集

決定:

- Local / BYOSの認証、権限、共有は保存先またはホストアプリへ委譲し、LayerDraw Serverは外部IdPからActorを解決した上でOrganization / Workspace / Project accessを管理する
- LayerDraw Serverは複数Organizationを第一級tenant boundaryとして管理し、部署、子会社、顧客ごとの分離にServerの複製を要求しない
- Instance AdminとOrganization Adminを分離する
- OrganizationはMembers、Teams、Service Accounts、Workspace、policy、storage / registry binding、audit scopeを所有する
- Workspaceは1つのOrganizationに所属し、Projectを整理・共同管理する
- Projectは1つのWorkspaceに所属し、document / revision / realtime room / historyの境界になる
- ローカル利用ではサインイン不要にする
- server-backed modeでは外部IdP / OIDC / reverse proxy authを利用でき、解決したActorをOrganization membershipへmapできるようにする
- Local / BYOSでは保存先ACLを正とし、LayerDraw Serverが外部保存先を使う場合は保存先ACLとLayerDraw Access decisionの積集合、Server所有storageではLayerDraw ACLを正とする
- 共同編集では参加者の名前、色、カーソル、選択中オブジェクトを表示する
- 共同編集の presence 情報は保存データではなく一時状態として扱う
- 選択中Entity / Relationはpresenceとして共有し、既定では排他ロックにしない
- 共同編集の durability は operation log と canonical Committed Revision checkpoint で保証し、`.layerdraw` export は Committed Revision から生成する
- realtime working changeとcheckpointはAuthoring Impactを評価し、schema write不可ActorのSchema変更をauthoritative working stateまたはCommitted Revisionとして公開しない
- multi-actor checkpointは各user / AI agentのcontributionとdecisionを保持してcurrent Policyで再評価し、checkpoint実行者やautosave service actorのgrantへ変更を付け替えない
- policy / membership変更時はparticipantとMCP sessionのeffective AuthoringGrantを再取得する

残る未決:

- IdP / token providerごとのAuthoringGrant発行・失効運用
- deployment ごとの `RealtimeRoom` adapter と operation compaction policy

### 9.8 ソース公開 / Self-host

決定:

- product sourceはpublic GitHub repositoryで公開する
- 製品中核がOSI承認ライセンスでないため、LayerDraw全体の正式分類は`source-available product`とし、`Open Source`、`OSS`、`Open Core`を公式分類に使わない
- 製品中核は`LicenseRef-LayerDraw-1.0`、protocol / generated wire等の明示surfaceは`Apache-2.0`とし、path / artifact matrixは`docs/legal/README.md`を規範とする
- Self-hostは無償で利用可能にする
- SDK package、native sidecar、WASM、Self-host artifactの取得と、商用製品への組み込み自体にはLayerDraw利用料金を要求しない
- LayerDrawを内部実装として使い、固有domainのdata、workflow、analysis、AI機能を主価値として提供する有償 / Multi-tenant SaaSを許可する
- ProviderがSchema Definitionを管理し、End Userが定義済みmodel内のinstance / Query / View等を操作するFixed Model Applicationを明示Safe Harborとして許可する
- 閲覧、埋込み、render、download、exportと、それに必要な保存、認証、access control、共有だけを提供するRead-Only Viewing Serviceを明示Safe Harborとして許可する
- LayerDrawの主要機能を汎用LayerDraw代替のhosted / managed serviceとして第三者へ再提供することは禁止する
- Stock Self-hostへのPack、Template、theme、branding、権限設定だけでLayerDrawの汎用機能集合を第三者へ再提供する場合もHosted LayerDrawとして扱う
- Restricted Hosted Offeringかは、server利用、Multi-tenant、有償だけで決めず、End Userへ任意Schema DefinitionまたはLayerDrawの汎用機能集合を直接・間接に提供しているかで判定する
- Schema DefinitionをEntity、Relation、row等のinstance dataへ符号化していても、End Userが実質的に任意の型、field、関係、constraint、semantic ruleを定義できる場合はGeneral-Purpose Schema Authoringとして扱う
- Public Coreは`fixed_semantic_model`等のAuthoring Policyを提供し、Vertical SaaS、AI semantic layer、Marketplace製品がSchema管理者とinstance編集者を分離できるようにする。ただしpreset名やUI非表示だけでなく実際のwrite経路を評価する
- protocol schema、generated wire binding、低レベルintegration surfaceは、互換integrationを阻害しないpermissive license境界を持てるようにする
- Engine、Runtime、Web、Desktop、VSCode、SDK、MCP Appsは同じpublic product monorepoでatomicに変更できるようにする
- managed deployment固有のcontrol plane、GitOps、credential、production policyはprivate repositoryへ分離する
- 公式Registry contentと生成専用distribution repositoryはproduct sourceから分離する
- public release artifactは一度buildして署名し、同じdigestをSelf-host配布とmanaged deploymentへpromoteする
- managed service固有の商用・流通機能はPublic Coreの現行要件に含めず、コア完成後に別途要件化する

Public repository公開前の必須gate:

- root `LICENSE`、`docs/legal/README.md`、Trademark Policy、CLA 1.0を株式会社DENCYUの正式な公開文書として固定する。外部弁護士reviewは法定要件またはOSI要件として固定しない
- CLA 1.0は日本法を準拠法、福岡地方裁判所を法令上の専属管轄を除く第一審の専属的合意管轄とし、Pull Request checkbox、GitHub metadata、required checkで個人Contributorの同意、権利、勤務先承認を記録する。法人自身をContributorとする場合は権限ある代表者の同意記録を別途保持する
- Contributor Privacy Notice、Code of Conduct、Support、security response経路を公開し、private vulnerability reportingを有効にする
- Git履歴、secret scanning、push protection、branch protection、required checksを検証する

Commercial / OEM agreement、SaaS Terms、Marketplace Termsとの用語整合は各offeringの提供開始条件であり、Public repository自体の公開を停止しない。Registry artifact license metadataとthird-party asset reviewはRegistry content公開時に必須とする。

licenseの法的適用範囲は[legal/README.md](legal/README.md)、repository topology、governance、CI、release、配布、SaaS CD、license enforcementは[repository-governance-and-delivery.md](repository-governance-and-delivery.md)を規範とする。

## 10. 用語

### Organization

LayerDraw Server内のtenant、security、membership、policy、storage / registry binding、audit scopeの分離境界。1つのServer Instanceに複数存在できる。

### Workspace

1つのOrganization内でProjectを分類し、TeamやMemberへ管理権限を付与するHost metadata単位。LDL sourceを標準配置へ整理する`workspace organization`操作とは別概念である。

### Entity

業務、アプリケーション、API、DB、VM、Subnet など、図に登場する意味のある構成要素。

### Relation

Entity 同士の意味付き関係。図上の線ではなく、構造上の関係を表す。

### Layer

業務、データ、アプリケーション、インフラ、ネットワークなどの階層または関心領域。

### View

構造マスタをどの切り口で表示するかを定義するユーザー向けの表示レシピ。

例:

- 受注業務を起点に、業務、アプリ、DB、インフラ、ネットワークまで影響範囲を見る
- アプリケーション Layer だけを詳細表示する
- ネットワーク Layer と VM / OS Layer の関係だけを見る
- データの発生源から利用先までをリネージとして見る

### ViewData materialization

Master GraphとViewRecipeからrenderer非依存のtyped ViewDataへ変換する内部処理。

ViewDataはユーザーが直接編集する正本ではない。Viewを選んだ結果としてLayerDraw内部で生成される。

### Renderer

ViewDataからRenderDataを生成し、2D / 2.5Dの図として描画する処理。

## 11. 2.5D 表現

LayerDraw における 2.5D は、見た目を装飾するための疑似 3D ではない。多層構造を奥行き方向として扱うための表現である。

基本軸:

- 横方向: フロー / 時系列 / 依存順
- 縦方向: グループ / ドメイン / システム境界
- 奥行き: Layer

これにより、同一 Layer 内の関係と Layer 横断の関係を同時に扱えるようにする。

## 12. 最終ゴール

LayerDraw の最終ゴールは、1 つの LayerDraw マスタから、必要なタイミングで複数の図を自動生成できる状態を作ることである。

生成したい図:

- 提案資料用の構成図
- 設計書用の詳細図
- 障害影響範囲図
- データリネージ図
- ネットワーク経路図
- 業務フロー連携図

このために、LayerDraw は個別の図面ファイルではなく、アーキテクチャ構造そのものを管理対象にする。
