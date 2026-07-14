# Engine WASM Worker トランスポート規範

<!-- SPDX-License-Identifier: LicenseRef-LayerDraw-1.0 -->

本書は `@layerdraw/engine-wasm` の専用 module Worker と Go `js/wasm` エンドポイントの規範契約である。キーワード「MUST」「MUST NOT」「SHOULD」は規範要件を表す。公開 `@layerdraw/engine-client`、LDL 意味論、UI、永続化、ネットワーク解決、再試行方針は本契約の範囲外である。

## 権威と責務

Worker は `layerdraw.engine_worker` version `1` の外側文法だけを扱う。Engine Protocol の envelope、handshake、schema digest、capability、コンパイル入力、BlobRef、診断、結果の意味は生成 Go codec と `internal/engine/endpoint` だけが扱わなければならない。TypeScript は制御 bytes を不透明な bytes として転送し、LDL の parse、resolve、既定値補完、結果修復、version 選択、schema digest 判定をしてはならない。

一つの Worker は一つの Go runtime、一つの endpoint instance、一つの `endpoint_generation`、一つの negotiated context を所有する。Go bridge は同期呼び出しだけを提供し、同時に一要求だけを実行する。`engine.compile` は必ず Issue #29 の `PrepareCompile` の後に、同じ plan の owned-byte `Execute` を一度だけ呼ぶ。コンパイラ stage を直接呼んではならない。

## 外側メッセージ文法

全レコードは own data property だけを持つ閉じた plain object でなければならない。未知 field、accessor、独自 prototype、Proxy、typed-array view、`SharedArrayBuffer`、resizable `ArrayBuffer`、同一 buffer identity の複数使用を拒否する。文字列は妥当な UTF-8 相当でなければならず、`endpoint_generation` と `exchange_id` は 1–128 bytes、`blob_id` は 1–256 bytes とする。

Host から Worker へのレコードは次の三種だけである。

```text
Init {
  worker_protocol: "layerdraw.engine_worker",
  worker_protocol_version: 1,
  kind: "init",
  endpoint_generation: string,
  expected_artifact_manifest_digest: "sha256:" + 64 lowercase hex,
  release_manifest_digest: "sha256:" + 64 lowercase hex
}

Request {
  worker_protocol: "layerdraw.engine_worker",
  worker_protocol_version: 1,
  kind: "request",
  endpoint_generation: string,
  exchange_id: string,
  control: fixed ArrayBuffer,
  blobs: [{ blob_id: string, bytes: fixed ArrayBuffer }]
}

Dispose {
  worker_protocol: "layerdraw.engine_worker",
  worker_protocol_version: 1,
  kind: "dispose",
  endpoint_generation: string
}
```

Worker から Host へのレコードは `Ready`、`Accepted`、`Response`、`TransportFailure` だけである。すべての post-init レコードは同じ generation を echo する。`Accepted` は buffer の所有権移転と dispatch 開始だけを示し、意味上の成功を示さない。

```text
Ready { kind, endpoint_generation, artifact_manifest_digest, transport_limits }
Accepted { kind, endpoint_generation, exchange_id }
Response { kind, endpoint_generation, exchange_id, control, blobs[] }
TransportFailure { kind, endpoint_generation?, exchange_id?, failure }
```

`Request.control` は生成 request envelope の UTF-8 bytes、`Response.control` は生成 response envelope の UTF-8 bytes そのものである。外側 `blobs` は `blob_id` と bytes だけを持つ。digest、size、media type、lifetime を外側に複製してはならない。attachment は `blob_id` 昇順、重複なしで送る。`exchange_id` は transport correlation 専用であり、Engine request ID、hash、認可、cache identity に使用してはならない。

Host と Worker は `control` と全 blob buffer を transfer list に含めなければならない。送信成功直後に送信側 buffer が detach されない実装は非準拠である。

## 固定ブラウザ上限

