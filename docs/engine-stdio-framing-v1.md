# Engine stdio framing 1.0 規範仕様

## 1. 規範範囲と実装状態

この文書は、LayerDraw Engine の native stdio transport が用いるバイナリフレーム形式 **LDSP framing 1.0** の公開互換性契約を規定する。対象は固定header、frame kind、field不変条件、上限、canonical blob chunk、byte-level I/O failure、およびgolden fixtureである。

LDSP framing 1.0と **Engine Protocol 1.0** は独立したversion空間である。LDSPの`framing_major=1`、`framing_minor=0`は、control payload内のEngine Protocol versionを表さず、Engine Protocol 1.0を選択または保証しない。Engine Protocolのversion、schema、handshake、operation、outcomeはgenerated JSON envelope側の契約である。

現在は`internal/transport/stdio`にframe codec / validator、canonical chunk planner、connection state machine、bounded admission、#28 handshake facadeと#29 `CompilePlan` facadeへの接続、atomic response-bundle writerを実装し、`cmd/layerdraw-engine stdio`からproduction compositionを起動する。これはEngine Protocol 1.0のnative stdio endpointであり、TypeScript client、Runtime、HTTP、Registry fetch、filesystem inputは実装しない。それらのendpoint外integrationは別Issueが所有する。

## 2. Byte grammar

各frameは次の連結である。

```text
Frame = Header[40] || Name[name_length] || Payload[payload_length]
```

すべての整数はunsigned big-endianである。varint、native-width integer、float、host byte order、padding、alignmentは使用しない。

| offset | width | field | 規範値 |
| ---: | ---: | --- | --- |
| 0 | 4 | `magic` | ASCII `LDSP`、hex `4c 44 53 50` |
| 4 | 1 | `framing_major` | `1` |
| 5 | 1 | `framing_minor` | `0` |
| 6 | 1 | `kind` | 3節のclosed enum |
| 7 | 1 | `flags` | kind固有。未定義bitは禁止 |
| 8 | 8 | `stream_id` | connection-local unsigned 64-bit ID |
| 16 | 4 | `sequence` | bundle内unsigned 32-bit sequence |
| 20 | 4 | `name_length` | 後続Nameのbyte数 |
| 24 | 8 | `payload_length` | 後続Payloadのbyte数 |
| 32 | 8 | `offset` | blob先頭からのbyte offset |

実装はbodyをreadまたはallocateする前に、少なくとも次をoverflow検査する。

- `name_length + payload_length`
- `offset + payload_length`
- sequenceのincrement
- chunkの累積byte数
- hostの`int`へ安全に変換できること

## 3. Closed frame kindとflag

| value | name | direction | purpose |
| ---: | --- | --- | --- |
| `0x01` | `REQUEST_CONTROL` | client → engine | generated JSON request envelope |
| `0x02` | `REQUEST_READY` | engine → client | upload admission credit |
| `0x03` | `BLOB_CHUNK` | 双方向 | named blobのcanonical chunk |
| `0x04` | `BUNDLE_END` | 双方向 | request uploadまたはresponse bundleの終端 |
| `0x05` | `CANCEL` | client → engine | streamのcancel |
| `0x06` | `RESPONSE_CONTROL` | engine → client | generated JSON response envelope |
| `0x07` | `CLOSE` | client → engine | orderly shutdown要求 |
| `0x08` | `STREAM_ERROR` | engine → client | trustworthyなgenerated envelopeがないframing/decode failure |

`BLOB_CHUNK`だけがflag bit `0x01`（`FINAL`）を使用できる。`BLOB_CHUNK`のflagsは`0x00`または`0x01`、その他のkindのflagsは`0x00`でなければならない。未知kind、未知version、reserved flag bitはframing-fatalである。

## 4. Field不変条件

