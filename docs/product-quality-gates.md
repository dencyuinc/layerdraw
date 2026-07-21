# Product Quality Gates

LayerDraw の実装は、機能を「存在するように見せる」だけでは完了扱いにしない。

## 1. 認証

- localStorage だけでユーザーを名乗る実装をログイン機能と呼ばない
- ログイン / ログアウトは、サーバー側で検証される session または外部 IdP の認証結果を必要とする
- 未認証でもローカルProjectのopen / edit / save、`.ldl` / `.layerdraw` import / export、ローカルrender / exportを使える
- サーバープロジェクト、共同編集、履歴、共有、AI / MCP token 発行は authenticated Actor を必要とする
- 認証プロバイダ未設定のビルドでは、ログインフォームを表示せず、サーバー操作を disabled にする

## 2. プロジェクト / 権限

- server-backed Project は必ず1つのOrganizationとWorkspaceに所属する
- 1つのServer Instanceで複数Organizationを管理でき、単一組織構成ではdefault Organization / Workspaceをbootstrapする
- API はrequestごとにInstance / Organization / Workspace / Project scopeとActor membershipを解決する
- Project一覧、検索、履歴、asset、artifact、共有、realtime、audit、handle、cursor、cache keyをOrganizationでscopeする
- cross-organization accessは存在、件数、ID、digest、検索結果を漏らさず拒否する
- Project は owner / editor / viewer の権限判定を持つ
- API は UI の状態を信用せず、request ごとに Actor と Project 権限を検証する
- logout はクライアント表示だけでなく、サーバー session を失効させる
- self-host では OIDC、reverse proxy auth、または better-auth adapter のいずれかを明示的に選ぶ

Current implementation:

- Go server は `AuthProvider` 境界を持つ
- `NewServer` の library default は `DisabledAuthProvider` により、health 以外の Project 操作を拒否する
- `cmd/layerdraw-server` の local default は `dev` cookie auth で、`/auth/dev/login` と `/auth/dev/logout` により署名付き session cookie を発行 / 破棄する
- `LAYERDRAW_AUTH_MODE=header` のときは trusted header から Actor を解決する
- `GET /api/session` で認証プロバイダ設定有無、ログイン済み Actor、ログイン / ログアウト URL を返す
- `LAYERDRAW_LOGIN_URL` / `LAYERDRAW_LOGOUT_URL` を設定した環境では、GUI から外部 IdP / host app のログイン / ログアウトへ遷移できる
- Cookie / session 連携のため API request は credentials を含めるが、CORS は `LAYERDRAW_CORS_ORIGINS` またはローカル開発 origin に限定する
- ローカル開発は `http://localhost:<port>` と `http://127.0.0.1:<port>` の両方で `GET /api/session` が成功することを確認する。片方だけのブラウザ検証を接続確認の完了条件にしない
- Realtime WebSocket の Origin 判定も `LAYERDRAW_CORS_ORIGINS` と同じ許可リストに従う
- Realtime WebSocket は通常 API と同じ Actor / Project 権限を検証する。browser から custom header を付けられない開発検証だけ、`LAYERDRAW_ALLOW_REALTIME_QUERY_AUTH=1` で query fallback を明示的に有効化できる
- Project 作成時に `ownerId` を保存し、Project API / package API / agent API / presence / realtime は owner / editor / viewer を検証する
- Project rename / duplicate / delete は UI の disabled に依存せず、API 側で権限と revision を検証する
- 通常のProject renameはserver metadataとDSL Project display nameだけを更新し、authored Project ID / StableAddressは変えない。Project ID変更は別の`migrate_project_identity`として全address / state移行を要求する
- 共有 UI は Project Hub にあり、owner が editor / viewer grant を更新できる
- Revision history は Project Hub から確認でき、保存、共有変更、realtime update などの operation と revision を追える
- OIDC / better-auth adapter 本体は未実装であり、現時点では trusted header adapter または host app 連携 URL を使う
- Organization / Workspace / membership / hierarchical ACL / tenant isolationはTo-Be要件であり、現行server実装には未実装である

## 3. UI / UX

