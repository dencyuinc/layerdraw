# Portable Engine conformance

このディレクトリは、同一のLDL入力が実行形態によって別の意味へ解釈されないことを保証する適合試験の正本である。

## 正本

- `testdata/engine_compile_parity_v1.json`: Go Engineが生成する入力、期待response、期待blob bytesの共通コーパス
- `testdata/portable_compile_matrix_v1.json`: コーパスを実行する全経路と、異常系分類を担保する実行可能テストの索引
- `testdata/viewdata_conformance_v1.json`: 全ViewData形状、RelationType投影、Query/Diff、State方針と閉じた失敗分類の共通コーパス
- `testdata/viewdata_conformance_matrix_v1.json`: ViewDataコーパスを実行するin-process、stdio、WASM、TypeScript client経路の索引
- `testdata/render_pipeline_conformance_v1.json`: 同じcomplete ViewDataをRender、全visual adapter、Viewer、Node/browser serializerへ通したRender Pipelineの決定的digest、Source Manifest、bounded failure結果
- `render_pipeline_runner.mjs`: Nodeと実browserが共有するpresentation/export conformance runner。semantic projectionやformatを追加せず、既存の公開package contractだけを組み合わせる
- `testdata/workbench_portable_editing_v1.json`: Working Document lifecycle、source patch preview、apply、stale rejection、closeを固定するWorkbench編集コーパス
- `testdata/engine_wasm_worker_v1.json`: WASM Worker transportの閉じたwire grammarとfailure vocabulary
- `testdata/local_runtime_persistence_v1.json`: Local Runtimeのproject/container lifecycle、state、asset、history、idempotency、再起動、transaction phase、filesystem fault、stdio process crashを束ねる共通コーパスと障害行列
- `stdio/v1/`: stdio framingの規範fixture

共通コーパスはsingle/multi-module Project、installed/root Pack、asset、全宣言family、決定論的rejection、resource limit、128 nodeの代表的大規模graph、cancellationを含む。`tools/wasmparity`がin-process Go oracleから生成し、生成差分があればCIを失敗させる。

ViewDataコーパスはdiagram、table、matrix、tree、flow、context、diffの全形状、全RelationType投影モード、Query/Diff source、Stateのnone/optional/required、source/map順序とlocale非依存入力を含む。成功ケースはViewData全体を比較し、invalid input、limit、cancellation、malformed wireはViewDataを一部も公開しない閉じた分類として比較する。

Workbench編集コーパスはopaqueな`document_handle`、`preview_id`、`engine_release`を正規化し、固定可能なoperation outcome、generation、changed source files、diagnostic code、source bytes、hash presenceを比較する。handleやpreview IDの値そのものをfixture化してはならない。

Local Runtime persistenceコーパスは同じsource、SemanticOperationBatch、asset bytes、terminal statusをin-process Runtime ports、local filesystem lifecycle、`layerdraw-host` stdio、TypeScript local-host clientで共有する。Go直呼びは固定clock/identityでstate hash、revision linkage、container bytes、restart後の結果を比較し、stdio clientはopaqueなhost/session IDを値として固定せず、同じdefinition/graph hash、typed result、external publication、state snapshot、history順序を検証する。障害行列の各entryは少なくとも1つの実行可能テストから読み込まれ、テスト内だけの別名fault tableを正本にしない。

## 比較契約

各経路はresponse semantics、outcome、diagnostics、definition/subject semantic hashes、StableAddress、recipe、classification、blob metadataを完全一致で比較する。規範blobはpublication順とbyte列を比較し、public clientだけはresponse内のBlobRef順にAPI値を返すため`blob_id`で対応付けてbyte列を比較する。

許可する正規化はコーパスの`normalization`に列挙した次の3件だけである。

1. artifactごとにlinked releaseが異なるため、`engine_release`を`$engine_release`へ置換する。
2. hard-cancel transportはEngine payloadをpublishしないため、cancellationを閉じた`cancelled` outcomeへ対応付ける。
3. public clientはtransport publication順ではなくresponse reference順でblobを公開するため、blob bytesを`blob_id`で対応付ける。

上記以外のtransport固有semantic実装、field除外、ordering例外、暗黙fallbackは禁止する。例外を追加する場合はコーパス、matrix、本書、全consumerを同じ変更で更新する。

ViewDataでは追加で、endpoint生成のdocument handleとrevision IDをdocument単位の変数へ、論理response byte数を`$returned_bytes`へ正規化する。これらはopaqueな実行所有値であり、ViewData、diagnostics、SourceRefs、StateRefs、item identityの意味値は一切除外・置換しない。

## 実行経路

`portable_compile_matrix_v1.json`は次の実行経路を閉じた集合として固定する。

- in-process Go oracle
- packaged raw stdio sidecar
- `@layerdraw/engine-client` + packaged stdio
- Node `worker_threads` + packaged Go/WASM
- real browser Worker + packaged Go/WASM
- `@layerdraw/engine-client` + real browser Worker

raw stdioはEngineのcooperative cancellationを別のsession testで保証する。Workerのhard cancellationは応答をpublishせずendpointを破棄し、public clientが`origin=client`かつ`outcome=cancelled`へ正規化してfresh endpointへ交換する。

## 異常系

corrupt、mismatched、stale、truncated、oversized、unsupportedはmatrixに記録した実行可能テストがstable failure/outcome、redaction、terminality、recoveryを検証する。matrixのpathまたはtest名が実装から消えた場合、Go conformance testが失敗する。

## 固定リリースセット

`make release-set-check`はnative engine、`@layerdraw/protocol`、`@layerdraw/engine-wasm`、`@layerdraw/engine-client`を1つのmanifestへ束ね、artifact bytes、protocol schema digest、package identity、runtime dependency closure、SPDX、CycloneDX、third-party noticesを再検証する。native engineは隣接する最終manifest bytesのdigestをhandshake authorityとして返す。

`make release-set-reproducible`は同一source revision、version、build timeから2回生成した全byteが一致することを検証する。