初期 artifact の上限は次のリテラル値である。host が指定できる値はこれらを下げることだけができ、上げてはならない。

| 項目 | 上限 |
| --- | ---: |
| control UTF-8 bytes | 8,388,608 |
| control JSON depth | 128 |
| blob ID bytes | 256 |
| attachment/output buffer 数 | 2,048 |
| input 一 blob | 33,554,432 bytes |
| input 合計 | 67,108,864 bytes |
| output 一 blob | 33,554,432 bytes |
| output 合計 | 67,108,864 bytes |
| control を含む response publication | 75,497,472 bytes |

`Ready.transport_limits` と Go bridge の初期化結果は、次の九つの snake_case key を過不足なく持つ閉じた object でなければならない。

```text
max_control_bytes, max_control_depth, max_blob_id_bytes, max_buffers,
max_input_blob_bytes, max_input_total_bytes, max_output_blob_bytes,
max_output_total_bytes, max_response_publish_bytes
```

`endpoint_generation` と `exchange_id` はそれぞれ 1–128 UTF-8 bytes、`blob_id` は 1–256 UTF-8 bytes で測る。JavaScript code unit 数で代用してはならない。version 1 の machine-readable corpus は `tests/conformance/testdata/engine_wasm_worker_v1.json` であり、artifact に `engine-wasm-worker-v1.json` として同梱する。Go と package Worker はこの同じ file を直接検証し、別 copy を作ってはならない。

生成 authority の `MaxWireJSONBytes` と `MaxWireJSONDepth` が上記 control 値より小さくなった場合は生成値を使う。大きくなっても artifact 契約を明示改訂せず上限を上げてはならない。

handshake が広告する browser compiler policy は Project files 512、Project source 16 MiB、Pack files 1,024、Pack bytes 32 MiB、assets 256、asset bytes 16 MiB、raster 一辺 8,192 pixels、raster 合計 16,777,216 pixels、declarations 250,000 である。これは native 既定値の alias ではない。

予測可能な count/size 超過は copy 前に拒否する。Go `js/wasm` linear memory や browser process 全体について portable な hard maximum または OOM subtype を主張してはならない。

## Go bridge ABI

Go runtime は `globalThis.__layerdrawEngineWasmV1` を一度だけ登録する。package Worker はこの低水準 ABI だけを使用する。

```text
initialize(endpointGeneration, verifiedReleaseManifestDigest)
request(endpointGeneration, controlArrayBuffer, blobIDsArray, blobBuffersArray)
dispose(endpointGeneration)
```

`initialize` 成功値は `{ ok: true, endpoint_generation, protocol_schema_digest, transport_limits }` である。`protocol_schema_digest` は生成 Go authority から返される。TypeScript または build script に schema digest literal を複製してはならない。

`request` 成功値は `{ ok: true, endpoint_generation, control, blob_ids, blobs }` である。`blob_ids` と `blobs` は同じ長さで index 対応する。失敗値は `{ ok: false, failure }` である。ABI 引数は outer grammar を検証済みの built-in string/Array/固定 `ArrayBuffer` でなければならない。

artifact manifest digest は WASM 内へ link してはならない。manifest が WASM hash を含むため自己参照になるからである。Worker は co-distributed manifest の canonical bytes と全 file hash を検証し、`Init.expected_artifact_manifest_digest` と一致した後だけ `initialize` を呼ぶ。release manifest digest は検証済み外部入力として `initialize` から Issue #28 descriptor へ渡す。

endpoint instance ID は Go/WASM runtime の初期化時に `crypto.getRandomValues` を背後に持つ Go `crypto/rand` から新しく mint しなければならない。host message、release manifest、artifact manifest、呼出側引数から受け取ってはならない。identity は一つの runtime/generation の descriptor だけで安定し、生成 handshake result 以外には公開しない。release version と source revision は build link 値、protocol schema digest は生成 authority、release manifest digest は artifact/release manifest を検証済みの Worker composition から渡された pin であり、endpoint の自己申告として信頼してはならない。

