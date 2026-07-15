# LayerDraw Language 1 詳細仕様

status: 規範設計仕様

本書は、[ldl-language-specification.md](ldl-language-specification.md)で定義するLayerDraw Language 1の構文・宣言モデルを、実装に依存せず決定的に評価するための詳細意味論を定義する。両文書を合わせてLanguage 1の完全な規範仕様とする。

LayerDraw製品はこの規範意味論をGo Engineだけに実装する。Compiler / Workbenchの実装入出力は[compiler-architecture.md](compiler-architecture.md)、Go / TypeScript / framework間の境界は[architecture.md](architecture.md)に従う。これはLanguage 1の意味を実装言語依存にするものではない。

言語本体仕様は字句、構文、宣言、module、identity、オーサリング面を所有する。本書は宣言の出現回数と既定値、Normalized Model、Query評価、ViewDataの具現化、正準シリアライズ、ハッシュ、診断、セマンティック操作を所有する。Product、UI、RelationType、View、Registry、状態、アーキテクチャ文書は説明資料であり、本契約を再定義してはならない。

両規範文書が矛盾する場合は、対象をより具体的に規定する規則を優先する。明示的な規則はサンプルより優先する。Language 1に実装依存のセマンティック既定値は存在しない。

## 1. 表記と適合条件

本書の**MUST**、**MUST NOT**、**SHOULD**、**SHOULD NOT**、**MAY**は規範要件を表す。

スキーマ表記は次を使う。

- `T?`: 省略可能フィールド。省略と`null`は異なる
- `T[]`: 順序を持つsequence
- `set<T>`: 重複を許さず正準順序でシリアライズする集合
- `map<K,V>`: key重複を許さず正準キー順序でシリアライズするmap
- `A | B`: 閉じたタグ付きunion
- `enum(...)`: 閉じた語彙
- `ref<K>`: 終端kindが`K`であるStableAddress
- `child-ref<O,K>`: 所有者kindが`O`、子kindが`K`であるStableAddress

未知のobjectフィールド、unionタグ、enum値、重複map keyはerrorとする。正規化データでは`null`を使わず、省略可能値はフィールド自体を省略する。

明記がない集合順序は、stringをNFC後のUnicode scalar lexicographic order、StableAddressを言語本体7.3節のStableSymbol order、複合itemをスキーマ記載順のfield tupleで比較する。mapのsemantic iterationも同じkey型順を使うが、JSON object serializationはRFC 8785 key orderを使う。

仕様書上の抽象型名だけに`UpperCamelCase`を使う。シリアライズされるフィールドとdiscriminatorは、言語本体仕様4.6節に従い`lower_snake_case`とする。ホスト言語bindingに現れる`camelCase`はadapter面であり正準wireではない。

適合コンパイラは、同じ実効ソースtree、解決済みdependency tree、Query arguments、およびstate依存評価では同じStateInputRefと対応するStateQuerySnapshotから、同じNormalized Model、QueryResult、ViewData semantic payload、ハッシュ、順序付き診断identityを生成しなければならない。localized diagnostic `message`だけはsemantic payload外である。ExternalStateSummaryはStateQuerySnapshotではなく、明示的なexport attachmentでありportable再実行の代替にならない。

## 2. 閉じた宣言スキーマ

lossless パーサが不完全なWorking Documentを保持できるよう、構造EBNFは汎用statementを受理する。ただしCommitted Revisionに含められるのは、以下の閉じたスキーマへ適合するブロックだけである。

明記がない限り次を適用する。

- singleton フィールド/ブロックは最大1回
- 反復フィールドは0回以上。集合指定がない限りauthored orderを保つ
- 必須フィールドの欠落はerror
- 空ブロックは全memberがoptionalの場合だけ合法
- flagは引数を持たず`true`へ正規化する。省略時は明記した既定値を使う
- 同一フィールドの重複は、値が同じでもerror

### 2.1 ルートと共通フィールド

| 所有者 | 必須 | 省略可能なsingleton | 反復可能 |
| --- | --- | --- | --- |
| ソースファイル | なし | 連続したmodule-doc section 1つ（`//!` lineは1つ以上可） | import、declaration |
| project entry | entry-local `project` 1つ | `reserved`、`moves` | project以外のdeclaration、export |
| pack entry | manifestで定義されたPack root | `reserved`、`moves` | declaration、export |
| 共通declaration body | なし | `description`、`tags`、`annotations` | なし |

`tags`はセマンティック set、`annotations`は`map<string,string>`である。空の`tags`または`annotations`は合法だがformatterが除去する。

`tags`/`annotations`をsourceで省略した場合も、normalized `Common.tags`はempty set、`Common.annotations`はempty mapとして必ずmaterializeする。`description`だけは省略時absentのままとする。

### 2.2 EntityTypeとColumn

EntityTypeは`representation`を必ず1つ持つ。`icon`、`image`、`color`、`columns`、`reserve`はそれぞれ0または1つ、名前付き`unique` 制約は0個以上、共通フィールドは任意である。

`columns` ブロックは一意なIDを持つColumn itemを0個以上含む。Columnはスカラー typeを必ず1つ持ち、modifierは次に限定する。

| 修飾子 | 出現数 | 既定値 / 制約 |
| --- | --- | --- |
| enum options | `enum`では必須1つ、それ以外では禁止 | active valueを1つ以上持つ |
| `reserve_values` | `enum`で0または1つ、それ以外では禁止 | 空なら除去 |
| `required` | フラグ | `false` |
| `default` | 0または1つ | Column 検証を満たす |
| `format` | `string`で0または1つ | 閉じたformat語彙 |
| `min`、`max` | `integer`/`number`で各0または1つ | `min <= max` |
| `min_length`、`max_length` | `string`で各0または1つ | 0以上かつ`min_length <= max_length` |

`unique`はrequired Columnとoptional Columnのどちらも参照できる。制約対象Columnがすべて存在するrowだけが比較へ参加する。optional Columnを参照した`unique`をerrorとする古い文書より本規則を優先する。

`integer` Columnの`min`/`max`はJSON-safe integer、`number` Columnではfinite binary64とする。string lengthはformat適用後・NFC normalized valueのUnicode scalar value数であり、UTF-8 byte数やUTF-16 code unit数ではない。cell/default/argument検証はscalar parse、date/datetimeまたはstring format normalization、enum membership、range/length、required、owner-local uniqueの順で行う。

Entity/Relation rowでは、headerにないColumnはunspecified、header内の`_`はexplicit absentである。unspecified Columnに`default`があればその値をnormalized rowへmaterializeし、なければabsentとする。explicit absentはdefaultを抑止し、optional Columnでだけ有効である。default適用後にrequired Columnがabsentならrow validation errorとする。ownerがrowを1件も持たないこと自体はrequired Column違反ではない。QueryParameterでは省略argumentにdefaultを適用し、その後のrequired欠落をerrorとする。

### 2.3 RelationTypeの既定値

RelationTypeはheaderのセマンティック kind、`from`、`to`、forward `label`を必ず1つずつ持つ。省略可能フィールドは次の既定値を使う。

| フィールド | 省略時の値 |
| --- | --- |
| `allow_self` | `false` |
| `duplicate_policy` | `deny_same_type_between_same_endpoints` |
| `cardinality.to_per_from` | `0..*` |
| `cardinality.from_per_to` | `0..*` |
| `reverse` | 省略 |
| 探索の`default_direction` | `outgoing` |
| traversal参加flag | すべて`false` |
| Relationエクスポートの`include_endpoints` | `true` |
| Relationエクスポートの`include_relation_rows` | `true` |
| Relationエクスポートの`sheet_name` | 省略 |

endpointの`types`または`layers`を省略すると、その次元を制限しない。明示的な空listは常にerrorとする。Language 1は意図的にinstanceを持てないRelationTypeを表現しない。

`deny_same_type_between_same_endpoints`は、同じRelationTypeと同じordered endpointsを持つ2本目を拒否する。`deny_any_between_same_endpoints`はtypeを問わず同じordered endpointsを持つ2本目を拒否する。後者は評価順に依存せず、そのpolicyを持つRelationが1本でも存在するendpoint pairには他のRelationを置けない。

### 2.4 Projectionとrenderの既定値

projection ブロックを省略した場合、composed、Diagram、Table、Contextは次のfallbackを使い、Matrix、Tree、Flowには参加しない。

| プリミティブ | 既定動作 |
| --- | --- |
| composed | `mode edge`、`priority 0`、`conflict diagnostic`、`keep_edge true` |
| diagram | `mode edge`、`source_endpoint from`、`target_endpoint to`、`edge_label forward_label`、`include_relation_type false` |
| table | Relationにrowがあれば`relation_rows`、なければ`relation`。両endpointとtypeを含む |
| matrix | 不参加 |
| tree | 不参加 |
| flow | 不参加 |
| context | labelからforward/reverse factを生成し、Relation rowは含めない |

`projection composed`はoptionalな`priority <signed-integer>`を持てる。値が大きい候補を優先する。Language 1のソース名は`keep_edge`だけであり、古い説明文書の`preserveWhenNested`をLDLや正準wireへ出してはならない。

render 既定値は次とする。

| プリミティブ | 既定値 |
| --- | --- |
| edge | `arrow forward`、`line solid`、`label forward_label`、authored colorなし |
| nested | `frame_label parent`、`frame_style subtle` |
| overlay | `kind badge`、`position top_right`、`max_items 4` |
| badge | iconなし、`label count`、`position top_right` |

`max_items`は1以上のintegerとする。colorはuppercaseの`#RRGGBB`または`#RRGGBBAA`へ正規化する。

### 2.5 Queryの出現数と既定値

| メンバー | 出現数 | 既定値 |
| --- | --- | --- |
| `reserve` | 0または1 | empty |
| `parameters` | 0または1 | empty |
| `select` | 必ず1 | なし |
| `where` | 0または1 | logical true |
| `relation_where` | 0または1 | logical true |
| `traverse` | 0または1 | traversalなし |
| `result` | 0または1 | `[seed_entities, traversed_entities, path_relations]` |
| 共通フィールド | 任意 | absent / empty |

`select`内のselectorは各1回まで。省略はunrestricted、明示的な空listは候補なしを意味する。traversalのRelationType selectorは`select.relation_types`を狭めることだけができる。

### 2.6 Viewの出現数と互換性

Viewはoptionalな`intent`と`reserve`、ソースをちょうど1つ、typed shapeをちょうど1つ、RelationTypeごとに一意なoverride、IDが一意なexport recipe、共通フィールドを持つ。

`diff` category、`diff` ソース、`diff` shapeは必ず同時に使う。それ以外のcategoryは、shape固有検証を満たす任意のnon-diff shapeを使える。categoryはshapeの意味を暗黙変更しない。

同じView内で1つのRelationTypeとprojection primitiveの組を複数回overrideしてはならない。省略フィールドはRelationType、次に2.4節の既定値から継承する。

| shape | 必須 | 既定値 |
| --- | --- | --- |
| Diagram | なし | `layout layered`、`direction left_to_right`、`abstraction normal`、composed false、placementなし |
| Table | なし | `rows entity`、type制限なし、固定Column flag false、named Column/sortなし |
| Matrix | `row_axis`、`column_axis`、`cell`を各1つ | axis label `display_name`、direction `outgoing`、セマンティック `relation_refs`、display `exists` |
| Tree | 空でない`relation_types` | `cycle_policy error`、`shared_child_policy error` |
| Flow | 空でない`relation_types` | `lane_by none`、`cycle_policy include_cycle_ref`、並列保持はfalse |
| Context | なし | `group_by layer`、row表示false、incoming/outgoing true |
| Diff | なし | 全subject kindをinclude、detect-moves false |

Matrixのaxis/cell ブロックは各1回だけ。Tableのnamed Column IDは一意で、sortは固定またはnamed output Columnを参照する。

Diagram placementはEntityごとに最大1件で、normalized arrayとformatterはEntity StableAddress順にする。Tableのnamed columnsとsortだけはauthored orderを保持する。その他shape内setは型付きStableAddress順、flag/fieldは8.1節のcanonical schema orderを使う。

### 2.7 エクスポートオプションの型

| オプション | 型 / 制約 |
| --- | --- |
| `width`、`height` | 正のinteger |
| `scale` | `0`より大きい有限number |
| `background` | `transparent`または正準color |
| `page_size` | `a3`、`a4`、`letter`、`legal`、`ledger` |
| `orientation` | `portrait`、`landscape` |
| `fit` | `none`、`page`、`width` |
| `legend` | フラグ |
| `profile` | Language 1で定義したXLSX profile |
| `lookup_sheets`、`hidden_ids`、`formulas`、`view_data_json` | フラグ |
| `bundle`、`header`、`source_manifest` | フラグ |
| `diagnostics`、`state_summary` | フラグ |
| `interactive`、`embed_assets` | フラグ |
| `exporter_profile` | 小文字ASCII profile ID。normalized時はExporterProfileRef |

ホスト依存のhidden既定値を許可しない。`exporter_profile`省略時はformatごとの`layerdraw/<format>@1`を選び、Language 1 exporter-profile registryからformatとspecification digestを解決したExporterProfileRefへ正規化する。profile IDは`[a-z0-9][a-z0-9._/-]*@[1-9][0-9]*`に一致し、registry欠落、digest不一致、format不一致はexport validation errorとする。Language 1ではbuiltin registryに固定されたprofileだけを適合plain exportに使い、同じIDの意味を差し替えてはならない。SVG/PNGの未指定width/heightはnormalized `auto`、scaleは`1`、backgroundは`transparent`とする。PDF/PPTX/DOCXは`a4`、`portrait`、`fit page`、legend falseとする。CSV/TSVを含む全flagは省略時falseであり、traceable CSV/TSVで必要な`header true`はrecipeに`header`を明示して要求する。

XLSX profile省略時はDiagramが`composed_diagram_workbook`（`composed true`）または`diagram_workbook`、Tableが`type_workbook`、Matrixが`matrix_workbook`、Treeが`tree_workbook`、Flowが`flow_workbook`、Contextが`context_workbook`、Diffが`diff_workbook`へ正規化する。categoryはこの既定値を変更せず、`impact_workbook`や`diagram_inventory_workbook`は明示指定時だけ使う。

profile互換性は、`type_workbook`=Table、`diagram_workbook`/`composed_diagram_workbook`/`diagram_inventory_workbook`=Diagram、`matrix_workbook`=Matrix、`tree_workbook`=Tree、`flow_workbook`=Flow、`diff_workbook`=Diff、`context_workbook`=Contextとする。`impact_workbook`はcategory `impact`かつshapeがDiagram/Table/Matrixの場合だけ有効である。不一致はexport recipe validation errorとする。

## 3. ModuleとPackの閉包

### 3.1 パス正規化

project相対module pathとasset pathはホスト OSを問わず`/`を使う。access checkより前に`.` segmentを除去し、`..`を適用する。ソース origin外へ出る結果はerrorとする。portable project/pack treeではabsolute path、backslash、NUL、空の中間segment、percent decode後のtraversal、symlinkを禁止する。

path比較は全ホストでcase-sensitiveとする。case-insensitive filesystem上ではcompile/package前にcase-fold collisionを検出する。ソース pathはUnicode NFCとする。

### 3.2 バインディング衝突

namespaceは期待されるdeclaration kindごとに分かれる。同一module・同一kindでは次を適用する。

- 同じIDのlocal declarationはerror
- named import aliasとlocal declarationまたは別named importの衝突はerror
- namespace aliasの重複はerror
- qualifierの有無で区別できるため、namespace aliasとlocal declaration IDの一致は許可
- named bindingとnamespace aliasの一致は、全利用箇所がqualifier有無で一意な場合だけ許可
- exported public nameは言語本体仕様どおりkindをまたいで一意

import resolutionをdeclaration orderで決めてはならない。

### 3.3 Pack宣言境界

PackOriginはEntityType、RelationType、Query、View、Referenceと、root reservation、move、import、exportを宣言できる。Project、Layer、Entity、Relation、Entity row、Relation rowを宣言してはならない。project factとLayerはprojectまたは`.layerdraw` Templateに属する。

Pack rootのreservationとmoveも、許可されたdeclaration kindとowner-scoped childに限定する。exportされていなくても、禁止kindを含むPackは無効とする。

Pack Query/Viewはproject-local bindingなしでvalidでなければならない。consumerはproject originへcopyしてproject-local root/Layerをbindできるが、不変なpack declaration自体は変更できない。

Language 1が解釈するminimal Pack manifest schemaは次である。

```text
PackManifest {
  format: "layerdraw-pack"
  format_version: 1
  id: "<publisher>/<pack-name>"
  name: lower_snake_identifier
  version: exact_semver
  language: 1
  entry: normalized pack-relative .ldl path
  dependencies: map<dependency-local-name,PackDependency>
}

PackDependency {
  id: "<publisher>/<pack-name>"
  version: exact_semver
}
```

Dependency range、tag、latest、Registry URL、credentialはmanifest dependency valueに置かない。updateはmanifest exact version、expanded dependency tree、resolved metadataを同時に変更する明示操作である。dependency-local nameはPack sourceのnonrelative import先頭segmentで、manifest `name`および他のlocal nameと衝突してはならない。同じcanonical pack IDを異なるlocal nameで重複宣言してはならない。

### 3.4 解決済みメタデータの最小スキーマ

`layerdraw.resolved.json`のセマンティック minimumは次とする。

```text
ResolvedDependencies {
  format: "layerdraw-resolved"
  format_version: 1
  language: 1
  root_pack_id?: "<publisher>/<pack-name>"
  installs: map<install_name, ResolvedPack>
}

ResolvedPack {
  canonical_id: "<publisher>/<pack-name>"
  version: string
  digest: "sha256:<lower-hex>"
  path: normalized project-relative path
  entry: normalized pack-relative path
  files: map<normalized pack-relative path, "sha256:<lower-hex>">
  dependencies: map<dependency-local-name, install_name>
  registry_source: string
}
```

Registry toolingは未知フィールドを保持してよいが、言語意味論へ含めない。`root_pack_id`はPack compileのときだけ必須で、CompileInputがどのcanonical Packをrootとしてcompileするかを選ぶmetadataである。Project compileでは存在しないか空でなければならず、Pack semantic identity、StableAddress、definition hashへ含めない。`layerdraw.resolved.json`全体のintegrity digestと、言語の`resolved`セマンティック ハッシュを区別する。後者のPack payloadは`canonical_id`、exact `version`、artifact `digest`、`entry`、`files`と、dependency-local nameから対象Packの`canonical_id`・exact `version`・artifact `digest`へのmapだけを持つ。Project install name、installed `path`、`registry_source`、mirror URL、credentialはセマンティック ハッシュへ含めない。Pack payloadはPackOrigin StableAddress順、dependencyはlocal name順、fileはnormalized path順でハッシュする。

全install nameとdependency-local nameは正準identifierで、各`dependencies` targetは`installs`の既存keyを指さなければならない。manifestとresolved metadataのversionはSemVer 2.0.0 grammarに一致するcanonical stringで、leading `v`やrangeをresolved exact versionへ使うことを禁止する。prerelease/build metadataはgrammarどおり許可し、exact identityでは文字列全体を比較する。resolved dependency graphは非循環でなければならない。同じcanonical pack IDを複数install nameでaliasしてよいが、exact version、artifact digest、entry、file digest mapが完全一致しなければresolution errorとし、semantic closureでは1つのPackOriginへdeduplicateする。異なるcanonical pack IDが同じinstalled pathを共有すること、1つのPack内でmanifest `name`とdependency-local nameが衝突することを禁止する。

非relative Pack specifierの最初のsegmentは、current originがProjectOriginなら`ResolvedDependencies.installs`のinstall name、PackOriginならcurrent `ResolvedPack`自身のmanifest `name`または`dependencies`のdependency-local nameとして解決する。dependency-local nameは対応するinstall nameへ1回だけ写像し、その先は対象PackOrigin内でmodule pathを解決する。consumer Projectの別名、filesystem上の隣接directory、Registry検索へfallbackしてはならない。同じPack sourceとresolved metadataからはhostを問わず同じPackOriginを得なければならない。

### 3.5 `.layerdraw` container manifest

portable `.layerdraw`の`manifest.json` minimumは次とする。

```text
LayerdrawManifest {
  format: "layerdraw-document"
  format_version: 1
  language: 1
  entry: normalized project-relative .ldl path
  project_address: ref<project>
  definition_hash: sha256
  resolved_file_digest: "sha256:<lower-hex>"
  files: map<normalized container-relative path,"sha256:<lower-hex>">
  redaction?: PackageRedaction
}

PackageRedaction {
  policy_id: non-empty string
}
```