- 主画面は GUI ワークベンチであり、DSL / JSON を常時表示しない
- 左は Entity プリセット、中央は 2D / 3D キャンバス、右は選択対象のインスペクターに分ける
- 右インスペクターは Entity / Relation / Layer / Canvas を選択状態で出し分ける
- ヘッダーの主要操作はメニューまたは Project Hub に集約し、右側にボタンを無秩序に並べない
- メニューに出す操作は実装済みのものに限り、未実装のダミー項目を製品UIに残さない
- 両サイドバーは開閉でき、狭幅ではキャンバスを押し潰さない
- 文字列は i18n 辞書を通す

## 4. Canvas

- 2D は選択レイヤー内の編集を主とし、Entity の追加、移動、Relation 作成が pointer release で確実に終了する
- Entity 移動、追加、削除、Relation 作成などの編集操作は undo / redo 可能にする
- DnD / pointer 操作は stuck state を残さない
- 共同編集中の他参加者カーソルと選択中 Entity / Relation は、2D / 3D の作業面に名前と色付きアウトラインで表示する
- 3D は Three.js を使い、縦面のレイヤー重なりとして表示する
- 3D の Entity は 3D 立体ではなく、レイヤー上の平面カードとして表示する

## 5. 実装境界

境界ごとのoperation、port、状態遷移、failure、capability、version / release conformanceは[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md)を規範とする。

- `App.tsx` に auth、Project Hub、canvas、3D、inspector、server sync の全責務を詰め込まない
- LDL parser、validator、formatter、StableAddress / hash、semantic operation、QueryResult、ViewData、ExportPlan、container規範はLayerDraw Engineだけに置く
- TypeScriptはgenerated protocol、Engine / Server client、Composer、layout、Render、Viewer、Library UI、React UI、ExportPlanを消費するbrowser / Node serializerに限定し、LDL意味論やExportPlanを再実装しない
- Echo、Wails、React、VSCode handlerはframework shellに限定し、domain semanticsを置かない
- browser LadybugDB adapterはLayerDraw Engineが生成したparameterized planだけを実行し、QueryやViewDataを組み立てない
- SearchDocument、Hybrid fusion、SearchResult、AnalysisResultはLayerDraw Engineだけが生成し、MCP / TS / Ladybug adapterへ意味論を重複実装しない
- AuthoringImpactはLayerDraw Engineだけが生成し、LDL外writeのHostOperationImpactはversioned owner protocolだけが宣言し、Accessが両者のPolicy decisionを所有する。document / asset / package適用はRuntime、Host metadataはServer Applicationがcommit enforcementを所有し、UI、MCP、SDK、handlerへsubject分類やoperation-to-capability mappingを重複実装しない
- RuntimeはAccess適用後のSearchDocumentだけをEmbedding ProviderとSearch Indexへ渡し、post-filterだけで検索権限を実装しない
- サーバー API 呼び出しは auth / project / realtime の client 境界に分ける
- native / Node WASM / browser single-thread WASM / browser multithread WASM / sidecar / server-linked Engineは、Structural Queryに加えてFTS、Vector、Hybrid、PageRank、K-Core、Louvain、SCC、WCCを同じconformance fixtureで検証する
- 公式Query-capable bundleで必須Ladybug primitiveが欠けた場合はreleaseを失敗させ、暗黙fallbackしない

## 6. Verification

- 変更ごとに `pnpm --filter @layerdraw/web typecheck` と `pnpm --filter @layerdraw/web build` を通す
- 画面変更は desktop / mobile の 2D / 3D を目視確認する
- テストを書くときは、auth、project operation、canvas interaction、package import/export を分けて検証する
- server-backed testでは、複数Organization間のmetadata、document、asset、history、realtime、audit隔離を必須integration testにする
- server-backed Search testでは候補生成前Access filter、score / snippet / 件数の非漏洩、stale index / cursor拒否を必須integration testにする
- Authoring Access testではEntityType / RelationType / Layer変更とEntity / Relation / row変更を別capabilityへ分類し、operation、fragment、source patch、import、Registry、restore、reconcile、realtimeの全経路で同じdecisionになることを検証する
- asset、Package transaction、Project設定のHostOperationImpactとDefinition差分のAuthoringImpactを同じevaluation digestへ束縛し、片方だけのgrantでcommitできないことを検証する
- fixed semantic modelではschema write不可ActorとAI agentが許可Type / RelationType / Layer内のinstanceを編集でき、Schema変更、Pack更新、generic source patchによる迂回を拒否する
- preview後のpolicy / membership / delegation変更、mixed-capability batch、権限不足approver、untrusted external headを拒否する
- multi-actor realtime checkpointは各contributionをorigin Actorのcurrent grantで再評価し、checkpoint trigger / autosave service actorによる権限の付け替えを拒否する