## copy と所有権

経路を zero-copy と呼んではならない。規範上の表現は「Worker 境界では transferable、JS/Go 境界では bounded copy」である。

1. Host から Worker は transfer で所有権を移し、structured-clone byte copy を行わない。
2. Go bridge は control ごとに `syscall/js.CopyBytesToGo` をちょうど一度呼ぶ。
3. 有効な各 input attachment は `PrepareCompile` 後、Issue #29 の owned blob source を作る時に `CopyBytesToGo` をちょうど一度呼ぶ。base64、文字列変換、追加 transport clone を行わない。
4. Issue #29 は owned `[]byte` を adopt する。Engine facade 内部の防御 copy は transport copy と数えない。
5. Go bridge は response control と各 output blob につき `ArrayBuffer` を一つ確保し、`CopyBytesToJS` をちょうど一度呼ぶ。
6. Worker から Host は transfer で所有権を移し、送信後に Worker 側参照を破棄する。

入力の JS 参照、Go owned blob lease、出力 staging は success、rejection、failure、cancellation、panic、copy failure、dispose の全経路で解放しなければならない。受信済み buffer を別要求で再利用または暗黙 replay してはならない。

Issue #29 の `BlobSink.Publish` は complete set を一括して受け、成功時に独立 bytes を取得するか、失敗時に BlobRef を一つも公開しない契約である。WASM sink は全 output の count、一 blob、合計を overflow-safe に検査してから staging を commit する。生成 response encode、response 全体上限、generation/lifecycle の最終確認のいずれかが失敗した場合、staging を解放して JavaScript に何も返さない。このため現行 Issue #29 API は atomic capped output を保証でき、transport-neutral endpoint の変更を必要としない。

## handshake と lifecycle

`Ready` は byte endpoint の準備だけを表す。互換性と `engine.compile` capability は生成 `engine.handshake` 成功だけが確立する。compile-before-handshake は Issue #29 が生成 failed response として処理する。

一つの endpoint generation は handshake attempt を一度だけ消費する。最初の attempt が rejected、failed、cancelled の場合は生成 response を一度だけ公開した後 generation を terminal にし、同じ runtime で再試行してはならない。二度目の handshake は既存 negotiated context を先に破棄し、生成 `protocol.handshake.invalid_connection_state` rejection を一度だけ返して generation を terminal にする。いずれも新 Worker、新 endpoint identity、新 generation でだけ再試行できる。

同一 Worker では同時要求を一つに制限する。Go `syscall/js.Func` 実行中は Worker event loop が停止するため、同じ Worker へ cancel message を送っても hard cancellation にならない。caller cancellation/deadline は owner が先に generation と pending exchange を terminal にし、その後 `Worker.terminate()` で runtime 全体を破棄する。古い generation の response は decode、adopt、callback、cache、promise settlement の前に捨てる。

replacement は新 generation、新 endpoint instance ID、空 negotiated context を作り、必ず handshake をやり直す。crash、artifact reload、hard cancellation、dispose は旧 generation を永久に無効化する。誤った generation の request は Go に入る前に `engine.worker.stale_generation` とし、誤った generation の response は host が黙って破棄する。

graceful dispose は最初に terminal state を記録し、pending publication を不可能にし、Go plan を abort し、listener、map、buffer 参照を解放し、最後に Worker を terminate する。dispose は冪等である。

Go session に並行 dispose/cancel が到達できる test composition では、terminal state を先に記録し、active plan の `Abort` を一度呼び、同期 `Execute` path の終了を join してから dispose を完了する。request-local sink の nil return だけを commit point とし、cancel が commit より先に勝った response は publication してはならない。実 browser の CPU-bound call は Worker event loop 自体を塞ぐため、owner は join を待たず generation を terminalize して Worker を hard terminate/replacement する。

## local failure