`files`は`manifest.json`自身を除く全ZIP file entryをpath順で列挙し、raw bytesのSHA-256を持つ。directory entryは列挙しない。`entry`と`layerdraw.resolved.json`は必ずfilesに存在し、前者から得たProject address/definition hash、後者のraw digestがmanifestと一致しなければならない。`document.json`、index、state、preview、exportなどderived/optional entryも存在するならfilesへ含めるが、definitionの正本にはならない。`document.json`が存在する場合はsource treeから再生成したNormalizedDocumentとbyte-equivalentでなければstale artifactとして拒否する。index/state/preview/exportは各schemaのdigest bindingが一致しない場合に無視または再生成し、sourceを修復する根拠にしてはならない。

`redaction`はpackage export時に同梱stateからfieldを意図的に除去した場合、または同梱StateQuerySnapshotがAccess判定により`inaccessible_field_paths` / `redacted_field_paths`を持つ場合に必須で、`policy_id`は秘密値を含まないNFC stringとする。完全な許可済みstateだけを含み追加redactionをしていないpackageでは`redaction`を禁止する。これはplain exportのSource Manifest fieldではなく、`.layerdraw` container全体の完全性属性である。

Host Document ID、actor、credential、storage binding、current session、absolute pathをmanifestへ入れてはならない。ZIP pathは3.1節に従い、duplicate entry、case-fold collision、encrypted entry、symlink、device fileを禁止する。container entry順やcompressionはsemantic equality外だが、manifestとraw file bytesが同じなら同じportable definitionとして扱う。

`manifest.json`、Pack `manifest.json`、`layerdraw.resolved.json`、generated `document.json`、`layerdraw.index.json`はRFC 8785 JSONをUTF-8で出し末尾LFを1つ付ける。readerはJSON object key orderやinsignificant whitespaceに依存してはならないが、writerとpackage artifactはこのcanonical formを使う。

## 4. スカラー形式

formatはstring値を検証し、明記したものだけ正規化する。ソース stringは明示format operationまで変更せず、Normalized Modelにはnormalized valueを入れる。

| 形式 | 検証とnormalization |
| --- | --- |
| `uri` | RFC 3986のabsolute URI。scheme必須。syntaxを検証しNFC ソース spellingを保持 |
| `email` | dot-atom local partとDNS hostname domainからなるRFC 5322 `addr-spec`。comment、quoted local part、address list、obsolete syntaxは禁止。spellingを保持 |
| `hostname` | RFC 1034/1035とRFC 1123のASCII label。各label 1-63 octets、final dotを除き最大253。lowercase化してfinal dotを除去 |
| `ipv4` | 4つのdecimal octet `0..255`。`0`以外のleading zero禁止。canonical dotted decimalへ変換 |
| `ipv6` | RFC 4291 input。RFC 5952のlowercase compressed textへ変換 |
| `cidr` | IPv4/IPv6とprefix length。ホスト bitは0でなければならない。canonical addressとdecimal prefixへ変換 |

Language 1の`hostname`はIDNA U-labelを受理しない。ASCII A-labelまたはformatなしstringを使う。format normalizationはtyped equality、unique、Query predicate、normalized JSON、セマンティック ハッシュへ反映する。

## 5. 正規化モデル契約

### 5.1 エンベロープと不変条件

正準normalized outputはProject documentとPack artifactの閉じたunionである。通常の`.ldl`/`.layerdraw` Projectは次とする。

```text
NormalizedDocument {
  format: "layerdraw-normalized"
  schema_version: 1
  language: 1
  project: Project
  dependencies: ResolvedPackSummary[]
  entity_types: EntityType[]
  relation_types: RelationType[]
  layers: Layer[]
  entities: Entity[]
  relations: Relation[]
  queries: Query[]
  views: View[]
  references: Reference[]
  assets: AssetBlobSummary[]
  identity: IdentityHistory
}
```

Registry publishまたはPack単体validationでは次を使う。

```text
NormalizedPackArtifact {
  format: "layerdraw-normalized-pack"
  schema_version: 1
  language: 1
  pack: PackRoot
  dependencies: ResolvedPackSummary[]
  entity_types: EntityType[]
  relation_types: RelationType[]
  queries: Query[]
  views: View[]
  references: Reference[]
  assets: AssetBlobSummary[]
  identity: IdentityHistory
}

PackRoot {
  address: ref<pack>
  canonical_id: "<publisher>/<pack-name>"
}

ResolvedPackSummary {
  address: ref<pack>
  canonical_id: "<publisher>/<pack-name>"
  version: string
  digest: "sha256:<lower-hex>"
}
```

`NormalizedPackArtifact`はProject、Layer、Entity、Relationおよびそれらのrowを持たない。Pack manifestの`name`、version、Registry source、signature、installed pathはresolution/publish metadataでありPackRoot semanticsへ入れない。canonical pack IDとPackOrigin addressは一致しなければならない。Pack Query/Viewは静的にnormalize/validateするが、QueryResult/ViewDataはconsumer ProjectへimportされMaster Graph inputが存在するときだけ生成する。