| kind | `stream_id` | `sequence` | Name | Payload | `offset` |
| --- | --- | --- | --- | --- | --- |
| `REQUEST_CONTROL` | nonzero、process内で未使用 | `0` | empty | 1..8,388,608 byte UTF-8 JSON | `0` |
| `REQUEST_READY` | known pending stream | `0` | empty | empty | `0` |
| `BLOB_CHUNK` | known ready requestまたはcurrent response | bundleの次sequence | BlobRef `blob_id`の正確なUTF-8 bytes | 0..1,048,576 bytes | blobの正確な次offset |
| `BUNDLE_END` | known stream | bundleの次sequence | empty | empty | `0` |
| `CANCEL` | 任意のnonzero stream | `0` | empty | empty | `0` |
| `RESPONSE_CONTROL` | known stream | `0` | empty | 1..8,388,608 byte canonical UTF-8 JSON | `0` |
| `CLOSE` | `0` | `0` | empty | empty | `0` |
| `STREAM_ERROR` | offending nonzero stream | `0` | 1..128 byteのstable ASCII error code | empty | `0` |

全kind共通で`name_length <= 4,096`、`payload_length <= 8,388,608`である。control上限8,388,608はgenerated Engine Protocol bindingの`MaxWireJSONBytes`と同一でなければならない。codecは依存境界を保つためcontrolをbounded valid UTF-8 JSONとして検査し、generated codecだけがschema validityおよびcanonical JSONを判定する。endpoint integrationはgenerated codecを通してからcontrol frameを書かなければならない。

direction、known stream、stream再利用、request correlationはsession integrationの状態不変条件であり、context-free frame codecだけでは判定しない。

## 5. Canonical blob bundle

`blob_id`はUnicode scalar stringとして有効なUTF-8でなければならない。比較と同一性は**raw UTF-8 bytesそのもの**を使用し、Unicode normalization、case folding、path解釈を行わない。NULを含むscalar valueはlength prefixにより安全であり、diagnosticへNameを出力しない。

bundle規則は次の通りである。

1. control frameの後、最初のblob chunkの`sequence`は`1`である。
2. blob定義はraw UTF-8 `blob_id` bytesの昇順であり、同一Nameのchunkは連続する。
3. 最初のchunkの`offset`は`0`、後続chunkのoffsetは直前の`offset + payload_length`と一致する。
4. non-final chunkは正確に1,048,576 bytesである。
5. nonzero blobのfinal chunkは残りの1..1,048,576 bytesである。
6. zero-byte blobは`offset=0`、empty payload、`FINAL`を持つframeちょうど1個である。
7. `FINAL`後の同一Name、2回目の`FINAL`、offset reset、Nameの降順または重複定義を禁止する。
8. chunkごとにsequenceを1増加し、`BUNDLE_END`は次のsequenceを持つ。uint32 wrapを禁止する。
9. 一つのbundleに異なる`stream_id`を混在させない。

chunk plannerはblob全体を追加allocateせず、size、first sequence、chunk count、各offset / length、end sequenceをoverflow検査して決定する。

## 6. Reader / writer failure semantics

readerは40-byte headerをexact-readする。headerを1 byteも読まないframe境界のEOFだけをclean EOFとして返す。partial header、Name途中のEOF、Payload途中のEOF、body read errorはtruncated / fatalである。

完全なheader、既知kind/version/flags、絶対上限内のbody lengthを確認できた場合、readerはそのbodyをexactに消費してからkind固有fieldを検査する。したがって、例えば絶対上限内だが1 MiBを超えるchunkやwrong sequenceは次のframe境界を保持したrequest-level failureとして分類できる。session層はoffending streamだけを終了し、trustworthyなgenerated requestがある場合は#28/#29のgenerated outcomeを使い、それがない場合だけ`STREAM_ERROR`と`BUNDLE_END`を書く。

次はframing-fatalであり、以後のreadを禁止する。

- wrong magicまたはunsupported framing version
- unknown kindまたはreserved flag
- partial header / physical body truncation / underlying read error
- `name_length > 4,096`または`payload_length > 8,388,608`
- body length、offset、host allocation sizeのarithmetic overflow
- writerのshort-progress、invalid count、broken pipe、その他write error

fatal後に`LDSP` magicをscanしてresynchronizeしてはならない。任意blob payload内に同じ4 bytesが存在できるため、scanはblob内部を偽headerとして選ぶ可能性がある。