生成 envelope を信頼して返せない初期化・外側 transport・lifecycle failure だけが次の閉じた local failure を使用する。raw exception、stack、path、URL、address、source bytes、browser OOM 文言を公開してはならない。

| code | phase | retryable |
| --- | --- | ---: |
| `engine.worker.unsupported` | `initialization` | false |
| `engine.worker.initialization_failed` | `initialization` | false |
| `engine.worker.artifact_mismatch` | `initialization` | false |
| `engine.worker.malformed_message` | `request` | false |
| `engine.worker.stale_generation` | `lifecycle` | true |
| `engine.worker.transfer_failed` | `transfer` | false |
| `engine.worker.crashed` | `runtime` | true |
| `engine.worker.terminated_by_caller` | `lifecycle` | false |
| `engine.worker.disposed` | `lifecycle` | false |

生成 request を decode できた後の handshake rejection、unnegotiated compile、BlobRef の missing/duplicate/unreferenced/conflict/size/digest failure、compiler resource/cancellation/invariant、output sink failureは生成 response で返す。local failure に変換してはならない。ただし uncaught bridge panic または runtime trap は内容を redact した `engine.worker.crashed` とし、endpoint を replacement 必須にする。

## artifact と再現性

artifact は Go `1.26.5`、`GOOS=js`、`GOARCH=wasm`、`CGO_ENABLED=0`、`GOWORK=off`、空 `GOEXPERIMENT`、`GOENV=off`、`GOFLAGS=-mod=readonly` で build する。`-trimpath -buildvcs=false` と `-ldflags=-buildid= -s -w` を使用し、release version と 40 桁 lowercase source revision だけを link する。時刻、hostname、absolute path、random identity、endpoint identity、artifact manifest digest を link してはならない。

`wasm_exec.js` は同じ Go `1.26.5` の `GOROOT/lib/wasm/wasm_exec.js` を byte-for-byte copy する。期待 SHA-256 は `0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14` である。digest が変わった場合は build を失敗させ、Go/tool support pair を同時に明示 review しなければならない。minify、transpile、patch、download substitute を禁止する。

artifact-local manifest は canonical JSON で、build flags、source/release、生成 Engine schema digest、固定 transport/compiler limits、`layerdraw-engine.wasm`、`wasm_exec.js`、legal files、artifact SBOM の size/media type/SHA-256 を含む。manifest 自身の digest field は持たない。CycloneDX 1.6 SBOM は runtime Go module と co-distributed Go WASM runtime support を区別し、`LICENSE`、`NOTICE`、`LICENSING.md`、`THIRD_PARTY_NOTICES.txt` を同梱する。

loader は取得直後に各 artifact の fixed `ArrayBuffer` を Worker-owned snapshot へ複製し、その snapshot を hash 検証する。検証後に path/URL を再取得してはならず、`wasm_exec.js` は検証済み snapshot から作った一時 Blob module だけを評価して object URL を直ちに revoke し、WASM は検証済み snapshot そのものから instantiate する。source が検証と実行の間に変化しても未検証 bytes を実行してはならない。

同一 clean revision を二つの隔離 directory に展開して build し、WASM、support JS、manifest、SBOM、legal output を byte-for-byte 比較しなければならない。release build は dirty tree を拒否する。

## browser feature contract

初期 profile は dedicated module Worker、WebAssembly `js/wasm`、固定 transferable `ArrayBuffer`、structured clone、`crypto.getRandomValues`、`performance.now`、`TextEncoder`、`TextDecoder`、`Blob` object URL の create/revoke、same-origin/CORS 対応 fetch または事前検証済み bytes を要求する。`SharedArrayBuffer`、Atomics、WASM threads、cross-origin isolation、SharedWorker、Service Worker、Node production transport は要求も広告もしない。

不足 primitive は `engine.worker.unsupported` とし、remote fallback や TypeScript compiler fallback をしてはならない。Node は同じ browser artifact を検証する test host に限る。