## 7. Test Coverage

Coverageはテストの十分性そのものではないが、未検証の実行可能pathを継続的に増やさないための必須gateとする。全体平均だけでpackage別の不足を隠してはならない。

### 7.1 Go

Goはcoverprofileのstatement blockを次の基準で検査する。機械可読の正本は[`tools/coverage-policy.json`](../tools/coverage-policy.json)とし、`make coverage-check`が不足を拒否する。

| Scope | Minimum |
| --- | ---: |
| repository全体 | 85% |
| PR / working treeで変更した実行可能statement | 90% |
| 新規packageの既定値 | 85% |
| Engine / Compiler / Workbench / Runtime / Access / Registry semantics | 95% |
| storage / query / external service adapter | 80% + 共通contract test |
| HTTP / stdio / WebSocket / WASM / Wails transport | 80% + transport integration test |
| `cmd/**` composition root | 80% + packaged binary test |

package別ruleは最も具体的なpath prefixを優先する。分類に該当しない新規packageは85%を自動適用し、thresholdを下げる変更は理由と代替のcontract / integration / E2E gateを同じPRで示す。

Go標準coverageはbranch coverageを独立計測しない。分岐の完全性はtable-driven test、invalid fixture、fuzz test、conformance testで補う。

### 7.2 TypeScript

TypeScript packageはVitest / Istanbul互換coverageで次を強制する。packageが追加された時点で各packageの設定とroot taskへ反映し、一時的な無制約状態を作らない。

| Scope | Statements / lines / functions | Branches |
| --- | ---: | ---: |
| protocol client / Composer / Render / Export / SDK | 85% | 80% |
| Accessまたはoperation / capability変換 | 95% | 90% |
| browser / provider adapter | 80% | 75% |
| React UI / framework shell | 75% | 70% |

React UI / framework shellはcoverage数値だけで完了とせず、対応するuser workflowのE2E testを必須とする。

### 7.3 Exclusions and non-substitutable gates

次は集計から除外できるが、無検証にはしない。

- generated Go / TypeScript binding: schema round-tripとcross-language conformance
- 宣言だけを持つgenerated registry: digest / regeneration drift check
- fixture、test helper、mock: 本番source coverageの分母から除外

次はcoverage thresholdで代替してはならない。

- LDL lexer / parser / formatter: valid / invalid golden fixture、round-trip、fuzz test
- StableAddress / hash / canonicalization: golden vector、実行環境間の決定性test
- Access / AuthoringImpact: deny、stale、mixed capability、迂回経路のtable test
- Runtime commit / realtime: race、failure injection、multi-actor integration test
- `.layerdraw` / `.ldpack` / ZIP: fuzz、ZIP slip、容量・深さ上限test
- protocol / generated binding: Go / TypeScript round-trip、unknown field、version negotiation conformance

Coverage exclusion、`// coverage:ignore`相当、threshold低下を通常の機能PRに混ぜない。必要な場合はquality policy変更として別にreviewする。

## 8. Dependency License and SBOM

- third-party dependencyはruntime / developmentの両方を`make license-check`で検査する
- 検査済みの全依存は`reports/dependency-licenses.json`へ機械可読な統合inventoryとして出力し、policy / lockfile digestから検査入力を追跡できるようにする
- 自動許可は[`tools/license-policy.json`](../tools/license-policy.json)のallowlistへ明示したpermissive licenseだけとし、未知のlicenseを推測して通さない
- Go dependencyはmodule / version / license本文digest、npm metadata不備はpackage / version / license本文digestへreviewを束縛する
- dependency updateでmodule、version、license expression、license本文のいずれかが変わった場合はCIを失敗させ、更新PR内で再確認する
- 配布bundleのNOTICE / SBOMはcompiled binaryまたはpacked artifactのruntime closureから生成し、development dependencyを配布依存として記載しない
- packaged artifact testはLicense、Notice、third-party notice、SBOMの存在とparse可能性を検証する