この閉じたunionがLanguage 1の意味上の正本であり、Go materializerがその唯一の意味変換・byte生成authorityである。Engine Protocol上のnormalized publicationはこのtreeを埋め込まず、role固有`BlobRef`が指すimmutable opaque bytesとして公開する。`@layerdraw/protocol/semantic`は現在`NormalizedDocument`または`NormalizedPackArtifact`のgenerated型を公開しない。canonical/public media type、RFC 8785 UTF-8、末尾LF、raw digest/size/lifetime、discriminator/root一致、互換性の規範は[schemas/README.mdのNormalized blob payload contract](../schemas/README.md#normalized-blob-payload-contract)に従う。protocol clientはcontrol envelopeだけをdecode/re-encodeし、normalized bodyをparse、default、再構築、再encodeしてはならない。

両envelopeのtop-level subject arrayはstructured StableSymbol orderでsortする。child arrayはchild StableAddress順とするが、Columnなどpresentation-significant sequenceはauthored orderを保つ。セマンティック referenceはすべてStableAddressへ解決する。module path、alias、ソース range、comment、pack install nameは除外しセマンティック indexへ置く。

各subjectは`id`、`address`とdeclaration固有フィールドを持つ。project-local addressをscopeするHost Document IDはこのenvelopeへ入れない。

`ResolvedPackSummary`は`address`、`canonical_id`、exact `version`、`digest`を持ち、PackOrigin StableAddress順にsortする。`address`は空pathのPackOrigin rootで、Diffの`pack` subjectになる。`dependencies` arrayにはeffective closureで1件以上のdeclarationが選択されたPackとそのtransitive Pack dependenciesだけを含め、未使用importやinstalled-only Packを含めない。version、artifact digest、Registryソース、install name、local pathはexact resolution/integrity metadataであり、normalized envelopeには記録するがdeclaration semantic equalityと`definition` hashから除外する。exact treeは別の`resolved` hashとCommitted Revision/container manifestが束縛する。

### 5.2 共通正規化値

```text
ScalarType = enum(string,integer,number,boolean,enum,date,datetime)
Scalar = string | integer | number | boolean
Color = canonical #RRGGBB | canonical #RRGGBBAA
NormalizedValue = Scalar | StableAddress | NormalizedValue[] | map<string,NormalizedValue>

AssetRef { digest:"sha256:<lower-hex>", media_type:enum(image/png,image/jpeg,image/webp,image/svg+xml) }
AssetBlobSummary { digest:"sha256:<lower-hex>", media_type:enum(image/png,image/jpeg,image/webp,image/svg+xml), byte_length:non-negative-integer }

Common {
  description?: string
  tags: set<string>
  annotations: map<string,string>
}

Column {
  id: string
  address: child-ref<EntityType|RelationType,column>
  display_name: string
  value_type: enum(string,integer,number,boolean,enum,date,datetime)
  enum_values?: string[]
  reserved_enum_values: set<string>
  required: boolean
  default?: Scalar
  format?: enum(uri,email,hostname,ipv4,ipv6,cidr)
  min?: number
  max?: number
  min_length?: integer
  max_length?: integer
}

UniqueConstraint {
  id: string
  address: child-ref<EntityType|RelationType,constraint>
  column_addresses: ref<column>[]
}

AttributeRow {
  id: string
  address: child-ref<Entity|Relation,row>
  values: map<ref<column>,Scalar>
}
```

`enum_values`はauthored option order、reserved enum値はcanonical sortを使う。rowのabsent cellは`values`から省略する。

Relation nested objectは次とする。

```text
EndpointRule {
  role: string
  entity_type_addresses?: set<ref<entity-type>>
  layer_addresses?: set<ref<layer>>
}

CardinalityBound { min: 0|1, max: 1|"many" }
Cardinality { to_per_from: CardinalityBound, from_per_to: CardinalityBound }

TraversalPolicy {
  default_direction: enum(outgoing,incoming,both)
  participates_in_impact: boolean
  participates_in_flow: boolean
  participates_in_hierarchy: boolean
  participates_in_dependency_matrix: boolean
}

ComposedProjection {
  mode: enum(edge,nest,overlay,badge,hide)
  priority: integer
  conflict: enum(keep_edge,prefer_first,diagnostic)
  keep_edge: boolean
  parent_endpoint?: enum(from,to)
  child_endpoint?: enum(from,to)
  overlay_endpoint?: enum(from,to)
  target_endpoint?: enum(from,to)
  badge_endpoint?: enum(from,to)
}

DiagramProjection {
  mode: enum(edge,hide)
  source_endpoint: enum(from,to)
  target_endpoint: enum(from,to)
  edge_label: enum(type,display_name,forward_label,reverse_label,none)
  include_relation_type: boolean
}

TableProjection {
  row_mode: enum(relation,relation_rows,automatic)
  include_from: boolean
  include_to: boolean
  include_relation_type: boolean
}

MatrixProjection {
  row_endpoint: enum(from,to)
  column_endpoint: enum(from,to)
  include_relation_rows: boolean
}

TreeProjection {
  parent_endpoint: enum(from,to)
  child_endpoint: enum(from,to)
}

FlowProjection {
  source_endpoint: enum(from,to)
  target_endpoint: enum(from,to)
  connector_kind: enum(sequence,control,data,message,error)
  branch_value_column_address?: ref<column>
}

ContextProjection {
  fact_template: string
  reverse_fact_template?: string
  include_attribute_rows: boolean
}

Representation = { kind:"container" } | { kind:"table" } | { kind:"shape", shape:enum(rect,rounded,ellipse,diamond,cylinder,cloud,hexagon,person,device) }

ProjectionSet {
  composed: ComposedProjection
  diagram: DiagramProjection
  table: TableProjection
  matrix?: MatrixProjection
  tree?: TreeProjection
  flow?: FlowProjection
  context: ContextProjection
}

RenderSet {
  edge: { arrow:enum(forward,backward,both,none), line:enum(solid,dashed,dotted), color?:Color, label:enum(type,display_name,forward_label,reverse_label,none) }
  nested: { frame_label:enum(parent,type,display_name,none), frame_style:enum(subtle,strong,none) }
  overlay: { kind:string, position:enum(top_left,top_right,bottom_left,bottom_right,center), max_items:positive-integer }
  badge: { icon?:string, label:enum(type,display_name,count,none), position:enum(top_left,top_right,bottom_left,bottom_right) }
}

RelationExport { include_endpoints:boolean, include_relation_rows:boolean, sheet_name?:string }
```

`automatic`はRelationType定義へ保持する正規化値であり、RelationTypeまたはgraph hashをinstance rowの増減だけで変更しない。ViewData materialization時にRelationごとに評価し、そのRelationにrowが1件以上あれば`relation_rows`、なければ`relation`を選ぶ。選択modeで禁止されるendpointフィールドはabsentにする。render objectはソースで定義したclosed enumと2.4節の既定値をすべてmaterializeする。

### 5.3 Master Graphの対象

```text
Project = Common & {
  id: string
  address: ref<project>
  display_name: string
}

Layer = Common & {
  id: string
  address: ref<layer>
  display_name: string
  order: integer
}

EntityType = Common & {
  id: string
  address: ref<entity-type>
  display_name: string
  icon?: string
  image?: AssetRef
  color?: Color
  representation: Representation
  columns: Column[]
  unique_constraints: UniqueConstraint[]
  reserved_column_ids: set<string>
  reserved_constraint_ids: set<string>
}

Entity = Common & {
  id: string
  address: ref<entity>
  display_name: string
  type_address: ref<entity-type>
  layer_address: ref<layer>
  rows: AttributeRow[]
  reserved_row_ids: set<string>
}

RelationType = Common & {
  id: string
  address: ref<relation-type>
  display_name: string
  semantic_kind: enum(dependency,data_flow,control_flow,deployment,network,security,containment,ownership,sequence,impact,reference,governance)
  allow_self: boolean
  duplicate_policy: enum(allow,deny_same_type_between_same_endpoints,deny_any_between_same_endpoints)
  from: EndpointRule
  to: EndpointRule
  cardinality: Cardinality
  forward_label: string
  reverse_label?: string
  columns: Column[]
  unique_constraints: UniqueConstraint[]
  traversal: TraversalPolicy
  projections: ProjectionSet
  render: RenderSet
  export: RelationExport
  reserved_column_ids: set<string>
  reserved_constraint_ids: set<string>
}

Relation = Common & {
  id: string
  address: ref<relation>
  display_name?: string
  type_address: ref<relation-type>
  from_address: ref<entity>
  to_address: ref<entity>
  rows: AttributeRow[]
  reserved_row_ids: set<string>
}
```

endpointのtype/Layer selectorはunrestrictedならabsent、それ以外はStableAddress setとしてsortする。composed、Diagram、Table、Context projectionはeffective fallbackを含め、Matrix、Tree、Flowはソースで未定義ならabsentにする。

source `image` stringは宣言moduleからorigin-relative pathへ解決し、raw asset bytesのSHA-256とvalidated media typeから`AssetRef`へ置換する。`assets`はeffective documentから参照される一意なdigestごとに1件を持ち、digest順でsortする。同じdigestに異なるmedia typeを宣言することはerrorである。authored relative path、origin-relative packaging path、source rangeはsemantic index/asset manifestにだけ保持し、normalized equalityへ含めない。したがってmodule/path relocationで同じbytesを参照し続ける限りsemantic hashは変わらない。

PNG/JPEG/WebPはsignatureと完全なdecode、SVGはwell-formed XMLと`image/svg+xml`で検証する。SVGのscript、event handler、`foreignObject`、外部URL/reference、animation elementは禁止し、pack contentとして実行しない。Committed Revisionで明示されたimageのmissing、digest不一致、unsupported mediaは`LDL1201`、unsafe/executable contentは`LDL1901`で失敗させる。Working Document previewだけがicon/representation fallbackを表示できる。

### 5.4 Query可能state projection

Backendの完全なstate/audit形式ではなく、Language Query/Viewへ渡せるcurrent state projectionを次に閉じる。Runtimeはbackend headを読み、Access判定とLDL `move_closure`適用後、1回の評価につき最大1件のimmutable `StateQuerySnapshot`を構築する。

```text
StateQuerySnapshot {
  format:"layerdraw-query-state"
  schema_version:1
  definition_project_address:ref<project>
  definition_hash:sha256
  graph_hash:sha256
  state_version:string
  captured_at:datetime
  inaccessible_field_paths:set<StateFieldPath>
  subjects:StateQuerySubject[]
}

StateQuerySubject {
  subject_address:ref<entity|relation|entity-row|relation-row>
  own_subject_hash:sha256
  fields:map<StateFieldPath,Scalar>
  redacted_field_paths:set<StateFieldPath>
}

StateInputRef =
  { kind:"none" }
  | { kind:"snapshot", snapshot_hash:sha256, state_version:string,
      captured_at:datetime, definition_hash:sha256 }

StateReadRef {
  subject_address:ref<entity|relation|entity-row|relation-row>
  field_path:StateFieldPath
}
```

`state_version`はbackendのinteger/string versionをNFC stringへ正規化したopaque値で、数値順序を意味しない。`captured_at`はそのdurable state versionが作成された時にstate headへ保存された不変datetimeで、同じstate versionでは常に同じ値を使う。request開始時刻やQuery predicateの暗黙clockではない。RuntimeはAccess判定を全registry pathへ適用し、actor scope全体で拒否されたpathを`inaccessible_field_paths`へregistry順で置き、その値を全subjectから除く。subjectsはStableAddress順、fieldsとsubject固有redacted fieldはregistry順とする。同じpathを1 subjectの`fields`と`redacted_field_paths`の両方へ置くこと、inaccessible pathをfieldsへ置くこと、型不一致、未知path、重複subject/pathはerrorである。

`subjects`にはactive Master GraphのEntity、Relation、rowのうち、registry pathについてpresent値またはredacted markerを1件以上持つstate recordをすべて含め、許可されたpresent fieldを選択recipeに応じて削らない。全registry fieldが通常absentのrecordは省略し、subject record欠落と同じmissing semanticsにする。型/Layer/View等のQuery非対象record、orphan/tombstone、registry外fieldをsnapshotへ含めてhashを環境依存にしてはならない。StateQuerySnapshotは論理的な完全projectionであり、Runtimeは同じhashとread結果を保つ限りlazy indexとして実装してよい。

Snapshotの`definition_hash`/`graph_hash`はそのdurable state versionが最後にbindされたdefinition/graph hashで、評価対象Committed Revisionと異なり得る。現在評価対象のidentityはQuery/View hashとViewData revisionが別に束縛し、各state recordの利用可否はown-subject hashで判定する。

| StateFieldPath | 型 |
| --- | --- |
| `system.created_at`、`system.updated_at` | `datetime` |
| `system.created_by.kind`、`system.updated_by.kind` | enum `user`, `agent`, `service_account`, `anonymous` |
| `system.created_by.id`、`system.created_by.display_name`、`system.updated_by.id`、`system.updated_by.display_name` | `string` |
| `system.created_revision`、`system.updated_revision` | `string` |
| `provenance.source.kind` | enum `manual`, `import`, `api`, `agent`, `external_system` |
| `provenance.source.label`、`provenance.source.uri`、`provenance.source.external_id` | `string` |
| `provenance.observed_at`、`provenance.verified_at`、`provenance.stale_after` | `datetime` |
| `provenance.verified_by.kind` | enum `user`, `agent`, `service_account`, `anonymous` |
| `provenance.verified_by.id`、`provenance.verified_by.display_name` | `string` |
| `provenance.confidence` | finite number `0..1` |

`system.created_at`/`created_revision`は現在のHost Document state lineageでsubjectが最初に成功commitまたはimportされた時刻/revision、`system.updated_at`/`updated_revision`はそのsubjectのown-subject semantic hashが最後に成功変更された時刻/revisionである。rename/moveは同じstate identityを移行してcreated値を保持し、updated値を変更する。rowは独立subjectであり、row変更をowner Entity/Relationのupdated値へ暗黙伝播しない。child作成/削除もowner own-subject hashを変えない限り同様である。provenanceだけの更新はsystem updated値ではなく`observed_at`/`verified_at`等を更新する。

Stateが存在しないdefinition-only importでは過去created/updated値をfilesystem mtime、Git author、process identityから推測しない。新しいHost Documentへimportしてstateを生成する場合、そのimport成功を新lineageのcreated値としてよいが、source provenanceを`import`として区別する。actorが解決できない場合はactor leafをabsentとする。Query評価時に現在時刻からmissing fieldを補完してはならず、相対時刻Queryはcallerが明示的なdatetime parameterを渡す。

State enumのoption orderは上表に記載した順で固定する。この順序をpredicate型検証、TableColumn enum schema、Table sort、`min`/`max` aggregateに使用する。enumのTable sortで文字列/codepoint順を使ってはならない。

StateQuerySnapshotはactive Master GraphのQuery対象だけを含める。Audit event、revision history、tombstone/orphan archive、field/cell ownership、lock、lease、pending operation、presence、backend locator、credentialは含めない。backendが保持する追加stateはこのprojectionで黙ってQuery可能にならない。

`StateInputRef.kind:snapshot`の各fieldは対応snapshotから再計算して一致しなければならない。`kind:none`は他fieldを禁止する。stateを参照しないQuery/Viewは常に`none`を使い、供給されたsnapshotを結果/hashへ混ぜない。

portable `.layerdraw`がstate依存のmaterialized ViewDataまたはplain export artifactを同梱する場合、参照する各snapshotのcomplete canonical JSONを`state/query-snapshots/<snapshot-hash>.json`へ必ず同梱する。canonical bytesはcomplete StateQuerySnapshotをRFC 8785 JSONとしてUTF-8で出し、末尾LFを1つ付ける。`<snapshot-hash>`は`sha256:`を除いたlowercase hexであり、file内容のJSON valueから`state-query-snapshot` domainで再計算した値と一致しなければならない。LayerdrawManifest `files`はこれとは別にLFを含むraw file digestを束縛する。同じsnapshotを複数artifactが参照しても1 entryへdeduplicateする。

同梱するstate依存ViewDataのSingleRevision `definition_hash`はLayerdrawManifest `definition_hash`と一致し、そのQuery argumentsとView recipeは同梱definitionから解決できなければならない。これによりpackage受領者は同梱definition、arguments、recipe、snapshotからQuery/Viewを再実行できる。条件を満たさないstale artifactはpackage生成時に拒否または再生成する。snapshot entryがないstandalone plain exportは7.9.1節の出自検証だけを保証する。

package redactionでsnapshot fieldを削る場合、削除を`inaccessible_field_paths`またはsubjectの`redacted_field_paths`へ残した新しいcomplete snapshotを作り、新しいsnapshot hashを計算する。旧snapshot hashを参照するViewData、Source Manifest、preview、plain exportをコピーしてはならない。redacted fieldを読まない派生成果物は新snapshotから再materialize・再exportし、読むものは`LDL1904`で生成不能としてpackageから除外する。したがってredacted packageが、削除前snapshotからの完全再実行可能性またはlossless state transferを主張することはない。

結果schemaの`state_policy:none`はinput none、`required`はinput snapshotを必須とする。`optional`だけがnoneまたはsnapshotのどちらも取れる。この不変条件はQueryResult、ViewDataBase、ExportPlan、ExportSourceManifestで共通である。

### 5.5 Query、View、Reference、および識別子

Query/Viewでは全symbolとColumn bindingをStableAddressへ変換し、2節の既定値をmaterializeする。Predicateのソース orderは、truthが可換でもexplanation orderのため保持する。

```text
Query = Common & {
  id: string
  address: ref<query>
  display_name: string
  state_input: enum(none,optional,required)
  parameters: QueryParameter[]
  select: QuerySelect
  where: Predicate
  relation_where: Predicate
  traverse?: QueryTraversal
  result: set<enum(seed_entities,traversed_entities,path_relations,induced_relations)>
  reserved_parameter_ids: set<string>
}

QueryParameter {
  id: string
  address: child-ref<Query,parameter>
  value_type and modifiers: Columnと同じnormalized scalar schema。ただしdisplay_nameなし
}

QuerySelect {
  layer_addresses?: set<ref<layer>>
  entity_type_addresses?: set<ref<entity-type>>
  relation_type_addresses?: set<ref<relation-type>>
  root_addresses?: set<ref<entity>>
}

Predicate =
  { kind:"all"|"any", children:Predicate[] }
  | { kind:"not", child:Predicate }
  | { kind:"field", field:enum(id,address,display_name,description,type,layer,tags,from,to), operator:PredicateOperator, value?:PredicateValue }
  | { kind:"state", field_path:StateFieldPath, operator:PredicateOperator, value?:PredicateValue }
  | { kind:"rows", quantifier:"any"|"all"|"none",
      type_addresses:set<ref<entity-type|relation-type>>, predicate:RowPredicate }

RowPredicate =
  { kind:"all"|"any", children:RowPredicate[] }
  | { kind:"not", child:RowPredicate }
  | { kind:"cell", column_addresses:set<ref<column>>, operator:PredicateOperator, value?:PredicateValue }
  | { kind:"state", field_path:StateFieldPath, operator:PredicateOperator, value?:PredicateValue }

PredicateOperator = enum(eq,ne,lt,lte,gt,gte,in,not_in,contains,starts_with,ends_with,exists,missing)
PredicateValue = { kind:"literal", value:Scalar|StableAddress|Scalar[]|StableAddress[] } | { kind:"parameter", parameter_address:ref<parameter> }

QueryTraversal {
  direction: enum(outgoing,incoming,both)
  min_depth: non-negative integer
  max_depth: non-negative integer
  cycle_policy: enum(error,visit_once,include_cycle_ref)
  relation_type_addresses?: set<ref<relation-type>>
}
```

normalized `result` setはスキーマ記載順にシリアライズする。`where`と`relation_where`は常に存在し、省略ソースは`{kind:"all",children:[]}`になる。

`state_input`はソース省略時`none`へmaterializeする。Predicate/RowPredicateに`kind:"state"`が1件でもあれば`optional`または`required`を必須とし、state predicateがないQueryでは`none`以外を禁止する。state predicateのvalue型は5.4節のfield型と一致しなければならない。

Queryの`contains`、`starts_with`、`ends_with`は型付き値に対する決定論的predicateであり、FTS tokenization、BM25、fuzzy search、Vector similarityを意味しない。FTS / Vector / Hybrid Searchで発見した候補を保存済みQueryへ使う場合、確認済みEntity StableAddressを`root_addresses`へmaterializeするか、明示的な既存predicateへ一般化する。Search text、rank、score、embedding profileをQueryへ暗黙追加してはならない。

ソースoperator `==`、`!=`、`<`、`<=`、`>`、`>=`はそれぞれ`eq`、`ne`、`lt`、`lte`、`gt`、`gte`へ正規化し、word operatorは同名を使う。`exists`と`missing`はvalueを禁止し、それ以外は型互換なvalueを必須とする。`where`のfield集合はEntity用、`relation_where`はRelation用に本体仕様16.3節の表で制限し、contextに存在しないfieldはvalidation errorとする。

```text
View = Common & {
  id: local-identifier
  address: ref<view>
  display_name: string
  state_input: enum(none,optional,required)
  category: enum(topology,inventory,dependency,hierarchy,flow,impact,diff,context)
  intent?: string
  source: QuerySource | DiffSource
  relation_projection_overrides: map<ref<relation-type>,ProjectionOverride>
  shape: ViewShape
  exports: ExportRecipe[]
  reserved_table_column_ids: ordered-set<local-identifier>
  reserved_export_ids: ordered-set<local-identifier>
}

ProjectionOverride {
  composed?: { mode?:enum(edge,nest,overlay,badge,hide), priority?:integer, conflict?:enum(keep_edge,prefer_first,diagnostic), keep_edge?:boolean, parent_endpoint?:enum(from,to), child_endpoint?:enum(from,to), overlay_endpoint?:enum(from,to), target_endpoint?:enum(from,to), badge_endpoint?:enum(from,to) }
  diagram?: { mode?:enum(edge,hide), source_endpoint?:enum(from,to), target_endpoint?:enum(from,to), edge_label?:enum(type,display_name,forward_label,reverse_label,none), include_relation_type?:boolean }
  table?: { row_mode?:enum(relation,relation_rows,automatic), include_from?:boolean, include_to?:boolean, include_relation_type?:boolean }
  matrix?: { row_endpoint?:enum(from,to), column_endpoint?:enum(from,to), include_relation_rows?:boolean }
  tree?: { parent_endpoint?:enum(from,to), child_endpoint?:enum(from,to) }
  flow?: { source_endpoint?:enum(from,to), target_endpoint?:enum(from,to), connector_kind?:enum(sequence,control,data,message,error), branch_value_column_address?:ref<column> }
  context?: { fact_template?:string, reverse_fact_template?:string, include_attribute_rows?:boolean }
  render?: RenderOverride
}

RenderOverride {
  edge?: { arrow?:enum(forward,backward,both,none), line?:enum(solid,dashed,dotted), color?:Color, label?:enum(type,display_name,forward_label,reverse_label,none) }
  nested?: { frame_label?:enum(parent,type,display_name,none), frame_style?:enum(subtle,strong,none) }
  overlay?: { kind?:string, position?:enum(top_left,top_right,bottom_left,bottom_right,center), max_items?:positive-integer }
  badge?: { icon?:string, label?:enum(type,display_name,count,none), position?:enum(top_left,top_right,bottom_left,bottom_right) }
}

QuerySource { kind:"query", query_address:ref<query>, arguments:map<parameter-address,Scalar> }
DiffSource {
  kind:"diff", before:non-empty string, after:non-empty string,
  query_address?:ref<query>, arguments:map<parameter-address,Scalar>
}

ExportRecipe {
  id:local-identifier
  address:child-ref<View,export>
  format:enum(json,yaml,svg,png,pdf,html,csv,tsv,xlsx,markdown,pptx,docx,mermaid,bpmn,drawio)
  filename:string
  fidelity:enum(lossless,traceable_summary,visual_only,lossy)
  source_refs:boolean
  exporter_profile:ExporterProfileRef
  options:ExportOptions
}

ExporterProfileRef {
  id:profile-id
  format:ExportRecipe.format
  registry_schema_version:1
  registry_digest:"sha256:<lower-hex>"
  specification_digest:"sha256:<lower-hex>"
}

ExportOptions =
  { kind:"json"|"yaml", diagnostics:boolean, state_summary:boolean }
  | { kind:"svg"|"png", width:"auto"|positive-integer, height:"auto"|positive-integer, scale:positive-number, background:"transparent"|Color }
  | { kind:"pdf"|"pptx"|"docx", page_size:enum(a3,a4,letter,legal,ledger), orientation:enum(portrait,landscape), fit:enum(none,page,width), legend:boolean }
  | { kind:"html", interactive:boolean, embed_assets:boolean }
  | { kind:"csv"|"tsv", bundle:boolean, header:boolean, source_manifest:boolean }
  | { kind:"xlsx", profile:enum(type_workbook,diagram_workbook,composed_diagram_workbook,matrix_workbook,tree_workbook,impact_workbook,flow_workbook,diff_workbook,context_workbook,diagram_inventory_workbook), lookup_sheets:boolean, hidden_ids:boolean, formulas:boolean, view_data_json:boolean }
  | { kind:"markdown", source_manifest:boolean }
  | { kind:"mermaid"|"bpmn"|"drawio", source_manifest:boolean }
```

QuerySourceとquery付きDiffSourceの`arguments`は既定値適用後のcomplete parameter-address mapとし、required欠落、未知parameter、型不一致をView validation errorとする。DiffSourceで`query_address`がabsentなら`arguments`はempty mapでなければならない。`before`と`after`は同一でないopaque revision selectorで、hostはmaterialization開始前にそれぞれを1つのimmutable Committed Revisionへ固定する。

View `state_input`はソース省略時`none`へmaterializeする。shapeが直接state fieldを参照するのはTableColumnSource `kind:"state"`だけで、1件以上あればView `state_input`を`optional`または`required`にし、直接参照がなければ`none`にする。QuerySourceでは参照QueryとViewからsnapshot load requirementを`required > optional > none`で決める。Query state readはQuery、shape readはView自身のpolicyを使うが、snapshotがある場合は両方へ同じ1件を渡す。DiffSource、DiffShape、またはDiffSource内Queryと、state依存Query/Viewの組み合わせは禁止する。

ViewShapeは次のclosed タグ付きunionとする。transient selection、展開状態、cursor、collaboration 状態を含めない。

```text
DiagramShape {
  kind:"diagram"
  layout:enum(layered,force,grid,radial,manual)
  direction:enum(left_to_right,right_to_left,top_to_bottom,bottom_to_top)
  abstraction:enum(summary,normal,detail)
  composed:boolean
  placements:{entity_address:ref<entity>,x:number,y:number,width:positive-number,height:positive-number}[]
}

TableShape {
  kind:"table"
  row_source:enum(entity,entity_rows,relation,relation_rows,automatic_relations)
  entity_type_addresses?:set<ref<entity-type>>
  include_entity_id:boolean
  include_type:boolean
  include_layer:boolean
  columns:TableColumnRecipe[]
  sorts:TableSort[]
}
TableColumnRecipe {
  id:string, address:child-ref<View,table-column>, label?:string
  source:TableColumnSource
  aggregate:enum(none,count,count_distinct,min,max,join_unique)
}
TableColumnSource =
  { kind:"field", field:enum(id,address,display_name,description,type,layer,tags) }
  | { kind:"attribute", column_addresses:set<ref<column>> }
  | { kind:"relation_endpoint", endpoint:enum(from,to), field:enum(id,address,display_name,type,layer) }
  | { kind:"derived_count", direction:enum(outgoing,incoming,both), relation_type_addresses?:set<ref<relation-type>> }
  | { kind:"state", field_path:StateFieldPath }
TableSort { column_id:string, direction:enum(ascending,descending), absent:enum(first,last) }

MatrixShape {
  kind:"matrix"
  row_axis:{entity_type_addresses?:set<ref<entity-type>>,label_field:enum(id,display_name,type,layer)}
  column_axis:{entity_type_addresses?:set<ref<entity-type>>,label_field:enum(id,display_name,type,layer)}
  cell:{relation_type_addresses?:set<ref<relation-type>>,direction:enum(outgoing,incoming,both),semantic:enum(relation_refs,path_refs),display:enum(exists,count,relation_types,attribute_summary),attribute_column_addresses?:set<ref<column>>}
}

TreeShape { kind:"tree", relation_type_addresses:set<ref<relation-type>>, cycle_policy:enum(error,truncate,duplicate_occurrence), shared_child_policy:enum(error,duplicate_occurrence,link) }
FlowShape { kind:"flow", relation_type_addresses:set<ref<relation-type>>, lane_by:enum(none,layer,entity_type,attribute), lane_column_addresses?:set<ref<column>>, cycle_policy:enum(error,truncate,include_cycle_ref), preserve_parallel:boolean }
ContextShape { kind:"context", group_by:enum(none,layer,entity_type), include_entity_rows:boolean, include_relation_rows:boolean, incoming:boolean, outgoing:boolean }
DiffShape { kind:"diff", include:set<DiffSubjectKind>, detect_moves:boolean }
```

Matrix cellのソース`attributes [<Column>...]`は`attribute_column_addresses`へ解決する。`display:attribute_summary`では互換性のあるRelationType Column setを1つ以上必要とし、その他のdisplayではこのフィールドを禁止する。`lane_by:attribute`は互換性のあるEntityType Column setを1つ以上必要とし、それ以外のmodeでは`lane_column_addresses`を禁止する。

`ProjectionOverride`はRelationTypeごとに1つで、上記フィールド以外を持てない。effective値は2.4節のfallback、RelationType宣言、View overrideの順でフィールド単位に上書きする。View shapeのselection、Tree/Flowのcycle policy、Treeのshared-child policyはprojectionへmergeせずshapeが所有する。merge後にmode固有のendpoint pair、Column owner type、template placeholderを検証し、不足または禁止フィールドがあればView validation errorとする。`render` overrideはRenderData hintだけを変更し、ViewDataのsubject selection、occurrence、Relation、row、source refsを変更してはならない。

```text
Reference {
  id: string
  address: ref<reference>
  text: string
}

TopLevelSubjectKind = enum(entity_type,relation_type,layer,entity,relation,query,view,reference)
OwnerChildSubjectKind = enum(entity_type_column,entity_type_constraint,relation_type_column,relation_type_constraint,entity_row,relation_row,query_parameter,view_table_column,view_export)
DiffSubjectKind = enum(project,pack,entity_type,relation_type,layer,entity,relation,query,view,reference,entity_type_column,entity_type_constraint,relation_type_column,relation_type_constraint,entity_row,relation_row,query_parameter,view_table_column,view_export)
AddressableChildKind = TopLevelSubjectKind | OwnerChildSubjectKind

IdentityHistory {
  root_reservations: map<root-address,map<TopLevelSubjectKind,set<local-id>>>
  moves: Move[]
  move_closure: MoveResolution[]
}

Move {
  kind: enum(project,entity_type,relation_type,layer,entity,relation,query,view,reference,entity_type_column,entity_type_constraint,relation_type_column,relation_type_constraint,entity_row,relation_row,query_parameter,view_table_column,view_export)
  owner_address?: StableAddress
  old_address: StableAddress
  new_address: StableAddress
}

MoveResolution {
  kind: Move.kind
  owner_address?: StableAddress
  source_address: StableAddress
  terminal_address: StableAddress
}
```

`View.id`および`ExportRecipe.id`は、それぞれtyped `address`の末尾IDとbyte-for-byteで一致しなければならない。1つのView内で`exports`の`id`、`address`、および正規化済みcase-sensitive `filename`はそれぞれ一意であり、active export IDは`reserved_export_ids`とdisjointでなければならない。reservation setはASCII local-identifierのbyte-lexicographic昇順でserializeし、Unicode normalizationをvalidation時に実行しない。

`ExportRecipe.format`、`options.kind`、および`exporter_profile.format`は同一でなければならない。`extension`は7.9.1のformat表にあるexact valueで、`filename`はそのextensionと一致するnon-empty basenameでなければならない。

`project`およびtop-level moveは`owner_address`を禁止し、owner-scoped child moveはterminal current ownerのStableAddressを必須とする。old/new addressは`kind`が表す同じsubject kindとoriginを持ち、childではnew addressのowner prefixが`owner_address`と一致しなければならない。owner自体のmoveによるold prefixは言語本体7.5.1節の順で導出する。PackOriginにProject-only kindまたは`project` moveを置くこと、ProjectOriginとPackOriginをまたぐmoveは禁止する。

`root-address`は空のpathを持つProjectOriginまたはPackOriginのStableAddressである。`root_reservations`は選択されたProject rootと、effective documentへ入った各Pack rootを必ず別keyで保持する。owner-scoped child reservationは各ownerの`reserved_*_ids`にだけ保持し、`root_reservations`へ重複させない。

Project rootのtop-level kindは`entity_type`、`relation_type`、`layer`、`entity`、`relation`、`query`、`view`、`reference`、Pack rootは`entity_type`、`relation_type`、`query`、`view`、`reference`に限定する。reservationがないkindもempty setとして保持し、root address、kindの順にserializeする。

`moves`はauthored immediate edgeを保持し、origin、kind、owner、old address順でシリアライズする。chainの各addressが最大1つのimmediate predecessor/successorを持つ制約はこの配列へ適用する。`move_closure`は各historical sourceからterminal currentへのderived mappingを同じ順で持ち、chain `a -> b -> c`では`a -> c`と`b -> c`を含む。closureのterminal fan-inは合法で、immediate-edge制約を再適用しない。Diff/state reconciliationは`move_closure`、history表示とdefinition hashは両配列を使う。Move/MoveResolutionのaddressがoriginを保持するため、複数Packのmoveも衝突しない。

### 5.6 セマンティックインデックス境界

セマンティック indexはgenerated source-navigation artifactでありnormalized セマンティック equalityへ含めない。スキーマ version `1`はSourceOrigin付きソース file digest、declaration/comment byte range、local/import/export binding、StableAddress、ownership、adjacency、dependency、own-subject ハッシュ、subtree ハッシュを含む。ソース rangeは10節と同じSourceOriginと、zero-based UTF-8 byteのhalf-open interval `[start,end)`を使う。

ソース/dependency digestが一致しないindexは拒否して再生成する。stale indexから意味を修復してはならない。

## 6. Query評価意味論

### 6.1 Query実行envelopeとQueryResult

```text
QueryExecutionRequest {
  query_address: ref<query>
  arguments: map<parameter-address,Scalar>
  expected_definition_revision?: integer|non-empty string
  expected_state_snapshot_hash?: sha256
}

QueryExecutionResponse =
  { status:"ok", result:QueryResult }
  | { status:"rejected", diagnostics:Diagnostic[] }

QueryResult {
  query_address: ref<query>
  arguments: map<parameter-address,Scalar>
  state_policy: enum(none,optional,required)
  state_input: StateInputRef
  state_reads: StateReadRef[]
  seed_entity_addresses: ref<entity>[]
  reached_entity_addresses: ref<entity>[]
  traversed_entity_addresses: ref<entity>[]
  path_relation_addresses: ref<relation>[]
  induced_relation_addresses: ref<relation>[]
  primary_entity_addresses: ref<entity>[]
  selected_relation_addresses: ref<relation>[]
  support_entity_addresses: ref<entity>[]
  paths: QueryPath[]
  cycle_refs: QueryCycleRef[]
  diagnostics: Diagnostic[]
}

QueryPath {
  entity_addresses: ref<entity>[]
  relation_addresses: ref<relation>[]
}

QueryCycleRef {
  kind: enum(cycle,merge)
  from_entity_address: ref<entity>
  to_entity_address: ref<entity>
  relation_address: ref<relation>
  orientation: enum(outgoing,incoming)
  retained_path: QueryPath
}
```

Runtimeはrequest受付時にdefinition revisionを1件へ固定し、`expected_definition_revision`を最初に検査する。一致後にQuery/argumentsを解決し、Query policyに従ってstate headを固定・StateQuerySnapshotを構築する。required state欠落/不整合は`LDL1604`で先に失敗させ、その後に`expected_state_snapshot_hash`を検査する。expected snapshot hashは実際に構築したsnapshot hashと完全一致しなければならず、state非依存Query、optional no-state、または異なるsnapshotでは`LDL1801`の不一致とする。これらは任意のhistorical revision/backend/snapshotを選択するselectorではない。

precondition不一致は`status:"rejected"`と`LDL1801`を返し、QueryResultを生成しない。Query/argument validation、required state欠落、Access拒否など評価を失敗させる診断も`rejected`へ置く。`ok`はQueryResultを必須とし、top-level diagnosticsを禁止する。optional no-state/staleの`LDL1605`は成功結果の`QueryResult.diagnostics`へ置く。`rejected.diagnostics`は1件以上で10.2節順とする。

path以外のarrayは重複なしかつ正準順序とする。pathはtraversal orderを保ち、`entity_addresses.length == relation_addresses.length + 1`を満たす。cycle refの`retained_path`はcandidate sourceまでのcanonical pathで、seed自身ならEntity 1件・Relation 0件とする。cycle refは`(kind,from,to,relation,orientation,retained_path entity/relation sequence)`順にsortする。

`state_policy`はnormalized Queryの`state_input`と同じである。policy noneではstate readsをemptyとする。optional/requiredでは`state_reads`をsubject StableAddress、次に5.4節のStateFieldPath registry順でsortし、重複を禁止する。predicateのboolean short-circuitに依存させず、各candidate/rowへ構文上適用される全state predicate readを記録する。`diagnostics`は10.2節順である。

`reached_entity_addresses`はdepth `1..max_depth`で到達した全Entityを保持し、`min_depth`未満の中間Entityも失わない。`traversed_entity_addresses`はそのうち`min_depth..max_depth`に入るEntityだけを保持する。`primary_entity_addresses`はQueryの`result`に含めた`seed_entities`と`traversed_entities`の和、`selected_relation_addresses`は含めた`path_relations`と`induced_relations`の和である。

`paths`はtraversalの有無にかかわらず各seedのtrivial pathと、各reached Entityへ採用した最初のcanonical pathを1件ずつ持つ。`visit_once`または`include_cycle_ref`で非採用となった代替pathは追加せずcycle/merge refに記録する。pathsは終端Entity StableAddress、次にEntity/Relation sequence順にsortする。

`support_entity_addresses`は、selected Relationの全endpointと、selected path Relationを含むretained path上の全Entityから`primary_entity_addresses`を除いた集合である。これにより`min_depth`未満の中間Entityや、Relationだけをresultへ含めた場合のendpointも失わない。support EntityはQueryのprimary resultではないが、ViewDataの参照整合性を閉じるために必須である。

### 6.2 評価パイプライン

1. 供給されたargumentsを検証/正規化する
2. 省略argumentへ宣言された既定値をmaterializeする
3. Queryのstate policyと供給snapshotからStateInputRefを固定・検証する
4. selectorとdefinition/state predicateからEntity/Relation eligibilityを作る
5. seedを決める
6. 宣言されていればtraversalを実行する
7. path Relation setとinduced Relation setを作る
8. `result` inclusionからprimary Entityとselected Relationを作る
9. selected Relation/pathのendpoint closureからsupport Entityを作る
10. result collection、state read、diagnosticをcanonicalizeする

未知/重複 argument、required argument欠落、無効 valueはgraph評価前に失敗させる。

`state_input:none`はsnapshotを受け取らず`StateInputRef {kind:"none"}`を使う。`optional`はsnapshotがあれば検証済みsnapshot ref、無ければ同じnone sentinelを使い、Query ownerに`LDL1605`を1件返す。`required`はsnapshot欠落を`LDL1604`で失敗させる。Runtimeがstate非依存Queryへsnapshotを供給しても適合evaluatorは読まず、hash / resultへ含めてはならない。

### 6.3 適格性

Entityは、指定されたLayer selector、EntityType selector、`where` predicateをすべて満たす場合にeligibleとなる。RelationはRelationType selectorと`relation_where`を満たす場合にeligibleとなる。

eligibilityはseed、traversal ソース/target、path Relation、induced Relationのすべてへ適用する。ineligibleなEntity/Relationを経由して探索してはならない。これにより`environment == prod`のような条件がrootだけでなく選択subgraph全体を制約する。

`select.roots`がある場合、eligibleな指定rootだけをcanonical Entity-address順でseedにする。現在のparameter/rowによりrootがineligibleならseedへ入れず`info` 診断を返す。rootsがなければ全eligible Entityをseedにする。

### 6.4 述語

explanationのためchildはauthored orderで評価するが、truthはboolean semanticsに従う。`all {}`はtrue、`any {}`はfalse、`not`はchildを必ず1つ持つ。

stringの`contains`、`starts_with`、`ends_with`はNFC後のUnicode code pointをcase-sensitiveで比較する。setの`contains`はnormalized exact membershipを検査する。locale foldingやimplicit スカラー conversionは禁止する。

field/cellがabsentの場合、`missing`だけがtrue、`exists`はfalse、その他のoperatorは`ne`と`not_in`を含めすべてfalseである。present値では`exists`がtrue、`missing`がfalseとなる。`eq`/`ne`は同じdeclared typeのnormalized exact equalityとその否定、`lt`/`lte`/`gt`/`gte`は同じnumber、integer、date、datetimeのtotal orderに限定する。date/datetimeはnormalized chronological valueで比較する。`in`/`not_in`はscalarまたはStableAddress左辺と同型list右辺のmembershipとその否定に限定する。set-valued `tags`は`eq`/`ne`によるset equality、または`contains`による1 string membershipだけを許す。integer/number、string/enum、local ID/StableAddressの暗黙変換は禁止する。

State readは次の順で解決する。

1. Access layerがstate field path自体を許可し、snapshotの`inaccessible_field_paths`に無いことを検証する。拒否されたpathはsnapshot有無やpolicyにかかわらず`LDL1904`で失敗させ、missingへ偽装しない
2. `StateInputRef.kind:none`ならfieldをabsentとして扱う。これは`state_input optional`でだけ合法である
3. snapshotにsubject recordが無ければfieldをabsentとして扱う
4. recordの`own_subject_hash`が評価中subjectと一致しなければ、`required`は`LDL1604`で失敗、`optional`はそのrecordの全fieldをabsentとして`LDL1605`をsubjectごとに1件返す
5. pathが`redacted_field_paths`にあれば`LDL1904`で失敗する
6. `fields`にpathが無ければabsent、あればregistry型のpresent値として通常predicateを適用する

snapshotの`definition_project_address`がeffective Projectと異なる場合、またはStateInputRef/hashが再計算と一致しない場合はpolicyにかかわらず無効入力として失敗する。snapshotのdefinition/graph hashがcurrent revisionと異なっても、参照recordのown-subject hashがすべて一致する場合は対象stateを利用できる。これによりReferenceや無関係subjectの変更で全stateをstaleにしない。

correlated row predicateは次を使う。

- `any`: complete child predicateを満たすrowが1つ以上
- `all`: rowが1つ以上あり、全rowが満たす
- `none`: 満たすrowがない
- 1つのrow predicate内の全`cell`は同じrowへbindする
- explanationではrow StableAddress順。truthはrow orderに依存しない

### 6.5 探索

traversalはdepthごとのbreadth-firstとし、seedのdepthを`0`とする。1つのfrontier Entityに対するcandidate Relationは`(RelationType StableAddress, Relation StableAddress, from StableAddress, to StableAddress, orientation)`順とし、orientationは`outgoing`を`incoming`より先にする。

`min_depth`と`max_depth`は0以上のintegerで、`min_depth <= max_depth`を必須とする。traversalを省略したQueryではseedだけを評価し、`reached_entity_addresses`、`traversed_entity_addresses`、`path_relation_addresses`、cycle refはempty、`paths`はseedのtrivial pathだけとする。

- `outgoing`: `from -> to`
- `incoming`: `to <- from`
- `both`: 両candidate listのordered union
- self Relationは`both`でも1回
- parallel Relationは別々に保持
- 有限 rangeはemitted non-seed Entityを制御し、必要なintermediate depthの探索を止めない

multi-source BFSのvisited集合は全seedで初期化し、frontierはseed StableAddress順とする。`visit_once`はEntityへの最初のcanonical shortest pathを採用し、そのEntityを再展開しない。`include_cycle_ref`も同じfirst-path ruleを使い、現在 path内または既訪問Entityへ到達したcandidateをcycle/merge refとして記録する。`error`は現在 path内へ戻るcandidateでevaluationを失敗させる。別のacyclic pathから既訪問Entityへ着く場合はmergeとしfirst canonical pathを使う。

`path_relation_addresses`は、emitされたtraversed Entityへ至るretained path上の全Relationを含む。`min_depth`以上へ到達するためのintermediate Relationも含める。`induced_relation_addresses`は`seed_entity_addresses`と`traversed_entity_addresses`の和に両endpointを持つeligible Relationからpath Relationを除いた集合とし、`result` inclusionに依存せず先に計算する。

### 6.6 結果の包含と順序

primary Entity setには`result`で要求したcategoryだけを含める。`seed_entities`はtraversal minimumに関係なくeligible seedを含む。`traversed_entities`はrange内のnon-seed reached Entityを含む。Relation categoryはEntity categoryと独立して選択し、endpoint欠落はsupport Entityで閉じるため、選択済みRelationを黙って除外しない。

seed/traversed arrayはEntity StableAddress順、Relation arrayはorientationを除いたcandidate tuple順、pathはEntity/Relation StableAddress sequence順とする。paginationはtransport責務でありlogical complete resultを変更しない。

## 7. ViewDataの具現化

### 7.1 共通パイプライン

Query-backed Viewは次の順で処理する。

1. QueryとViewのstate policyからsnapshot load requirementを決め、必要なら1件へ固定する
2. 6節に従ってQueryを評価する
3. 下記の`ViewSelection`を作る
4. RelationTypeとView overrideをresolveする
5. typed ViewData shapeを1つだけmaterializeする
6. definition SourceRefs、StateRefs、診断を付与する
7. セマンティック collectionをcanonical sortする

```text
ViewSelection {
  primary_entity_addresses: set<ref<entity>>
  support_entity_addresses: set<ref<entity>>
  materialization_entity_addresses: set<ref<entity>>
  relation_addresses: set<ref<relation>>
  entity_row_addresses: set<ref<entity-row>>
  relation_row_addresses: set<ref<relation-row>>
  cell_refs: set<{row_address:ref<row>,column_address:ref<column>}>
  state_reads: set<StateReadRef>
  retained_paths: QueryPath[]
}
```

`materialization_entity_addresses`はprimaryとsupportの和、`relation_addresses`はQueryResultのselected Relation、`state_reads`はQueryResultのstate readsである。`retained_paths`はQueryの`result`が`path_relations`を含む場合だけQueryResult.pathsを保持し、それ以外はemptyとする。row集合はmaterialization Entityおよびselected Relationが所有する全row、cell集合はその全present cellとする。View shapeは以下で明示した場合だけsupport Entityをprimaryと同じ表示対象として扱い、それ以外でもendpoint・path・source ref解決のために参照できる。row predicateに一致したrowだけへ暗黙に絞らず、row shapeはowner配下の全rowを決定的に扱う。

snapshot load requirementは`required > optional > none`で決め、その値をViewData `state_policy`へ記録するが、Query state readにはQuery自身、shapeの直接state readにはView自身のpolicyを適用する。Queryがstate非依存ならsnapshotが固定されていてもQueryResultは`StateInputRef.none`を使う。ViewDataはQueryまたはViewのいずれかがstate依存してsnapshotがあれば固定snapshot ref、optionalだけでsnapshotが無ければnone、両方noneでもnoneを持つ。QueryとViewへ異なるsnapshotを渡してはならない。

Diff Viewでは2つの不変revision inputが1、2を置き換える。ViewDataはMaster Graphを変更せず、新しいfactを正本へcopyしない。

各ViewData itemは最小かつ完全なソース refsを持つ。複数ソースのsummaryはordered unionを持つ。derived keyは言語本体仕様のSHA-256 tuple ruleを使い、LDL identityとして扱わない。

### 7.2 Diagram

materialization Entityごとにprovisional root occurrenceを1つ作りEntity StableAddress順に並べる。support Entityのoccurrenceは`role support`、primary Entityはrepresentationに従い`node`または`container`とする。Diagram shapeの`composed`がtrueの場合だけcomposed projection candidateを次の順で適用し、falseなら全selected RelationをDiagram projectionへ送る。

```text
priority descending
RelationType StableAddress ascending
Relation StableAddress ascending
from Entity StableAddress ascending
to Entity StableAddress ascending
```

`nest`はMaster factを動かさず、childごとにcandidateをgroup化して次の閉じた規則で処理する。

1. 全candidateが同じparentを指す場合、最初のcandidateをnest根拠とし、残りは`keep_edge`に従ってedgeまたはsupportへ送る。
2. 異なるparentがあり、1件でも`conflict diagnostic`ならnestせず、全candidateをsupportへ保持して`LDL1704` warningを出す。
3. それ以外で`conflict prefer_first`が1件以上なら、その中の最初のcanonical candidateを採用する。非採用の`conflict keep_edge` candidateは必ずedge、その他は各candidateの`keep_edge`がtrueならedge、falseならsupportへ送る。
4. 異なるparentがあり全candidateが`conflict keep_edge`ならnestせず、全candidateをedgeとして残す。

採用したnestはprovisional root occurrenceを新規複製せず、そのoccurrenceと既存subtreeをparent配下へ移す。したがって1つのEntity occurrenceがrootとchildへ重複しない。

composed modeはrelationごとに次のとおり消費する。`edge`はDiagram queueへ送る。`hide`は`hidden_relation` support itemだけを作る。`nest`は上記規則を適用し、採用relationは`keep_edge true`の場合だけDiagram queueにも送る。`overlay`/`badge`は対応itemだけを作りDiagram queueへは送らない。occurrence ancestry cycleを作るnestは`LDL1702` errorでView materialization全体を失敗させる。nest後にoverlay/badgeをcandidate orderでtarget occurrenceへ付ける。ViewDataは全overlay/badge itemを保持する。renderの`max_items`はRenderDataで可視item数とoverflow表示を決めるpresentation hintであり、ViewData itemを削除・集約したりkeyやSourceRefsを変更してはならない。

Diagram queueのRelationはeffective Diagram projectionを適用し、`mode edge`ならedge、`mode hide`なら`hidden_relation` support itemにする。Diagram shapeの`composed false`では全selected Relationをこのqueueへ直接送る。選択されたRelationのendpoint closureだけで入った非表示Entityは`hidden_entity`、競合や変換不能で視覚要素を生成しないsourceは`source_only`のsupport itemとして保持する。各support itemは原因となったEntity/Relationと完全なSourceRefsを持つ。同じ`(support_kind,entity_address?,relation_address?)`を生む原因が複数ある場合は1itemへ統合し、SourceRefsをordered unionにする。

`place <Entity> ...`はvisible root occurrenceがちょうど1つのEntityだけを対象にできる。0件、nested-only、複数occurrenceは無効とする。Language 1はViewData occurrence keyをplacement正本へ保存しない。

`layout`、`direction`、`abstraction`はViewData構造を削除・追加しないpresentation inputであり、normalized `shape`としてViewDataへ保持する。`abstraction`によるlabel/row表示量の差はRenderData責務で、SourceRefsやsupport itemを失わせてはならない。

### 7.3 Table

primary Entityとselected Relationを対象に、ソースrow sequenceは次とする。

- `entity`: selected Entityごとに1row
- `entity_rows`: selected Entity attribute rowごとに1row
- `relation`: selected Relationごとに1row
- `relation_rows`: selected Relation attribute rowごとに1row
- `automatic_relations`: selected Relationごとにeffective TableProjectionを読み、`relation`は1 Relation 1row、`relation_rows`は各Relation row、`automatic`はrowがあれば各Relation row、なければ1 Relation 1rowとする

`entity_type_addresses`と固定flag `include_entity_id`/`include_type`/`include_layer`は`entity`/`entity_rows`だけで有効であり、Relation系row sourceでは禁止する。`entity_type_addresses`はprimary Entityをさらに絞り、support EntityをTable rowへ追加しない。固定Columnは順に`entity_id`（label `id`、string local ID）、`entity_type`（label `type`、StableAddress string）、`entity_layer`（label `layer`、StableAddress string）を使う。Viewのnamed Column IDはこれらと衝突してはならない。

初期順序はowner/row StableAddress順とする。各declared Columnはtyped cellとソースrefsを生成する。missingソースはabsent cellであり、`null`やempty stringではない。

`source attribute`は`rows entity_rows`または`rows relation_rows`でのみ有効であり、現在rowの対応cellを読む。owner単位の`rows entity`または`rows relation`で二次元属性表を単一値へ暗黙縮約することは禁止する。owner単位では固定field、relation endpoint、derived countだけを使用できる。`source relation_endpoint`はrelation/relation_rowsだけ、`source derived_count`はentity/entity_rowsだけで有効とする。互換性違反はView validation errorである。

`source state`は現在のsource row identityをStateQuerySubjectの`subject_address`として5.4節のfield pathを読む。`entity`/`relation`はowner subject、`entity_rows`/`relation_rows`はrow subject、`automatic_relations`は実際に選ばれたrelationまたはrelation-row subjectを使う。field absentまたはoptional no-state/stale recordはabsent TableCell、present fieldはregistry ScalarTypeのTableCellになる。redacted/Access拒否は`LDL1904`、required snapshot欠落/staleは`LDL1604`でView materializationを失敗させる。optional no-stateはView ownerに`LDL1605`を1件、stale recordはsubjectごとに1件返す。TableCellのStateRefsへsubject addressとfield path、ViewDataBaseへ実効StateInputRefを記録し、aggregate/group rowはcontributing StateRefsをordered unionする。

`source derived_count`は`ViewSelection.relation_addresses`のうち、指定RelationType setとdirectionを満たして現在rowのowner Entityへ接続するRelation本数を数える。RelationType set省略は全selected RelationTypeを意味する。`outgoing`はownerが`from`、`incoming`はownerが`to`、`both`は和集合であり、self Relationは1回だけ数える。parallel Relationは別々に数え、Relation row数は使用しない。`entity_rows`では同じownerの各rowに同じcountを出す。

`automatic_relations`ではeffective TableProjectionの`include_from`、`include_to`、`include_relation_type`から、それぞれStableAddress string型でlabelも同名の固定Column `from`、`to`、`relation_type`をこの順に生成する。選択されたRelationType間でflagが異なる場合もColumnは和集合として1回だけ作り、対象外Relationのcellをabsentにする。Viewのnamed Columnは固定ColumnとID衝突してはならない。`automatic_relations`でRelation row属性を出す場合、全present cellをColumn StableAddress別のtyped dynamic ColumnとしてColumn StableAddress順に追加し、同じlocal IDでも異なるColumn addressを混同しない。dynamic TableColumnの`id`はsource Column StableAddress文字列、`label`はsource Columnの`display_name`、`address`はabsent、`source_column_addresses`はその1件とする。

aggregate Columnがなければソース rowをそのままViewData rowにする。1つ以上あれば、全non-aggregate Columnをdeclaration orderのgroup keyにする。absentは独立したgrouping valueとする。

明示sortを適用する前のgroup row順は、各groupへ最初に入ったsource rowのcanonical source sequence順とする。empty inputから作るempty groupは唯一のrowである。

ソースrowがemptyでnon-aggregate Columnもない場合はempty groupを1件作り、`count`/`count_distinct`を0、その他をabsentにする。non-aggregate Columnが1件以上あるempty inputは0rowを生成する。

- `count`: ソース row数
- `count_distinct`: present normalized valueのdistinct数
- `min`/`max`: present typed valueの極値
- `join_unique`: distinct stringをcanonical スカラー orderで`, `結合
- `none`: 集約しないグループ化Column

`min`/`max`はinteger、number、date、datetimeまたは同一enum schema、`join_unique`はstring/enumだけに使える。enumの`min`/`max`は宣言option orderを使う。`count`と`count_distinct`の出力とTableColumn `value_type`はinteger、`min`/`max`は入力ScalarType、`join_unique`はstringである。`join_unique`のdistinct stringはUnicode code point順で`, `結合する。非aggregate field `address`/`type`/`layer`とrelation endpointは`stable_address`、`tags`は`string_set`、その他は宣言ScalarTypeを使う。present valueがないaggregateはabsent。ただし`count`と`count_distinct`は`0`。groupソースrefsはcontributing row/cellのordered unionとする。

sortはgrouping後にauthored orderで適用する。sort対象はsourceで名前を持つView table Columnまたは上記固定Column IDだけであり、runtimeに導出されるdynamic Column StableAddressをsource sortから参照してはならない。absentは宣言した位置へ置き、tieはsource-address tupleで解消する。

### 7.4 Matrix

axis selectorへ一致するprimary EntityをEntity StableAddress順に並べる。support Entityは、Queryの`result`によってprimaryにも選択されない限りaxisへ暗黙追加しない。同一axis内の重複membershipはerrorだが、1つのEntityがrow/column両axisに現れることは許可する。

Viewで列挙した全RelationTypeはeffective MatrixProjectionを持たなければならない。`relation_refs` cellでは各RelationTypeの`row_endpoint`→`column_endpoint`を`outgoing`、逆を`incoming`、両方を`both`として、axis Entityを結ぶ全selected Relationを入れる。View shapeの`semantic`だけが`relation_refs`か`path_refs`を決め、RelationType側で重複定義しない。

`path_refs`は追加のgraph探索を行わず、`ViewSelection.retained_paths`だけを使う。`outgoing`は先頭Entityがrow、末尾Entityがcolumnのpath、`incoming`は先頭がcolumn、末尾がrowのpath、`both`は両集合のordered unionとする。Queryにtraversalがない、またはQueryResultがpathを保持しない`path_refs` Viewはvalidation errorとする。Relation/pathはcanonical sortする。`attribute_summary`は`semantic relation_refs`かつ対象RelationTypeのeffective MatrixProjectionで`include_relation_rows true`の場合だけ有効で、`path_refs`との組み合わせを禁止する。

display valueはセマンティック valueを変更せず次のように導出する。

- `exists`: セマンティック refsがnon-emptyか
- `count`: Relation refまたはpath数
- `relation_types`: distinct RelationType StableAddressごとに1件のdisplay labelを作り、RelationType StableAddress順に並べたlist。同じlabelでも別typeなら除去しない
- `attribute_summary`: 明示設定された互換Columnのpresent Relation row cellを`(Relation StableAddress, row StableAddress, Column StableAddress)`順に並べた`MatrixAttributeItem` list

`attribute_summary`は値だけへ平坦化せず、Relation・row・Column・typed valueのtupleを保持する。重複値も異なるsource cellなら別itemであり、暗黙に除去しない。セマンティックrefがないcellもViewData cellとして存在し、ソースsetはemptyとする。totalは明示shapeフィールドが追加されるまで推測生成しない。

### 7.5 Tree

effective Tree projectionを持つRelationTypeだけを使う。RelationTypeのTree projectionはparent/child endpointだけを決め、cycle/shared-child policyはViewのTree shapeだけが所有する。parent/child candidateはDiagram candidate orderに従う。Query seedと`materialization_entity_addresses`の積集合が空でなければ、その集合をrootとする。積集合が空なら、materialization集合内のparent candidateを持たないEntityをrootとする。これによりQueryの`result`から除外されたseedをTreeが暗黙復活させない。cycleにより全Entityがparentを持つ場合は最初のcanonical materialization Entityから`cycle_policy`を適用する。

- `error`: 最初のcanonical cycleでView materialization全体を失敗させ、partial TreeViewDataを返さない
- `truncate`: cycle refを出して展開停止
- `duplicate_occurrence`: acyclicなshared-child pathだけ複製する。ancestry cycleを閉じるcandidateは新しいoccurrenceを作らず`cycle_refs`へ1件出して再帰展開を止める

shared childは`error`、`duplicate_occurrence`、`link`に従う。`error`は最初のcanonical shared childでView全体を失敗させる。`link`は最初のparentにprimary occurrenceを作り、後続parentからlink refを作る。全occurrenceはEntityとvia-Relationソースrefsを持つ。

### 7.6 Flow

materialization Entityをstep、effective Flow projectionを持つselected Relationをconnectorにする。`branch_value_column_address`がないRelationはconnectorを1件作る。指定がある場合はRelation rowごとにconnectorを1件作り、そのrowのcellがpresentなら`branch_value`、candidateの`branch_row_addresses`へそのrowを1件保持する。rowが0件ならbranch valueなし・row集合emptyのconnectorを1件作る。これにより複数rowを1つのscalarへ暗黙縮約しない。`preserve_parallel`がtrueなら全connectorを別々に保持し、falseならsource、target、connector kind、branch-value presenceとtyped valueが同じconnectorをRelation address、branch row address、SourceRefsのordered unionへまとめる。merge後のfinal connector multisetでindegree/outdegreeを数え、indegreeが1より大きいstepをjoin、outdegreeが1より大きいstepをbranchとする。

`branch_value_column_address`はそのFlowProjectionを所有するRelationTypeのColumnでなければならない。Viewで選択したRelationTypeにbranch Columnが複数ある場合、それらは同じScalarType、enum option sequence、formatを持たなければならず、不一致はView validation errorとする。rowにcellがない場合はabsentとして扱い、normalized rowですでにmaterializeされたColumn defaultだけを読み、追加のdefault適用はしない。connector mergeではabsentと明示的empty stringを区別する。

final connectorのcanonical orderは`(source Entity address,target Entity address,connector kind,branch-value-present,typed normalized branch value,Relation addresses,branch row addresses)`とする。この順で仮追加し、既存connectorと有向cycleを作るものをcycle-closing connectorとする。`cycle_policy error`は最初のcycle-closing connectorでViewを失敗させる。`truncate`はそのconnectorを通常集合へ入れず、connector key、kind、branch value、branch row set、Relation set、SourceRefsを完全に複製した`FlowCycleRef`へ入れる。`include_cycle_ref`はconnectorを保持し、同じpayloadの`FlowCycleRef`も作る。これ以外のcycle検出やhost依存のtopological heuristicを使わない。

lane keyは`none`、Layer StableAddress、EntityType StableAddress、またはtyped normalized attribute valueのいずれか。`lane_by attribute`では、step Entityの全rowから`lane_column_addresses`に一致するpresent cellを集め、typed equalityでdistinct化する。0値なら明示key`missing`、1値ならそのtyped valueを使い、2値以上なら1stepを1laneへ一意に置けないためView materialization errorとする。同じ値が複数rowにある場合は1laneとし、FlowLaneのSourceRefsへ全contributing row/cellを保持する。attribute lane labelはstring/enum/date/datetimeをnormalized stringのまま、integer/numberを正準decimal、booleanを`true`/`false`、`missing`をempty stringとして出す。laneはtype tagを含むcanonical key順、acyclic lane内stepはtopological orderの後にEntity StableAddressでtie-breakする。cyclic componentはEntity StableAddress順でcycle refsを持つ。

### 7.7 Context

各materialization Entityをfocal Entityとし、そのEntityに接続するselected Relationについてfactを生成する。focalが`from`ならoutgoing、`to`ならincomingで、shapeの対応flagがtrueの場合だけ生成する。self Relationは両flagがtrueならoutgoing/incomingを各1件、片方だけならその1件を作る。group keyは`none`なら定数`all`、`layer`ならfocal Layer StableAddress、`entity_type`ならfocal EntityType StableAddressとする。

group labelは`none`ならempty string、`layer`/`entity_type`なら対応subjectの`display_name`とし、host localeで置換しない。

effective forward templateは、明示値がなければ`"{from.display_name} <forward_label> {to.display_name}"`である。effective reverse templateは明示値、RelationTypeの`reverse_label`から作る`"{to.display_name} <reverse_label> {from.display_name}"`、forward templateの順で選ぶ。`{relation.display_name}`はinstance値があればそれを使い、なければRelationType `display_name`を使う。placeholder置換は文字列連結だけを行い、locale変換やMarkdown escapeを行わない。

effective templateからfact textを生成し、group key順、focal Entity StableAddress順、outgoingをincomingより先、RelationType/Relation StableAddress順とする。両directionを明示した場合だけ同じRelationからforward/reverse factを両方作る。`include_entity_rows`はfocal Entity rowを含める。Relation rowはshapeの`include_relation_rows`と、そのRelationTypeのeffective ContextProjection `include_attribute_rows`が両方trueの場合だけ含める。含めたRelation row addressは対応する`ContextFact.row_addresses`にも入れる。同じattributeが複数factから参照されても同一ContextGroup内ではrow addressで1件に統合し、SourceRefsは全参照factのordered unionにする。ContextAttributeは所属`group_key`を保持し、別groupの同じrowは別itemとする。この段階でMarkdownを生成しない。

### 7.8 Diff

Diff materializationは、Diff View定義を含むimmutable `recipe_revision`と、selectorから固定したimmutable `before_revision`、`after_revision`の3入力を持つ。recipe revisionは比較対象と同一である必要はない。Diff subject kindは`project`、`pack`、`entity_type`、`relation_type`、`layer`、`entity`、`relation`、`query`、`view`、`reference`、`entity_type_column`、`entity_type_constraint`、`relation_type_column`、`relation_type_constraint`、`entity_row`、`relation_row`、`query_parameter`、`view_table_column`、`view_export`の閉じた集合とする。`include`省略時は全kindを含む。

`detect_moves true`の場合だけ、after revisionがbefore revisionのdescendantであり、その履歴区間を覆う`IdentityHistory.move_closure` mappingを一意に構成できなければならない。mappingをbefore addressへ適用し、subjectはresult StableAddress、owner-scoped childはresult child addressで比較する。descendant関係または一意なmove chainを証明できなければView validation errorとする。`false`ではmappingを適用せず、任意の2 revisionを比較でき、ID変更はremoved/addedになる。

Diff sourceにQueryがある場合、recipe revisionで解決した`query_address`と同じStableAddressのQueryがbefore/after両revisionに存在しなければならない。各revisionのparameterはStableAddress、`value_type`、modifier、required/defaultが一致し、recipe revisionのcomplete arguments mapをそのまま検証できなければならない。Query本体は各revisionの定義を使って同じargumentsで独立評価し、Query own-subject/subtree hashが異なること自体は許可してDiff scopeの変化として扱う。`detect_moves`はQuery lookupやparameter addressへ適用しない。Queryまたはparameterがrenameされた比較は、QueryなしDiffを使うか、両revisionに共通する別Queryを宣言する。

各scopeはViewSelectionのEntity、Relation、row、cell Columnに加え、それらが参照するLayer、EntityType、RelationType、constraint、宣言元Pack root、Queryとparameterを再帰的に含むsubject closureとする。両revision scopeのaddressの和へDiff shapeの`include`を適用する。

move mappingはQuery評価後、scopeの和を取る前にbefore resultへ適用する。scopeに入ったaddressについては両revisionの実在subjectを比較するため、片側のQuery結果にだけ現れたという理由だけでadded/removedにしない。Queryを省略したDiffは両revisionのcomplete normalized subject closureを対象にする。

- beforeのみ: `removed`
- afterのみ: `added`
- 同addressでown-subject ハッシュが異なる: `updated`
- valid moveかつセマンティック同一: `moved`
- valid moveかつセマンティック変更あり: `moved_updated`

owner subtreeだけが変わってもowner own-subject ハッシュが同じならownerを`updated`にしない。child changeを別に出す。フィールド diffはnormalized フィールド pathとbefore/after valueを持ち、kind、result StableAddress、フィールド path順とする。heuristic renameは禁止する。

`pack` subjectだけはdeclaration own-subject hashではなくResolvedPackSummaryの`canonical_id`、`version`、`digest` tupleを比較する。同じPackOriginでversionまたはartifact digestが変われば`updated`とし、resolution changeをfield diffへ出す。これはsemantic `definition` hashとは別にexact dependency updateをDiffで可視化するためであり、Pack declaration childのsemantic changeは各child Diffにも現れる。Packには`moved`を適用しない。

`added`はafter subjectの全semantic leaf fieldを`before_present:false`、`removed`はbefore subjectの全semantic leaf fieldを`after_present:false`として列挙する。`updated`と`moved_updated`は値またはpresenceが変わったleafだけ、`moved`はempty `fields`を持つ。mapはkeyをpath tokenへ追加し、ordered listはlist全体を1 field valueとして比較してordinal差分を作らない。result StableAddressはafter addressがあればafter、なければbeforeとする。

### 7.9 プレーンエクスポート境界

plain exportは1つのcomplete ViewDataと1つのExportRecipeを入力にする。mediumで表現できないvisual metadataは省略できるが、declared fidelityとsource-ref要件を満たせない場合は失敗させる。黙ってfidelityを下げてはならない。

Language 1のplain export capabilityを次に固定する。表にないshape/formatは無効である。各cellは、XLSXの埋め込みViewData昇格を使わない場合のnative最大fidelityであり、`lossless > traceable_summary > visual_only > lossy`の順で、それ以下のfidelityだけを要求できる。

| shape | format | 最大fidelity | 必須条件 |
| --- | --- | --- | --- |
| 全shape | `json`, `yaml` | `lossless` | complete ViewData、`source_refs true` |
| diagram | `xlsx`, `html` | `traceable_summary` | occurrence/edge/container/overlay/badge/support sheetまたは構造とSourceRefsを保持 |
| diagram | `csv`, `tsv` | `traceable_summary` | `bundle true`、`source_manifest true`でoccurrence/edge/container/overlay/badge/supportを分割出力 |
| diagram | `svg`, `png`, `pdf`, `pptx`, `docx`, `drawio` | `visual_only` | visual elementからViewData keyへの対応をartifact metadataに保持 |
| diagram | `mermaid` | `lossy` | なし |
| table | `xlsx` | `lossless` | `view_data_json true`かつ`hidden_ids true`。それ以外は`traceable_summary` |
| table | `csv`, `tsv` | `traceable_summary` | `bundle true`、`header true`、`source_manifest true` |
| table | `html` | `traceable_summary` | row/cell SourceRefsを保持 |
| table | `pdf`, `pptx`, `docx` | `visual_only` | なし |
| table | `markdown` | `lossy` | なし |
| matrix | `xlsx` | `lossless` | `view_data_json true`かつ`hidden_ids true`。それ以外は`traceable_summary` |
| matrix | `csv`, `tsv` | `traceable_summary` | `bundle true`、`source_manifest true` |
| matrix | `html` | `traceable_summary` | cell semantic refsを保持 |
| matrix | `svg`, `png`, `pdf`, `pptx`, `docx` | `visual_only` | なし |
| tree | `xlsx`, `csv`, `tsv`, `html` | `traceable_summary` | occurrence key、path、cycle/link ref、SourceRefsを保持。CSV/TSVは`bundle true`、`source_manifest true` |
| tree | `mermaid` | `traceable_summary` | `source_manifest true`。未指定時は`lossy` |
| tree | `svg`, `png`, `pdf`, `pptx`, `docx`, `drawio` | `visual_only` | なし |
| flow | `xlsx`, `csv`, `tsv`, `html` | `traceable_summary` | step/connector/cycle refとSourceRefsを保持。CSV/TSVは`bundle true`、`source_manifest true` |
| flow | `mermaid` | `traceable_summary` | `source_manifest true`。未指定時は`lossy` |
| flow | `bpmn` | `lossy` | LayerDrawの汎用Entity/connectorをBPMN elementへ意味保存できないため、視覚的近似としてのみ許可 |
| flow | `svg`, `png`, `pdf`, `pptx`, `docx`, `drawio` | `visual_only` | なし |
| flow | `markdown` | `lossy` | なし |
| context | `csv`, `tsv`, `xlsx`, `html`, `markdown` | `traceable_summary` | factごとのSourceRefsを保持。CSV/TSVは`bundle true`、`source_manifest true` |
| context | `pdf`, `pptx`, `docx` | `visual_only` | なし |
| diff | `csv`, `tsv`, `xlsx`, `html`, `markdown` | `traceable_summary` | before/after address、field path、SourceRefsを保持。CSV/TSVは`bundle true`、`source_manifest true` |
| diff | `pdf`, `pptx`, `docx` | `visual_only` | なし |

`lossless`または`traceable_summary`を要求するrecipeは`source_refs true`を必須とする。`visual_only`でもSourceRefsを要求した場合は同伴Source Manifestを必須とする。`lossy`はsource ref保証を持たない。ただしViewDataの`state_policy`が`optional`または`required`なら、fidelityとrecipe flagにかかわらず同伴Source Manifestを必須とする。これによりsnapshot利用時だけでなくoptional no-state評価も成果物へ束縛する。表でXLSXが許可されているshapeは、`view_data_json true`かつ`hidden_ids true`なら明示的に最大fidelityを`lossless`へ引き上げる。complete ViewData JSONを変更せず埋め込み、workbook presentationとの差異があってもJSONを正とする。

PPTX/DOCXは1つのViewDataを図または表として置くplain exportだけを表す。複数View、文章構成、承認書式など異なるbusiness meaningを生成するDocument GenerationはLanguage 1 plain export外とする。

primary filenameのcanonical extensionを次に固定する: `json .json`、`yaml .yaml`、`svg .svg`、`png .png`、`pdf .pdf`、`html .html`、`csv .csv`、`tsv .tsv`、`xlsx .xlsx`、`markdown .md`、`pptx .pptx`、`docx .docx`、`mermaid .mmd`、`bpmn .bpmn`、`drawio .drawio`。`bundle true`でもprimary filenameは`.csv`または`.tsv`のままにし、追加tableと`<stem>.sources.json`を同じlogical ExportArtifact bundleへ入れる。`source_manifest true`の他形式も同名のcompanionをbundleへ追加する。host固有のZIP化や保存先pathはLanguage semantics外である。

#### 7.9.1 ExportPlanとSource Manifest

Language 1は次のnormalized `ExportInvocationInput`からsemantic `ExportPlan`を決定的に生成する。versioned exporter profileはrecipeの`exporter_profile`で固定され、invocation時に別profileへ黙って置換してはならない。フォント、text measurement、layout engine、rasterizer、PDF/OOXML/draw.io XML serializerなどartifact byte生成の技術仕様は、Language 1ではなくそのprofileが所有する。したがってLanguage適合性は異なるprofile間のvisual artifact byte一致を要求せず、同じExportInvocationInputから同じExportPlan、fidelity判定、representation集合、Source Manifest payloadを得ることを要求する。profile参照はPlanとManifestへ記録し、同じprofile内での再現性をprofile仕様が保証する。

Language 1 exporter-profile registryは`format:"layerdraw-exporter-profiles"`、`schema_version:1`、profile IDをkeyとする`profiles` mapを持つmachine-readable artifactである。各entryは`id`、対象`format`、profile specification artifactの`specification_digest`を持つ。registry digestはregistry objectをRFC 8785でserializeしたbytesのraw SHA-256、specification digestはcanonical profile specification bytesのraw SHA-256である。`ExporterProfileRef`は解決に使ったregistryのschema versionとdigestを必ず保持し、registry、ProfileRef、compiler/exporter bundleのentryおよびspecification bytesが一致しなければならない。これにより省略時の`layerdraw/<format>@1`を含むID解決表そのものを固定する。profile specificationのlayout/serializer内容はTAだが、ProfileRefによるregistry・identity・format・digest bindingは本言語契約である。Profileは本節のshape/format/fidelity、representation、SourceRefs要件を拡張・縮小・再解釈してはならず、そのartifact実現方法だけを定義する。Registryまたはprofile specificationの暗黙network取得は禁止する。

```text
ExportInvocationInput {
  view_data: DiagramViewData|TableViewData|MatrixViewData|TreeViewData|FlowViewData|ContextViewData|DiffViewData
  recipe: ExportRecipe
  state_summary?: ExternalStateSummary
}

ExportPlan {
  schema_version: 1
  invocation_hash: sha256
  view_data_hash: sha256
  state_policy: enum(none,optional,required)
  state_input: StateInputRef
  recipe_hash: sha256
  state_summary_hash?: sha256
  profile_ref_hash: sha256
  recipe_address: ref<view-export>
  requested_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  native_max_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  effective_max_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  fidelity_basis: enum(native,embedded_viewdata)
  format: ExportRecipe.format
  exporter_profile: ExporterProfileRef
  artifacts: ExportArtifactEntry[]
  representations: ExportRepresentation[]
}

ExportArtifactEntry {
  logical_path: basename
  role: string
  media_type: string
  primary: boolean
  content_digest?: "sha256:<lower-hex>"
}

CompletedExportArtifactEntry = ExportArtifactEntry & {
  content_digest: "sha256:<lower-hex>"
}

ExportRepresentation {
  viewdata_key: string
  artifact_role?: string
  locator?: string
  disposition: enum(rendered,tabular,embedded,omitted)
  omission_reason?: enum(unsupported_visual_detail,support_only,lossy_format)
  source: SourceRefs
}
```

`artifacts`は`(primary desc,role,logical_path)`、`representations`は`viewdata-root`を先頭、続いてViewData item key順にsortする。`lossless`はcomplete ViewDataを`embedded`するreserved `viewdata_key:"viewdata-root"` representationを必須とし、そのsourceはViewDataBase.sourceとする。他のrepresentationはpresentationである。`traceable_summary`は全primary semantic itemを`rendered`または`tabular`にし、support itemを省略する場合も`omitted`と理由とSourceRefsを残す。`visual_only`は表示対象itemだけを対応付け、`lossy`は完全な対応を要求しない。

artifactsはちょうど1件の`primary true`を持ち、その`logical_path`はExportRecipe filenameおよびSource Manifest `primary_artifact`と一致しなければならない。`rendered`/`tabular`/`embedded` representationは非空artifact role/locatorを必須とし、同じartifact role内のlocatorは一意とする。locatorは、同じprofile specification digestと同じExportPlanに対して決定的で、profileが定義する解決手順により対象artifact bytes内の要素を一意に指さなければならない。描画時刻、実行環境、非決定的な生成順序をlocatorへ使ってはならない。profile digestが変わればlocatorの互換性は要求しない。`omitted`はrole/locatorを禁止し`omission_reason`を必須とする。他のdispositionはomission reasonを禁止する。1つのViewData keyを複数artifactへ表現してよいが、同じ`(viewdata_key,artifact_role,locator)`は重複できない。

次のいずれかを満たす場合は`<stem>.sources.json`を必ず生成する。(1) `source_manifest true`、(2) ViewDataの`state_policy != none`、(3) `source_refs true`かつprimary artifactが7.9.2のcomplete ViewDataをlosslessに含むJSON/YAMLでも`view_data_json true`のXLSXでもない。artifact内部metadataだけで代替してはならない。schemaは次である。

```text
ExportSourceManifest {
  format: "layerdraw-export-sources"
  schema_version: 1
  invocation_hash: sha256
  view_data_hash: sha256
  state_policy: enum(none,optional,required)
  state_input: StateInputRef
  recipe_hash: sha256
  state_summary_hash?: sha256
  profile_ref_hash: sha256
  recipe_address: ref<view-export>
  revision: SingleRevision | DiffRevision
  requested_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  native_max_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  effective_max_fidelity: enum(lossless,traceable_summary,visual_only,lossy)
  fidelity_basis: enum(native,embedded_viewdata)
  exporter_profile: ExporterProfileRef
  primary_artifact: string
  artifacts: CompletedExportArtifactEntry[]
  representations: ExportRepresentation[]
}

ExportResult {
  plan: ExportPlan
  source_manifest_path?: basename
  source_manifest_digest?: "sha256:<lower-hex>"
  artifacts: CompletedExportArtifactEntry[]
}
```

`view_data_hash`、`recipe_hash`、`state_summary_hash`、`profile_ref_hash`、`invocation_hash`は9節の各export domainで計算する。Exportの`view_data_hash`は意味上の表示同一性を測る`viewdata` domainではなく、revision、shape、SourceRefs、StateRefs、state policy、StateInputRefを含む`export-viewdata` domainを必ず使う。Plan/Manifestの`state_policy`と`state_input`はViewDataBaseと完全一致し、state非依存Viewではpolicy none/input none、optional no-stateではpolicy optional/input none、snapshot利用時はhash/version/captured_at/definition hashを保持する。state summaryがない場合だけ`state_summary_hash`をabsentとし、`export-invocation` payloadではJSON `null`を使う。PlanとManifestの5hash値、state policy、state inputは一致しなければならず、recipe revisionから`recipe_address`を解決して再計算した値と異なるartifactは拒否する。これによりrecipe optionsを含むinvocation全体を、recipe addressだけに依存せず束縛する。

`native_max_fidelity`は7.9表、`effective_max_fidelity`はXLSX埋め込み昇格後の上限、`fidelity_basis`は昇格の有無を示す。requested fidelityがeffective maximumを超えるPlanは生成しない。ExportPlanではserialization前の`content_digest`を省略し、生成後のSource Manifestではmanifest自身を除く全artifact entryにraw artifact bytesのSHA-256を必須とする。Source ManifestはRFC 8785 JSONをUTF-8で出し、末尾LFを1つ付ける。manifest自身を`artifacts`へ再帰登録せず、raw manifest bytes digestをExportResultへ返す。`.layerdraw`内ではLayerdrawManifest `files`がこのdigestを束縛し、standalone deliveryではcaller/storageがExportResultのdigestをtrust anchorとして保持する。sidecar discoveryはprimary `<stem>.<ext>`に対する同directoryの`<stem>.sources.json`だけを使い、Manifestの`primary_artifact`とartifact digestが一致しなければならない。`logical_path`はbundle内basenameで一意、`locator`はformat profileが定義するsheet/row、DOM id、drawing object id、page/object idなどであり、同じartifact role内で一意でなければならない。これによりartifact bytes、ViewData item、sourceの対応を機械検証できる。

ExportPlanのartifact entriesではserialization前の`content_digest`を省略しなければならず、Source ManifestとExportResultは生成後の`CompletedExportArtifactEntry`だけを持つ。したがってSource Manifest有無にかかわらずManifest自身を除く全artifactのraw bytesがdigestで束縛され、Manifest自身は`source_manifest_digest`で別に束縛される。`source_manifest_path`と`source_manifest_digest`は両方presentまたは両方absentで、7.9.1の条件によりManifestが必要なexportではpresentでなければならない。

Source Manifestが保証するのは、artifact bytes、complete ViewData input、recipe、profile、StateInputRef、およびSourceRefs/StateRefsの出自束縛である。standalone plain exportはStateQuerySnapshot本体を暗黙同梱せず、Master GraphからQuery/Viewを再実行できるとは主張しない。lossless JSON/YAMLまたは埋め込みViewDataは同じViewDataから別媒体を再生成できるが、同じMaster GraphからViewDataを再materializeする保証とは別である。後者のportable rematerializationは3.5節の`.layerdraw` snapshot entryを必要とする。ExternalStateSummaryは表示・連携用の任意attachmentであり、StateQuerySnapshotの代替にしてはならない。

#### 7.9.2 JSONとYAML

JSON/YAML lossless artifactは次のenvelopeをcompleteに含む。

```text
ViewDataArtifact {
  format: "layerdraw-viewdata"
  schema_version: 1
  view_data: DiagramViewData|TableViewData|MatrixViewData|TreeViewData|FlowViewData|ContextViewData|DiffViewData
  diagnostics?: Diagnostic[]
  state_summary?: ExternalStateSummary
}

ExternalStateSummary {
  format: "layerdraw-state-summary"
  schema_version: 1
  definition_hash: sha256
  state_version: integer|string
  payload_hash: sha256
  payload: NormalizedValue
}
```

`diagnostics`と`state_summary`はrecipe flagがtrueの場合だけ含める。canonical ViewData artifact内のすべてのDiagnostic/DiagnosticRelatedはoptional localized `message`を省略し、stable identity fieldsだけを出す。`state_summary true`はSingleRevision Viewだけで有効で、その`definition_hash`と一致するimmutable ExternalStateSummaryを`ExportInvocationInput.state_summary`へ明示しなければならない。trueで欠落、falseでpresent、不一致、payload hash不一致はexport errorとする。`state_summary_hash`はExternalStateSummary全体を9節の`export-state-summary` domainでハッシュし、ExportPlanとSource Manifestへ必ず記録する。`payload_hash`はRFC 8785でserializeした`payload` bytesのraw SHA-256を`sha256:<lower-hex>`で表す。Diffでstateを添付する場合はLanguage plain exportではなく、before/after state contractを持つDocument Generationを使う。state payloadの意味はstate schemaが所有し、LDL compilerは解釈しない。JSONはRFC 8785 bytesに末尾LFを1つ付ける。YAMLはYAML 1.2でJSONと共通のdata modelだけを使い、anchor、alias、tag、directive、comment、non-JSON scalarを禁止する。正準YAML bytesは上記RFC 8785 JSON textと同一とする。JSONはYAML 1.2の有効なsubsetであるため、`.yaml` extensionでもbyte一意性とlossless round-tripを両立する。

#### 7.9.3 CSVとTSV bundle

CSV/TSVはUTF-8、LF、double-quote escapingを使う。CSV delimiterは`,`、TSV delimiterはTABである。fieldにdelimiter、`"`、CR、LFのいずれかがあればfield全体を`"`で囲み、内部`"`を`""`にする。integer/number/boolean/date/datetimeはnormalized scalar text、list/map/SourceRefsはRFC 8785 JSON text、absent cellはempty field、明示empty stringはquoted `""`とする。`header true`では最初のrowへcanonical column IDを出す。

shapeごとのtable roleは次で固定する。最初のroleがprimary filenameを使い、残りは`<stem>.<role>.<csv|tsv>`を使う。

| shape | table role |
| --- | --- |
| Diagram | `occurrences`, `edges`, `containers`, `overlays`, `badges`, `support_items` |
| Table | `rows`, `columns` |
| Matrix | `cells`, `row_axis`, `column_axis` |
| Tree | `occurrences`, `cycle_refs`, `link_refs` |
| Flow | `steps`, `connectors`, `lanes`, `cycle_refs` |
| Context | `facts`, `attributes`, `groups` |
| Diff | `changes`, `field_diffs` |

各ViewData itemを1rowにし、nested itemはowner keyを持つ別roleへ展開する。各rowは先頭に`viewdata_key`、次にnested owner key、続いて7.10節のschema記載順のfield、末尾に`source_refs`を持つ。Tree occurrenceは`parent_occurrence_key`、Context factは`group_key`、Diff FieldDiffは`diff_change_key`をexport-only owner columnとして加える。ContextAttributeのgroup keyはschema fieldを使う。map/set/list/objectは1 field内のRFC 8785 JSON textとし、SourceRefsも同じ方法で格納する。全shapeの`traceable_summary` CSV/TSVは`bundle true`、`header true`、`source_manifest true`を必須とする。いずれかがfalseなら最大fidelityを`lossy`とする。

#### 7.9.4 XLSXとその他の形式

XLSX profileのvisible sheet/table構成はExportPlanのartifact roleとrepresentationを実装する。`hidden_ids true`は各visible itemにViewData keyを対応付ける非表示`_layerdraw_ids` sheetを必須とする。`view_data_json true`は7.9.2のcanonical JSON bytesをpaddingなしbase64urlへ変換し、非表示`_layerdraw_viewdata` sheetへ格納する。row 1は`chunk_index`,`base64url_chunk` header、row 2以降は0始まりindexと30,000 ASCII文字以下の連続chunkを持つ。index順に連結・base64url decodeしたbytesがcanonical JSONと一致しなければならない。workbook row limitを超える場合はlosslessへ黙って降格せずresource-limit export errorとする。reserved sheet名衝突時に別名へ変えてはならずexport errorとする。

HTML、Markdown、Mermaid、BPMN、SVG、PNG、PDF、PPTX、DOCX、draw.ioはExportPlanのsemantic mappingを満たすversioned exporter profileを必要とする。HTMLのinteractive code、外部asset URL、script trustはLanguage semantics外で、`embed_assets false`でもimplicit network fetchを行ってはならない。Markdownの`traceable_summary`、Mermaidの`traceable_summary`、およびvisual formatで`source_refs true`の場合はSource Manifestが必須である。BPMNは汎用Flow semanticsを完全保存しない`lossy` profileだけを許し、mapping recipeの存在を暗黙前提にしない。draw.ioはDiagram/Tree/Flowのvisual occurrence/edge/connectorを図形へ写す`visual_only` profileであり、editable Master Graph形式ではない。

[view-conversion-contract.md](view-conversion-contract.md)は上表の説明資料であり、規範能力を追加・変更してはならない。

### 7.10 ViewDataの通信スキーマ

全ViewDataは次を持つ。

```text
ViewDataBase {
  kind: enum(diagram,table,matrix,tree,flow,context,diff)
  category: enum(topology,inventory,dependency,hierarchy,flow,impact,diff,context)
  shape: ViewShape
  project_address: ref<project>
  view_address: ref<view>
  query_address?: ref<query>
  revision: SingleRevision | DiffRevision
  state_policy: enum(none,optional,required)
  state_input: StateInputRef
  source: SourceRefs
  diagnostics: Diagnostic[]
}

SingleRevision { kind:"single", revision_id:non-empty string, definition_hash:sha256 }
DiffRevision { kind:"diff", recipe_revision_id:non-empty string, recipe_definition_hash:sha256, before_revision_id:non-empty string, before_definition_hash:sha256, after_revision_id:non-empty string, after_definition_hash:sha256 }

SourceRefs {
  subject_addresses: set<StableAddress>
  entity_addresses: set<ref<entity>>
  relation_addresses: set<ref<relation>>
  layer_addresses: set<ref<layer>>
  row_addresses: set<ref<row>>
  cell_refs: set<{row_address:ref<row>,column_address:ref<column>}>
  asset_digests: set<sha256>
  state: StateRefs
}

StateRefs { reads:set<StateReadRef> }
```

ソースcollectionはemptyでも必須。`subject_addresses`は他の全StableAddress ref集合とStateReadRef subject addressの和に加え、Project、Pack、型、Query、View、Reference、constraint、parameter、View childなど専用集合を持たないsubjectを保持する。AssetはStableSymbolではないため`asset_digests`だけに置く。StableSymbol tuple順、cell refはrow、column address順、asset digestはlower-hex順、StateRefsはsubject addressとfield registry順とする。`state_policy:none`ではStateRefsをemptyとする。`optional`のno-state inputでは値を取得できなくても、結果へ影響したattempted readをStateRefsへ保持する。snapshotの場合、ViewDataBase sourceはQueryResultの全state readsとshape materializationの全直接read、各item/cell sourceはそのdefinition SourceRefsに属するsubjectへのQuery readと自身の直接readを持つ。summary/aggregateはcontributing StateRefsをordered unionする。derived conflictにソースtokenがないView診断はstable診断envelopeからsource-module rangeを省略できる。

Query-backed Viewは`SingleRevision`、Diff Viewは`DiffRevision`を必須とする。各definition hashは対応Committed Revisionの`definition` domain hashである。Diffの`project_address`はafter revisionのProject StableAddressで、before側Project addressはDiffChangeの`before_address`と`before_source`に保持する。`recipe_revision_id`はView recipeとexport recipeを解決したCommitted Revisionを指し、before/afterだけから推測してはならない。Diff Viewの`state_policy`と`state_input`は必ずnoneである。

```text
DiagramViewData = ViewDataBase & {
  occurrences: DiagramOccurrence[]
  edges: DiagramEdge[]
  containers: DiagramContainer[]
  overlays: DiagramOverlay[]
  badges: DiagramBadge[]
  support_items: DiagramSupportItem[]
}
DiagramOccurrence { key:string, entity_address:ref<entity>, layer_address:ref<layer>, parent_key?:string, via_relation_address?:ref<relation>, role:enum(node,container,support), source:SourceRefs }
DiagramEdge { key:string, from_occurrence_key:string, to_occurrence_key:string, relation_address:ref<relation>, relation_type_address:ref<relation-type>, source:SourceRefs }
DiagramContainer { key:string, occurrence_key:string, child_keys:string[], source:SourceRefs }
DiagramOverlay { key:string, target_occurrence_key:string, overlay_entity_address:ref<entity>, relation_address:ref<relation>, relation_type_address:ref<relation-type>, source:SourceRefs }
DiagramBadge { key:string, target_occurrence_key:string, relation_address:ref<relation>, relation_type_address:ref<relation-type>, label?:string, source:SourceRefs }
DiagramSupportItem { key:string, support_kind:enum(hidden_relation,hidden_entity,source_only), entity_address?:ref<entity>, relation_address?:ref<relation>, source:SourceRefs }
```

Diagram arrayはmaterialization candidate order、child keyはoccurrence orderを使う。

`hidden_relation`は`relation_address`必須・`entity_address`省略、`hidden_entity`は`entity_address`必須・`relation_address`省略、`source_only`は原因に応じて少なくとも一方を必須とする。overlayはeffective `overlay_endpoint`のEntityを必ず`overlay_entity_address`へ保持する。

```text
TableValueType = ScalarType | enum(stable_address,string_set)
TableViewData = ViewDataBase & { columns:TableColumn[], rows:TableRow[] }
TableColumn { key:string, id:string, address?:child-ref<View,table-column>, label:string, value_type:TableValueType, enum_values?:string[], source_column_addresses:set<ref<column>>, state_field_path?:StateFieldPath }
TableRow { key:string, cells:map<column-key,TableCell>, source:SourceRefs }
TableCell { present:boolean, value?:Scalar|StableAddress|string[], source:SourceRefs }

MatrixViewData = ViewDataBase & { row_axis:MatrixAxisItem[], column_axis:MatrixAxisItem[], cells:MatrixCell[] }
MatrixAxisItem { key:string, entity_address:ref<entity>, label:string, source:SourceRefs }
MatrixAttributeItem { relation_address:ref<relation>, row_address:ref<relation-row>, column_address:ref<column>, value:Scalar }
MatrixCell { key:string, row_key:string, column_key:string, semantic_refs:(ref<relation>|QueryPath)[], display_value:boolean|integer|string[]|MatrixAttributeItem[], source:SourceRefs }

TreeViewData = ViewDataBase & { roots:TreeOccurrence[], cycle_refs:TreeRef[], link_refs:TreeRef[] }
TreeOccurrence { key:string, entity_address:ref<entity>, via_relation_address?:ref<relation>, children:TreeOccurrence[], source:SourceRefs }
TreeRef { key:string, from_occurrence_key:string, to_entity_address:ref<entity>, relation_address:ref<relation>, source:SourceRefs }

FlowViewData = ViewDataBase & { lanes:FlowLane[], steps:FlowStep[], connectors:FlowConnector[], cycle_refs:FlowCycleRef[] }
FlowLane { key:string, label:string, step_keys:string[], source:SourceRefs }
FlowStep { key:string, entity_address:ref<entity>, lane_key:string, branch:boolean, join:boolean, source:SourceRefs }
FlowConnector { key:string, from_step_key:string, to_step_key:string, kind:enum(sequence,control,data,message,error), branch_value?:Scalar, branch_row_addresses:set<ref<relation-row>>, relation_addresses:set<ref<relation>>, source:SourceRefs }
FlowCycleRef { key:string, connector_key:string, from_step_key:string, to_step_key:string, kind:enum(sequence,control,data,message,error), branch_value?:Scalar, branch_row_addresses:set<ref<relation-row>>, relation_addresses:set<ref<relation>>, source:SourceRefs }

ContextViewData = ViewDataBase & { groups:ContextGroup[] }
ContextGroup { key:string, label:string, facts:ContextFact[], attributes:ContextAttribute[], source:SourceRefs }
ContextFact { key:string, direction:enum(outgoing,incoming), text:string, entity_address:ref<entity>, relation_address:ref<relation>, row_addresses:set<ref<row>>, source:SourceRefs }
ContextAttribute { key:string, group_key:string, owner_address:ref<entity|relation>, row_address:ref<row>, values:map<ref<column>,Scalar>, source:SourceRefs }

DiffViewData = ViewDataBase & { changes:DiffChange[] }
DiffChange { key:string, kind:enum(added,removed,updated,moved,moved_updated), subject_kind:DiffSubjectKind, before_address?:StableAddress, after_address?:StableAddress, fields:FieldDiff[], source:SourceRefs, before_source?:SourceRefs, after_source?:SourceRefs }
FieldDiff { key:string, path:string[], before_present:boolean, before?:NormalizedValue, after_present:boolean, after?:NormalizedValue }
```

全`key`はdeterministicなViewData-local identityとする。map key重複は禁止する。`TableCell`の`present:false`は`value`を持たず、明示的 empty stringは`present:true`である。

Tableの`value_type:enum`は`enum_values`を1件以上必須とし、その他の型は省略する。`source state` Columnは`state_field_path`必須、`source_column_addresses` emptyで、enumなら5.4節のoption orderを使う。`source attribute`はstate pathを禁止しsource Column setを必須、固定/field/endpoint/derived Columnは両方を禁止する。

DiffChangeの`before_source`はbefore subjectが存在するkind、`after_source`はafter subjectが存在するkindで必須とし、存在しないsideでは省略する。`source`は両sideのordered unionである。これにより同じStableAddressが両revisionに存在しても、source refのrevision sideを失わない。

## 8. 正準フォーマット

### 8.1 決定的レイアウト

formatterのlogical line widthはindent後100 Unicode スカラー valuesとする。Entity/Relation/row item、heredoc content、string literalは自動wrapしない。それ以外のlist/objectは、complete formatted constructが収まりattached commentがなければ1行、そうでなければ1 item per line、trailing commaあり、owner indent + 2 spacesとする。

空ブロックは`{}`。non-empty ブロックはheader行末に`{`、closing braceを単独行へ置く。token間は原則1 spaceとし、`.`、`@`、range `..`、predicate operator、`:`、`->`はcanonical sampleに従う。`:`の後は1 space、`->`の両側は1 spaceとする。

importはmodule documentation後の連続sectionにまとめる。importとdeclaration間、top-level declaration間は1 blank line。ただし同kindの隣接groupはblank lineなしを保持できる。先頭/末尾blank lineを除去し、file末尾はLF 1つとする。

View shape内部のcanonical field/block順は次とする。

| shape | 順序 |
| --- | --- |
| Diagram | `layout`, `direction`, `abstraction`, `composed`, Entity StableAddress順の`place` |
| Table | `rows`, `entity_types`, `entity_id`, `type`, `layer`, authored `column`, authored `sort` |
| Matrix | `row_axis(entity_types,label)`, `column_axis(entity_types,label)`, `cell(relation_types,direction,semantic,display,attributes)` |
| Tree | `relation_types`, `cycle_policy`, `shared_child_policy` |
| Flow | `relation_types`, `lane_by`, `cycle_policy`, `preserve_parallel` |
| Context | `group_by`, `entity_rows`, `relation_rows`, `incoming`, `outgoing` |
| Diff | `include`, `detect_moves` |

Relation projection/render nested fieldは言語本体14.7/14.8節のsample順、ExportRecipeは言語本体24.1節の順を使う。

### 8.2 コメントとヒアドキュメント

standalone `//` commentは次のsyntax nodeより前の相対位置を保持し、そのnodeと同じindentにする。end-of-line commentはsyntaxから1 space空けて同じlogical lineへ残す。wrapが必要ならcommentを動かす前にconstructをmultiline化する。

`///`はdeclaration/group item直前、`//!`はsource file先頭の連続module-doc sectionにだけ置く。連続する複数のmodule-doc lineは合法で順序を保つ。`///`とdeclaration間のblank lineは禁止する。

formatterはheredoc markerとセマンティック contentを保持する。line endingをLFへ統一し、nonblank contentをowner ブロック indent + 2 spaces、closing markerをフィールド indentへ置く。セマンティック text末尾LFの有無を保持する。common indent除去はparse時に1回だけ行い、format後の再parseで同じtextを得る。

### 8.3 無効な作業中文書

構造的に不完全なWorking Documentのformatはbest-effortでありcanonical outputを保証しない。ソース treeが構造parseでき、全statementをスキーマ分類できる場合だけcanonical formatを定義する。missing endpointなどformatと無関係なセマンティック errorはlocal whitespace formatを妨げない。フィールド/set/reservation/moveのreorderにはdeclaration-level 検証成功が必要である。

## 9. 正準シリアライズとハッシュ

Language 1のセマンティック ハッシュはSHA-256を使い、`sha256:<lowercase-hex>`で表す。preimageは次とする。

```text
UTF8("layerdraw-language-1\u0000" + domain + "\u0000")
  || RFC8785(canonical-payload)
```

domainは`resolved`、`definition`、`graph`、`state-query-snapshot`、`query`、`view`、`viewdata`、`export-viewdata`、`export-recipe`、`export-profile-ref`、`export-state-summary`、`export-invocation`、`subject`、`subtree`、`child-set`、`operation-batch`、`viewdata-item`とし、異なるdomainのハッシュを交換可能として扱わない。

- ソース byte digestはdomain prefixなしでexact ソース bytesをraw SHA-256するintegrity digestでありセマンティック ハッシュではない
- resolved digestは3.4節のnormalized metadataをハッシュする
- own-subject payloadは`{kind,address,fields}`。normalized subjectから`id`と全セマンティック フィールドを含め、addressable child payloadとソース metadataを除外する
- Project root own-subject fieldsはProjectのCommon/display field、そのrootのreservation、ProjectOrigin immediate movesとmove_closure。Pack rootはPackRoot field、そのrootのreservation、当該PackOrigin immediate movesとmove_closureを含む
- subtree payloadは`{owner_address,owner_hash,children:[{address,hash}]}`。childはStableSymbol順で、childがaddressable childを持つ場合の`hash`はそのchildのsubtree hash、持たない場合はown-subject hashとする。Project rootは全project-local subject、各Pack rootはeffective closureに入った当該PackOrigin subjectを再帰的に覆う
- graph payloadはactive Project graph フィールド、type、Layer、Entity、Relation、row、およびそれらが参照するAssetRef/AssetBlobSummaryを含み、reservation、move、Query、View、Reference、ソース metadataを除外する
- definition payloadはcomplete normalized declaration contentとPackOrigin identityを含むが、ResolvedPackSummaryの`version`/`digest`、dependency Registry/install/path、ソース metadataを除く。exact dependency bytesは`resolved` domainで別に束縛する
- state-query-snapshot payloadは5.4節のcomplete StateQuerySnapshot。backend locator、backend version/ETag、credential、Access tokenは含めない
- Query payloadはnormalized Query、参照スキーマのsubtreeハッシュ、input revision graph hash、arguments、実効StateInputRef。state非依存Queryは必ず`{kind:"none"}`を使う
- Query-backed View payloadはnormalized View subtree hash、引数適用済みQuery hash、input revision graph hash、Viewの直接state参照に使うStateInputRef。Diff View payloadはnormalized View subtree hash、recipe/before/after definition hashでStateInputRefを含めない。ExternalStateSummaryは含めない
- ViewData payloadは7.10節のcomplete ViewData object。shapeを含み、revision、すべてのSourceRefs、Diagnostic/DiagnosticRelatedのoptional localized `message`、transport pagination、RenderDataだけを除外する
- export-viewdata payloadは7.10節のcomplete ViewData object。revision、shape、すべてのSourceRefsを含み、Diagnostic/DiagnosticRelatedのoptional localized `message`だけを省略する
- export-recipe payloadは7.9節のcomplete normalized ExportRecipe
- export-profile-ref payloadはcomplete ExporterProfileRef
- export-state-summary payloadはcomplete ExternalStateSummary
- export-invocation payloadは`{view_data_hash,recipe_hash,state_summary_hash,profile_ref_hash}`。state summary absent時の値はJSON `null`
- child-set payloadは`{owner_address,child_kind,child_addresses}`。addressをStableSymbol順に並べる
- operation-batch payloadはeffective `document_id`をmaterializeし、map/set/orderを本書どおり正規化したcomplete OperationBatch
- ViewData item keyは`vdi:<kind>:<digest>`とし、`digest`はitem kindごとのtuple payloadを`viewdata-item` domainでハッシュした32 byte SHA-256のpaddingなしbase64url表現とする

ViewData item keyのtuple payloadを次に固定する。tuple内の集合はそれぞれの正準順序、typed valueはnormalized JSON表現を使う。

| item kind | tuple payload |
| --- | --- |
| `diagram-occurrence` | `[view_address,entity_address]` |
| `diagram-edge` | `[view_address,relation_address,from_occurrence_key,to_occurrence_key]` |
| `diagram-container` | `[view_address,occurrence_key]` |
| `diagram-overlay` / `diagram-badge` | `[view_address,relation_address,target_occurrence_key]` |
| `diagram-support` | `[view_address,support_kind,entity_address?,relation_address?]` |
| `table-column` | `[view_address,address-or-fixed-id]` |
| `table-row` | `[view_address,row_source,base-row-identity-tuples,group-key-values]` |
| `matrix-axis` | `[view_address,axis-kind,entity_address]` |
| `matrix-cell` | `[view_address,row-entity-address,column-entity-address]` |
| `tree-occurrence` | `[view_address,root-entity-address,ancestry-relation-addresses,entity_address]` |
| `tree-ref` | `[view_address,ref-kind,from_occurrence_key,to_entity_address,relation_address]` |
| `flow-lane` | `[view_address,normalized-lane-key]` |
| `flow-step` | `[view_address,entity_address]` |
| `flow-connector` | `[view_address,from_entity_address,to_entity_address,connector_kind,branch_value?,branch_row_addresses,relation_addresses]` |
| `flow-cycle-ref` | `[view_address,connector_key]` |
| `context-group` | `[view_address,normalized-group-key]` |
| `context-fact` | `[view_address,direction,entity_address,relation_address,row_addresses]` |
| `context-attribute` | `[view_address,group_key,owner_address,row_address]` |
| `diff-change` | `[view_address,change-kind,before_address?,after_address?]` |
| `diff-field` | `[view_address,diff-change-key,path]` |

同じtupleから異なるitemを生成してはならない。もしmaterialization規則の変更で同一tupleが複数itemを表す必要が生じた場合、実装固有suffixを加えずLanguage schemaを改訂する。

Tableのbase-row identity tupleは`entity`=`[entity_address]`、`entity_rows`=`[entity_address,row_address]`、`relation`=`[relation_address]`、`relation_rows`=`[relation_address,row_address]`、`automatic_relations`は実際に選んだgrainに応じてrelationまたはrelation-row tupleを使う。非aggregate rowはその1tuple、aggregate rowはcontributing tupleのcanonical listを使う。`group-key-values`はnon-aggregate Columnのpresent flagとtyped normalized valueをdeclaration orderで並べ、non-aggregate Columnがなければempty listとする。SourceRefsのtype/Layer/asset追加だけでrow keyを変えてはならない。

normalized スキーマでrequired collectionとした空collectionはシリアライズする。optional スカラー/objectはabsentのままとする。本規則、normalized スキーマ、RFC 8785でハッシュ bytesを一意に決める。

### 9.1 スキーマバージョニング

LDL source syntax generationとgenerated スキーマ versionは分離する。Source fileはversion headerを持たない。Language 1はnormalized スキーマ `1`、resolved スキーマ `1`、StateQuerySnapshotスキーマ`1`、セマンティック index スキーマ `1`、診断 protocol `1`、operation protocol `1`から開始する。normalized contentの互換identityはcanonical/public media-type versionとtop-level `schema_version`の組であり、Engine Protocol schema digestは`BlobRef`を含むcontrol JSONだけを束縛する。

normalized outputの必須フィールド、enum意味、normalized既定値、ordering、ハッシュinput、reference表現、またはcanonicalizationの追加・変更は、normalized `schema_version`とProject/Packそれぞれのcanonical/public media-type versionをすべて上げる。Engine schemaのmedia-type定数も同じ変更で更新し、control schema digestへaccepted content versionを反映する。同じversionの内容またはbyte driftは不適合である。セマンティックでないoptional ソース/index フィールドはminor metadata versionで追加できるがセマンティック ハッシュを変えてはならない。readerは未知higher versionを拒否する。旧versionからのmigrationは明示的・決定的に新artifactを生成し、未知フィールドを推測または黙って破棄しない。

accepted LDL syntaxまたはLanguage 1の意味を変える変更は新しいLDL generationを必要とする。ただしgenerationはsource headerではなくcompiler capability、release manifest、container/package metadata、およびmigration toolingで扱う。freeze後のLanguage 1 fixtureは、valid inputのnormalized resultと無効 inputのrequired primary 診断を一切変えないclarificationだけを許可する。

## 10. 診断

```text
SourceOrigin = { kind:"project" } | { kind:"pack", pack_address:ref<pack> }
SourceRange { origin:SourceOrigin, module_path:normalized origin-relative source path, start_byte:non-negative integer, end_byte:non-negative integer }
DiagnosticRelated { relation:enum(cause,conflict,dependency,previous,current,target), message?:string, range?:SourceRange, subject_address?:StableAddress, owner_address?:StableAddress }
Diagnostic {
  protocol_version: 1
  code: LDL diagnostic code
  severity: enum(error,warning,info)
  message_key: lower_snake_case
  arguments: map<string,NormalizedValue>
  message?: string
  range?: SourceRange
  subject_address?: StableAddress
  owner_address?: StableAddress
  related: DiagnosticRelated[]
}
```

`start_byte <= end_byte`を必須とし、ソースに対応しないstate/protocol診断は`range`を省略する。owner-scoped subjectでownerを特定できる場合は`owner_address`を必須とし、subject addressの文字列parseだけに依存させない。`message_key`と`arguments`はlanguage-neutralなstable diagnostic identityである。`message`は任意localeの人間向けrenderingであり、互換性、sort、hash、cache keyに使わない。clientは`code`、`message_key`、`arguments`、構造field、subject/ownerを使う。

### 10.1 安定コードレジストリ

| 範囲 | 分類 |
| --- | --- |
| `LDL1000-LDL1099` | encoding / 字句構文 |
| `LDL1100-LDL1199` | 構造構文 / 宣言スキーマ |
| `LDL1200-LDL1299` | module / import / export / pack / asset解決 |
| `LDL1300-LDL1399` | シンボル / 識別子 / 予約 / 移動 |
| `LDL1400-LDL1499` | スカラー / Column / row / unique |
| `LDL1500-LDL1599` | Relation endpoint / 重複 / cardinality / projection |
| `LDL1600-LDL1699` | Query 検証 / 評価 |
| `LDL1700-LDL1799` | View / ViewData / エクスポート |
| `LDL1800-LDL1899` | operation / revision / ハッシュ / conflict |
| `LDL1900-LDL1999` | 状態境界 / security |

最初のregistry entryを次に固定する。

| コード | 既定severity | 意味 |
| --- | --- | --- |
| `LDL1001` | error | 無効なUTF-8 |
| `LDL1101` | error | 構造構文error |
| `LDL1102` | error | 未知または重複したフィールドまたはブロック |
| `LDL1201` | error | 未解決またはescaping module |
| `LDL1202` | error | import循環 |
| `LDL1203` | error | dependency digest不一致 |
| `LDL1301` | error | 未知または曖昧なsymbol |
| `LDL1302` | error | 重複または予約済みidentity |
| `LDL1303` | error | 無効なmove graph |
| `LDL1401` | error | スカラー/Column type不一致 |
| `LDL1402` | error | 無効または重複したrow |
| `LDL1403` | error | unique違反 |
| `LDL1501` | error | 無効なRelation endpoint/self rule |
| `LDL1502` | error | 重複 policy違反 |
| `LDL1503` | error | cardinality違反 |
| `LDL1504` | error | 無効なprojection契約 |
| `LDL1601` | error | 無効なQuery/arguments |
| `LDL1602` | info | 明示的 Query rootが現在ineligible |
| `LDL1603` | error | `error` policyでのtraversal cycle |
| `LDL1604` | error | required StateQuerySnapshot欠落または参照state recordがstale |
| `LDL1605` | warning | optional StateQuerySnapshot欠落またはstale recordをmissingとして評価 |
| `LDL1701` | error | 無効なView ソース/category/shape |
| `LDL1702` | error | Viewの具現化競合 |
| `LDL1703` | error | 未対応のエクスポート忠実度またはオプション |
| `LDL1704` | warning | composed multi-parent候補を選択せずsupportとして保持 |
| `LDL1801` | error | stale revision/セマンティック ハッシュ |
| `LDL1802` | error | セマンティック操作 conflict |
| `LDL1901` | error | 禁止された状態/credential/実行可能 content |
| `LDL1902` | warning | definitionはcommittedだがstate reconciliationが必要 |
| `LDL1903` | error | operation recovery結果を一意に確定できずneeds-review |
| `LDL1904` | error | state fieldへのAccess拒否またはredacted field参照 |

上表entryのstable `message_key`を次に固定し、primary diagnosticの`arguments`はempty mapとする。

| code | message_key |
| --- | --- |
| `LDL1001` | `invalid_utf8` |
| `LDL1101` | `invalid_structure_syntax` |
| `LDL1102` | `unknown_or_duplicate_schema_member` |
| `LDL1201` | `module_pack_or_asset_resolution_failed` |
| `LDL1202` | `import_cycle` |
| `LDL1203` | `dependency_digest_mismatch` |
| `LDL1301` | `unknown_or_ambiguous_symbol` |
| `LDL1302` | `duplicate_or_reserved_identity` |
| `LDL1303` | `invalid_move_graph` |
| `LDL1401` | `scalar_or_column_type_mismatch` |
| `LDL1402` | `invalid_or_duplicate_row` |
| `LDL1403` | `unique_constraint_violation` |
| `LDL1501` | `invalid_relation_endpoint_or_self_rule` |
| `LDL1502` | `relation_duplicate_policy_violation` |
| `LDL1503` | `relation_cardinality_violation` |
| `LDL1504` | `invalid_projection_contract` |
| `LDL1601` | `invalid_query_or_arguments` |
| `LDL1602` | `query_root_ineligible` |
| `LDL1603` | `query_cycle_policy_violation` |
| `LDL1604` | `required_query_state_unavailable_or_stale` |
| `LDL1605` | `optional_query_state_missing_or_stale` |
| `LDL1701` | `invalid_view_source_category_or_shape` |
| `LDL1702` | `view_materialization_conflict` |
| `LDL1703` | `unsupported_export_fidelity_or_options` |
| `LDL1704` | `composed_parent_ambiguity_retained` |
| `LDL1801` | `stale_revision_or_semantic_hash` |
| `LDL1802` | `semantic_operation_conflict` |
| `LDL1901` | `forbidden_state_credential_or_executable_content` |
| `LDL1902` | `definition_committed_state_reconciliation_required` |
| `LDL1903` | `operation_recovery_needs_review` |
| `LDL1904` | `query_state_field_forbidden_or_redacted` |

既存ruleのrequired primary codeを変えない範囲でsupplementary codeを追加できる。`LDL1001`〜`LDL1901`および`LDL1904`のerrorは、source compile、Query評価、View materialization、export validation、またはsemantic operationの該当処理を失敗させ、その処理が要求した新しいCommitted Revisionまたはartifactの公開を禁止する。`LDL1605`はoptional stateを決定的なmissingとして扱った評価結果に付くwarningであり、結果を失敗させない。`LDL1902`と`LDL1903`はdefinition公開後のstate protocolまたはoperation recoveryの状態を報告する非gate診断であり、既存のCommitted Revisionを遡及的に無効化しない。`LDL1903`は結果が一意に確定できない重大性をerrorで表し、OperationResultを`needs_review`にする。hostはUI表示上のfilterや強調を変えてよいが、protocolのcode/message_key/severityを変更してはならない。

### 10.2 順序と範囲

診断はsource origin順（project、次にPack StableSymbol）、normalized module path、start byte、end byte、severity順（error、warning、info）、code、subject StableAddress、owner StableAddress、`message_key`、RFC 8785 argumentsでsortする。optional sort fieldはabsentをpresentより前に置く。primary rangeは問題修正に必要な最小ソース rangeとし、related itemはrelationのenum順、range、subject、owner順でsortしてsemantic duplicateを禁止する。related `message`も非semanticである。

rangeはzero-based UTF-8 byteのhalf-open intervalとする。tokenを特定できない場合はnearest owner declaration header、それもなければmodule `[0,0)`を使う。

incomplete Working Documentのerror recoveryは追加診断を返してよいが、conformance fixtureとfully parsed documentのstable primary code/rangeは一致しなければならない。stable 診断だけをAPI compatibility保証へ含める。

## 11. セマンティック編集操作

本節は変更の意味とsource rewriteを定義し、Actorの権限を定義しない。Go Engineはoperation、scoped fragment、source patch、import等のbefore / afterから[authoring-access-control.md](authoring-access-control.md)のAuthoringImpactを生成する。operation名や`subject_kind`をAccess / Runtime / TSが独自にcapabilityへmapしてはならない。同じsemantic diffは入口にかかわらず同じAuthoringImpactを持つ。

### 11.1 バッチエンベロープ

```text
OperationBatch {
  document_id?: non-empty string
  base_revision: integer|non-empty string
  idempotency_key: string
  expected_subject_hashes: map<StableAddress,sha256>
  expected_subtree_hashes: map<StableAddress,sha256>
  expected_child_sets: ChildSetPrecondition[]
  operations: Operation[]
}

ChildSetPrecondition {
  owner_address: StableAddress
  child_kind: AddressableChildKind
  hash: sha256
}
```

`operations`は1件以上、`idempotency_key`は空でない1〜128 byteのUTF-8文字列とする。`expected_child_sets`は`(owner_address,child_kind)`で一意かつその順にsortする。routeがDocumentを一意にscopeしないtransportでは`document_id`を必須とする。

idempotency scopeはHost Document IDである。route scopeから省略された`document_id`はeffective Host Document IDをmaterializeしてからcanonical OperationBatch payload hashを計算する。同じDocumentと`idempotency_key`に対する再送はpayload hashが同じなら最初に確定したOperationResultを返し、operationを再適用しない。payload hashが異なれば`rejected`と`LDL1802`を返す。結果が`needs_review`のkeyはrecoveryで確定するまで新しいpayloadへ再利用してはならない。

operationは1つのtransactional Working Document overlay上で順番に実行する。後続operationは同batch内で先に作成されたsubjectのdeterministic StableAddressを参照できる。batch全体を1回検証し、source tree、reservation、move、normalized definition、semantic index、およびdefinition headを1つのCommitted Revisionとしてatomicに公開する。失敗時はそのdefinition revisionを公開しない。

provenance、freshness、audit、外部state snapshotはLDL definition transactionに含めない。これらは同じ`idempotency_key`と公開済みrevisionを記録し、失敗時はstateをstaleとして明示する。definitionとstateを同一物理transactionに置けるhostは同時commitしてよいが、Language 1は分散transactionを要求しない。identity moveに伴うstate address移行が未完了ならdefinitionは有効なままstate reconciliationを要求する。

### 11.2 閉じたoperation集合

各操作は必須フィールド`operation`を持つ閉じたタグ付きunionである。`operation`は正準wire discriminatorであり、値は`lower_snake_case`とする。未知の`operation`、各操作で未定義のフィールド、および必須フィールドの欠落はエラーとする。`create_subject`の作成対象種別は、discriminatorと衝突させず`subject_kind`で指定する。

```text
Operation = CreateSubjectOperation
  | UpdateSubjectFieldOperation
  | DeleteSubjectOperation
  | UpsertRowOperation
  | DeleteRowOperation
  | CreateRelationOperation
  | UpdateRelationEndpointOperation
  | RenameSubjectOperation
  | MigrateProjectIdentityOperation
  | MoveEntityToLayerOperation

PlacementHint {
  module_path?: normalized project-relative .ldl path
  group_anchor_address?: StableAddress
  position: enum(before,after,end)
}

CreateSubjectOperation {
  operation: "create_subject"
  parent_address: StableAddress
  subject_kind: enum(entity_type,relation_type,layer,entity,query,view,reference,entity_type_column,entity_type_constraint,relation_type_column,relation_type_constraint,query_parameter,view_table_column,view_export)
  id: identifier
  fields: CreateFields<subject_kind>
  placement?: PlacementHint
}

UpdateSubjectFieldOperation {
  operation: "update_subject_field"
  target_address: StableAddress
  path: non-empty string[]
  action: enum(set,remove)
  value?: normalized authored value
}

DeleteSubjectOperation { operation:"delete_subject", target_address:StableAddress }
UpsertRowOperation { operation:"upsert_row", owner_address:ref<entity|relation>, row_id:identifier, values:map<ref<column>,Scalar>, explicit_absent_column_addresses?:set<ref<column>>, placement?:PlacementHint }
DeleteRowOperation { operation:"delete_row", row_address:ref<entity-row|relation-row> }
CreateRelationOperation { operation:"create_relation", parent_address:ref<project>, id:identifier, type_address:ref<relation-type>, from_address:ref<entity>, to_address:ref<entity>, fields?:CreateFields<relation>, placement?:PlacementHint }
UpdateRelationEndpointOperation { operation:"update_relation_endpoint", relation_address:ref<relation>, endpoint:enum(from,to), entity_address:ref<entity> }
RenameSubjectOperation { operation:"rename_subject", target_address:StableAddress, new_id:identifier }
MigrateProjectIdentityOperation { operation:"migrate_project_identity", project_address:ref<project>, new_project_id:identifier }
MoveEntityToLayerOperation { operation:"move_entity_to_layer", entity_address:ref<entity>, layer_address:ref<layer> }
```

`CreateFields<K>`は5節のnormalized subject schemaから`id`、`address`、owner-scoped child collection、reservation、move、generated defaultを除いた、kind `K`のclosed authored field objectである。必須fieldとnested objectの必須memberは2節および言語本体のsource declaration schemaと同じで、symbol referenceはStableAddress、scalarはnormalized scalarを使う。sourceで既定値を持つmemberはoperation payloadでも省略でき、commit前に同じ既定値をmaterializeする。たとえばQueryで`traverse`自体はoptionalだが、指定した`traverse` objectは`direction`、`min_depth`、`max_depth`、`cycle_policy`を必要とする。Flow shapeは`relation_type_addresses`を必要とし、既定値を持つ`lane_by`、`cycle_policy`、`preserve_parallel`は省略できる。`entity`では`display_name`、`type_address`、`layer_address`、共通field、`relation`では省略可能な`display_name`と共通fieldを許可する。未知field、別kindのfield、PackOriginをparentとする作成、Project作成、Relationを`create_subject`で作ること、rowを`create_subject`で作ることは禁止する。

`upsert_row`はrow全体の置換/作成でありpartial patchではない。`explicit_absent_column_addresses`省略はempty setへ正規化する。`values`とexplicit absent setは互いに素で、owner typeのColumnだけを参照できる。どちらにもないColumnはunspecifiedとして2.2節のdefaultを適用し、explicit absentはdefaultを抑止する。default適用後にrequired/format/uniqueを検証する。既存rowの1 cellだけを変更する場合はrow StableAddressへの`update_subject_field`と`values/<column-address>` pathを使う。

EntityType `image`のoperation valueは5.2節のAssetRefである。対応bytesはoperation前に同じHost Document scopeのasset stagingへdigest検証付きで存在しなければならず、OperationBatchへraw bytesやhost pathを埋め込まない。runtimeはstandard `assets/` placementへcontent-addressedに保存し、対象moduleからのrelative source pathを生成する。staged bytes欠落またはdigest不一致はbatchをrejectする。

| `subject_kind` | 必須field | 省略可能field |
| --- | --- | --- |
| `entity_type` | `display_name`, `representation` | `icon`, `image`, `color`, 共通field |
| `relation_type` | `display_name`, `semantic_kind`, `from`, `to`, `forward_label` | `allow_self`, `duplicate_policy`, `cardinality`, `reverse_label`, `traversal`, `projections`, `render`, `export`, 共通field |
| `layer` | `display_name`, `order` | 共通field |
| `entity` | `display_name`, `type_address`, `layer_address` | 共通field |
| `relation`（`create_relation`専用） | なし | `display_name`, 共通field |
| `query` | `display_name`, `select` | `state_input`, `where`, `relation_where`, `traverse`, `result`, 共通field |
| `view` | `display_name`, `category`, `source`, `shape` | `intent`, `state_input`, `relation_projection_overrides`, 共通field |
| `reference` | `text` | なし |
| `entity_type_column`, `relation_type_column` | `display_name`, `value_type` | `enum_values`, `reserved_enum_values`, `required`, `default`, `format`, `min`, `max`, `min_length`, `max_length` |
| `entity_type_constraint`, `relation_type_constraint` | `column_addresses` | なし |
| `query_parameter` | `value_type` | `enum_values`, `reserved_enum_values`, `required`, `default`, `format`, `min`, `max`, `min_length`, `max_length` |
| `view_table_column` | `source` | `label`, `aggregate` |
| `view_export` | `format`, `filename`, `fidelity` | `source_refs`, `exporter_profile`, `options` |

共通fieldは`description`、`tags`、`annotations`である。省略した意味上のdefaultはnormalized modelでmaterializeする。`parent_address`はtop-level subjectではProject root、owner-scoped childでは許可されたowner kindを指し、`subject_kind`と一致しないownerはerrorとする。

ソースplacement hintはexisting module/groupまたはstandard placementを指定できるが意味とidentityへ影響しない。`group_anchor_address`はaddressable groupそのものではなく、目的group内の既存top-level/group itemまたはownerを指す。`before`/`after`はanchor必須でそのitemの前後、`end`はanchorがあればそのitemを含むgroup末尾、なければ`module_path`内のstandard kind group末尾を意味する。anchorと`module_path`を両方指定した場合は同じmoduleでなければならない。両方を省略した`end`またはplacement自体の省略はstandard layoutを使う。hintを満たせない場合は別fileへ黙って書かず`placement_changed`で失敗させる。

`update_subject_field`はregistered anonymousフィールド全体を置換する。`action set`は`value`必須、`remove`は`value`禁止で、省略可能フィールドだけremoveできる。positional list indexをoperation targetにしてはならない。

field-path registryはnormalized スキーマから導出し、次に閉じる。

| subject | 有効なtop-level path token |
| --- | --- |
| Project | `display_name`、`description`、`tags`、`annotations` |
| Layer | `display_name`、`order`、`description`、`tags`、`annotations` |
| EntityType | `display_name`、`icon`、`image`、`color`、`representation`、`description`、`tags`、`annotations` |
| EntityType/RelationType Column | `display_name`、`value_type`、`enum_values`、`reserved_enum_values`、`required`、`default`、`format`、`min`、`max`、`min_length`、`max_length` |
| Unique 制約 | `column_addresses` |
| Entity | `display_name`、`type_address`、`description`、`tags`、`annotations`。`layer_address`は`move_entity_to_layer`だけで変更する |
| RelationType | `display_name`、`semantic_kind`、`allow_self`、`duplicate_policy`、`from`、`to`、`cardinality`、`forward_label`、`reverse_label`、`traversal`、`projections`、`render`、`export`、共通フィールド |
| Relation | `display_name`、`type_address`、共通フィールド。`from_address`/`to_address`は`update_relation_endpoint`だけで変更する |
| Entity/Relation row | `values/<column-address>`だけ。row全体は`upsert_row`を使う |
| Query parameter | `display_name`を除くColumnスカラー/modifier token |
| Query | `display_name`、`state_input`、`select`、`where`、`relation_where`、`traverse`、`result`、共通フィールド |
| View | `display_name`、`category`、`intent`、`state_input`、`source`、`relation_projection_overrides`、`shape`、共通フィールド |
| View table Column | `label`、`source`、`aggregate` |
| View export | `format`、`filename`、`fidelity`、`source_refs`、`exporter_profile`、`options` |
| Reference | `text` |

`annotations`は2番目のfinal tokenにmap keyを取れる。row `values`は2番目のfinal tokenにresolved Column StableAddressを取り、`set`はpresent cellを作る。row cellへの`remove`はoptional Columnだけに有効で、sourceでは明示`_`をmaterializeしてColumn defaultを抑止する。row `values`全体のset/removeは禁止し、`upsert_row`を使う。その他mapはスキーマが明示しない限りcomplete フィールドとして置換する。addressable childはowner collection pathではなく自身のStableAddressで変更する。

### 11.3 事前条件と競合

batch開始時点で存在する直接targetごとに`expected_subject_hashes`のown-subject hashを要求する。addressable childを持つownerの`delete_subject`または`rename_subject`には、そのownerの`expected_subtree_hashes`も必須とし、子要素の同時作成・更新・削除を`subtree_changed`として検出する。新しいtop-level/child subject、Relation、rowの作成、および既存top-level/child subject、Relation、rowの削除またはrenameには、対応するownerとchild kindの`expected_child_sets`を要求する。`upsert_row`はrowが既存ならrow hash、新規ならownerの`row` child-set hashを要求する。同batch内で作成したsubjectを後続operationがtargetにする場合はprecondition不要である。

`rename_subject`はProjectOrigin subjectだけを対象とし、Project root自体は対象外である。runtimeはsemantic indexから、renameでauthored source bindingが書き換わる全project-local referencing subjectをread-setとして列挙する。clientはその各subjectのown-subject hashを`expected_subject_hashes`へ含めなければならず、欠落または不一致は`reference_broken`または`subject_changed`で拒否する。targetがownerなら全child address mappingもsubtree preconditionで保護する。これによりrenameと同時に行われた参照元編集を黙って上書きしない。

`migrate_project_identity`は全project-local addressを変更するためProject rootの`expected_subtree_hashes`を必須とし、base以後にproject内で1件でもsemantic changeがあれば`subtree_changed`として拒否する。

Project root配下のtop-level kindもchild-setとして扱う。たとえばEntity作成は`(Project root, entity)`、Relation作成は`(Project root, relation)`を比較する。child-set hashは9節の`child-set` domainを使う。newer base revisionでも全target preconditionが一致し、変更したfield/child-setが独立ならmergeできる。

`base_revision`はcurrent revisionと同じかcurrentのancestorでなければならない。ancestorでない、存在しない、または保持期限外で比較不能なbaseは`stale_revision`で拒否する。ancestorであるstale baseは上記全preconditionと依存検証が一致する場合だけcurrent headへmergeできる。

conflict classを次に固定する。

```text
stale_revision
subject_changed
subtree_changed
child_set_changed
same_field_changed
delete_vs_update
duplicate_identity
reference_broken
schema_row_incompatible
placement_changed
project_identity_changed
```

operation応答の意味を次に固定する。

```text
OperationResult {
  status: enum(committed,rejected,committed_state_stale,needs_review)
  revision?: integer|non-empty string
  definition_hash?: sha256
  diagnostics: Diagnostic[]
  conflicts: Conflict[]
}

Conflict {
  class: enum(stale_revision,subject_changed,subtree_changed,child_set_changed,same_field_changed,delete_vs_update,duplicate_identity,reference_broken,schema_row_incompatible,placement_changed,project_identity_changed)
  target_address?: StableAddress
  owner_address?: StableAddress
  child_kind?: AddressableChildKind
  path?: string[]
}
```

`committed`はdefinitionと要求されたstate effectが完了、`committed_state_stale`はdefinitionだけがcommitされ`LDL1902`とstate reconciliationを伴う。`rejected`はdefinitionを公開せず、validation diagnosticまたは上記conflict classを返す。`needs_review`はwrite-ahead recordから公開状態を一意に判定できない場合だけ使い`LDL1903`を返す。`committed_state_stale`と`needs_review`はLDLソースの無効を意味せず、state protocol statusとして診断へ接続する。

`committed`/`committed_state_stale`は`revision`と`definition_hash`を必須、`conflicts`をemptyとする。`rejected`/`needs_review`は確定済みrevision/hashを返してはならない。`rejected`はdiagnosticsまたはconflictsを1件以上、`needs_review`は`LDL1903`を必ず持つ。conflictsはclassのenum記載順、target、owner、child kind、path順でsortしsemantic duplicateを禁止する。diagnosticsは10.2節順とする。

同一subjectの異なるregistered フィールド pathは、相互検証へ影響しない場合だけmergeできる。スキーマ/row editは同じColumn、required/既定値、type、enum set、unique 制約、row cellへ触れる場合にdependentとする。

### 11.4 Host-scoped operation preview

Host Documentのcurrent headに対してcommitせずoperationを検証するpreviewは次の独立envelopeを使う。`OperationBatch`のidempotency/write契約を流用しない。

```text
HostOperationPreviewInput {
  document_id?: non-empty string
  base_revision: integer|non-empty string
  expected_subject_hashes: map<StableAddress,sha256>
  expected_subtree_hashes: map<StableAddress,sha256>
  expected_child_sets: ChildSetPrecondition[]
  operations: Operation[]
}

HostOperationPreviewResult {
  status: enum(valid,invalid)
  base_revision: integer|non-empty string
  evaluated_revision: integer|non-empty string
  preview_definition_hash?: sha256
  diagnostics: Diagnostic[]
  conflicts: Conflict[]
}
```

routeがDocumentを一意にscopeしないtransportでは`document_id`を必須とする。`operations`は1件以上で、map/set/orderとprecondition requirementは11.1節および11.3節を適用する。runtimeは同じHost Documentのcurrent headを`evaluated_revision`として1件へ固定し、base revision/全preconditionを検査してから、そのhead上の1つのWorking Document overlayへoperationを順番に適用する。`valid`はpreview definition hashを必須とし、error diagnostic/conflictを禁止する。`invalid`はhashを返さず1件以上のdiagnosticまたはconflictを持つ。resultの`base_revision`は入力値、`evaluated_revision`は固定したheadと一致しなければならない。どちらもrevision、idempotency record、state effect、audit、storage writeを生成しない。

### 11.5 Portable operation preview

Host Documentを開かずprovided source treeへoperationを試行するpreviewは、`OperationBatch`ではなく次の独立envelopeを使う。

```text
PortableDefinitionInput {
  entry: normalized project-relative .ldl path
  files: map<normalized portable-tree-relative path,bytes>
  input_digest: "sha256:<lower-hex>"
}

PortableOperationPreviewInput {
  definition: PortableDefinitionInput
  expected_definition_hash?: sha256
  operations: Operation[]
}

PortableOperationPreviewResult {
  status: enum(valid,invalid)
  definition_hash?: sha256
  definition?: PortableDefinitionInput
  diagnostics: Diagnostic[]
  conflicts: Conflict[]
}
```

portable tree rootは`.layerdraw`を展開した時のcontainer rootと同じ論理rootである。`files` keyはそのrootからの相対pathで、Project source、`layerdraw.resolved.json`、`pack/<install-name>/...`の解決済みPack source、`assets/...`の検証対象bytesを同じmapに保持できる。3.1節と3.5節のpath安全規則を適用し、manifest自体の有無にかかわらずpreview中のfilesystem/network fallbackを禁止する。`entry`はこのmap内のProject `.ldl` fileを指す。`input_digest`はpath順の`{path,raw_sha256}` arrayをRFC 8785でserializeしたbytesのraw SHA-256で、各`raw_sha256`はfile bytesのraw SHA-256とする。transportは`bytes`をbinary、base64、またはresource attachmentで運んでよいが、Go Engineへ渡すbyte列とdigestは同一でなければならない。

`operations`は1件以上で、supplied definition上の1つのWorking Document overlayへ順番に適用する。Host Document ID、base revision、idempotency key、actor、state、audit、および11.3節のconcurrency preconditionを入力にしてはならない。`expected_definition_hash`がpresentなら入力をcompileしたdefinition hashと一致しなければ`invalid`と`LDL1801`を返す。previewはsupplied bytes自体を競合の基準とするため、host headとのmergeや同時実行安全性を主張しない。

`valid`はvalidation済みcanonical source treeとdefinition hashを必須とし、diagnosticsにerror、conflictsを含めない。返却`definition.input_digest`は変換後bytesから再計算する。delete/renameに伴うreservation/moveは返却source tree内だけへmaterializeする。`invalid`はdefinition/hashを返さず、1件以上のdiagnosticまたはconflictを持つ。どちらもCommitted Revision、OperationResult、state effect、audit、storage writeなどの外部副作用を生成しない。preview結果を保存するcallerはHost Documentを開き、current revision/preconditionを取得して新しい`OperationBatch`としてapplyし直さなければならない。

### 11.6 Identityの副作用

committed top-level subject削除はroot reservationへIDを追加する。committed child削除はソース languageが定義するnearest surviving ownerのchild reservationへ追加する。owner削除時のchild tombstoneはLDLではなく状態/auditへ置く。

`rename_subject`はdurable moveを1つ書き、selected project-localソースbindingをすべて更新し、owner-child subtreeのold/new address mappingをdefinition revisionへ生成する。外部state referenceは同じidempotency keyで移行し、完了できなければ`committed_state_stale`とする。`migrate_project_identity`は全project-local addressに同じ処理を行う。pack-origin subjectは不変とする。

## 12. Language 1凍結条件

次のartifactが2つの規範文書と一致した時点でLanguage 1を仕様完成とする。

1. declaration スキーマ registry
2. separately owned machine-readable normalized スキーマ version 1
3. resolved dependency スキーマ version 1
4. セマンティック index スキーマ version 1
5. 診断 code registry
6. セマンティック操作 スキーマ
7. valid/無効 ソース fixture
8. 正規化JSONのgolden fixture
9. Query/ViewDataのgolden fixture
10. formatter冪等性fixture
11. ハッシュ/move migration fixture
12. AssetRef/asset manifest schemaと安全性fixture
13. ExportPlan/Source Manifest schema
14. builtin exporter-profile registryとprofile specification digest fixture
15. shape×format×fidelityおよびJSON/YAML/CSV/TSV/XLSX export golden fixture
16. OperationBatch/OperationResult/idempotency/conflict fixture

これらは実装言語、プロセス モデル、transport、実行可能 パッケージング、配布 チャネルから独立した言語artifactである。

2のmachine-readable normalizedスキーマはLanguage 1 freezeの独立したlanguage-contract deliverableであり、Engine Protocol schema closureやTypeScript transport clientへ手書きdomain型を追加する作業ではない。portable-compilation publicationがversioned opaque blobであることと、このfreeze artifactを別途完成させる義務は両立する。

## 13. 規範参照

- RFC 3986, URI Generic Syntax: <https://www.rfc-editor.org/rfc/rfc3986>
- RFC 5322, Internet Message Format: <https://www.rfc-editor.org/rfc/rfc5322>
- RFC 1034 / RFC 1035, Domain Names: <https://www.rfc-editor.org/rfc/rfc1034>, <https://www.rfc-editor.org/rfc/rfc1035>
- RFC 1123, Internet Host Requirements: <https://www.rfc-editor.org/rfc/rfc1123>
- RFC 4291, IPv6 Addressing Architecture: <https://www.rfc-editor.org/rfc/rfc4291>
- RFC 5952, IPv6 Text Representation: <https://www.rfc-editor.org/rfc/rfc5952>
- RFC 3339, Date and Time on the Internet: <https://www.rfc-editor.org/rfc/rfc3339>
- RFC 8785, JSON Canonicalization Scheme: <https://www.rfc-editor.org/rfc/rfc8785>
- Semantic Versioning 2.0.0: <https://semver.org/spec/v2.0.0.html>
- YAML 1.2.2: <https://yaml.org/spec/1.2.2/>