writerはframe全体を検証してから、header、Name、Payloadの順にexact bytesを書く。合法なpartial writeは残りを継続し、zero-progress、invalid byte count、error付きpartial writeをfatalとする。公開`Encoder`はframe単位とbundle単位のconcurrent callerをserializeする。sessionは`RESPONSE_CONTROL`、全output blob、`BUNDLE_END`を単一bundle leaseで書くため、別streamのresponse bytesはinterleaveしない。

## 7. Connection、admission、process lifecycle

connectionは`pre_handshake`、`negotiated`、`draining`、`closed`または`fatal`のいずれかである。最初のsuccessful operationは`engine.handshake`でなければならず、成功時の#28 `NegotiatedContext`をconnectionへ一度だけbindする。handshake rejection後は再試行できる。negotiated後のsecond handshakeはcontextを置換せず、#28のgenerated rejectionになる。handshake前compileは#29のgenerated unnegotiated failureになる。

clientはnonzero `stream_id`をprocess lifetime内で再利用してはならない。再利用はcorrelationを一意にできないためconnection fatalである。`request_id`はnonterminal request間で一意であり、同時重複のlater requestだけを#28 endpoint-owned generated failureにする。terminal後の同じ`request_id`はlogical retryとして許可する。

sessionは次を固定上限として実装する。

- nonterminal compile controlは最大64個
- 保持するcontrol payload総量は最大16 MiB
- upload creditを持つstreamは同時に正確に1個
- uploadまたはdispatch slotは最大4個
- admitted requestのdeclared input予約総量は最大576 MiB
- response output blob保持量はrequestごとに最大512 MiB

eligible requestはcontrol arrivalのFIFO順に`REQUEST_READY`を得る。credit以前のblob、wrong stream、wrong sequence / offsetはそのrequestだけを失敗させる。unreferenced、missing、size/digest/lifetime mismatchはcollectorがbounded bytesをsealした後、#29が同じgenerated failureへ変換し、compilerを呼ばない。input bufferはrequest-ownedであり、cancel、failure、dispatch完了時に解放する。

`CANCEL`はunknown / terminal streamではno-opである。pending、upload、dispatchのrequest contextとplanをcancel / abortし、publication前ならgenerated `cancelled` responseを一度だけ書く。`deadline_at`のvalidationとcontext conversionは#28 endpoint facadeだけが所有し、stdio側は別policyを作らない。

`CLOSE`またはframe境界のstdin EOFはadmissionを停止し、pending / partial uploadをcancelし、sealed / dispatching requestのterminal bundleを完了してexit 0になる。partial header/body、bad magic/version/kind/flag、absolute length overflow、broken stdoutは全requestをcancelし、resyncせずexit 1になる。SIGINTは130、SIGTERMは143、CLI usage errorは2である。stdoutはLDSP bytes専用であり、stderrへ出せるのはdata-independentな固定operation codeだけで、control、source、blob、path、stack、underlying errorを出してはならない。

## 8. Compatibilityとgolden corpus

framing majorの変更は、既存parserがframe境界または必須意味を安全に理解できない変更である。header width / field offsetの変更、kind値の再割当、既存fieldの意味変更、上限を既存peerと非互換にする変更はmajorを上げる。

compatible minorは、framing 1.0 peerが明示的にnegotiatedまたは拒否でき、既存1.0 bytesの意味を変更しない追加に限る。ただしframing 1.0はversion byteを正確に`1,0`へ固定するため、1.0 parserは未知minorを受理しない。reserved kind / flagを1.0のまま割り当ててはならない。

authoritative corpusは[`tests/conformance/stdio/v1/manifest.json`](../tests/conformance/stdio/v1/manifest.json)と同directoryの`.frame` filesである。corpusは全8 kind、big-endian field、NUL / non-ASCII Name、binary payload内の`LDSP`を固定し、各fileのSHA-256をmanifestに記録する。Go testsはfixtureをdecodeし、manifest fieldと照合し、再encodeした全byteの一致を検証する。Issue #32以降のTypeScript clientも同一fixtureを読み、独自の第二正本を作ってはならない。
