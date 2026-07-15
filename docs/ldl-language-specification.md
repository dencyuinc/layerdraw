# LayerDraw 言語仕様

status: 規範的設計仕様

この文書は、正準な LayerDraw Language (`.ldl`) のソース構文、宣言モデル、モジュール解決、アイデンティティ、およびオーサリング境界を定義する。[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md) は、閉じた宣言デフォルト、正規化データ、Query と ViewData の評価、フォーマット詳細、ハッシュ、診断、およびセマンティック操作を定義する。これらを合わせて、完全な規範的 Language 1 仕様となる。関連する製品文書およびアーキテクチャ文書は参考情報であり、言語を再定義してはならない (MUST NOT)。

Language `1` は、ここで定義するコンパクト構文のみを使用する。以前の HCL 風の `name = value`、単数形の `entity`、単数形の `relation`、および冗長な述語ブロック形式はレガシー入力である。これらは代替の正準構文ではない。

## 1. 規範用語

キーワード **MUST**、**MUST NOT**、**SHOULD**、**SHOULD NOT**、および **MAY** は規範的要件である。

- **source file**: 1 つの UTF-8 `.ldl` ファイル。
- **module**: プロジェクトまたはインストール済みパックの origin から解決される 1 つのソースファイル。
- **entry module**: コンパイラに渡されるモジュール。通常は `document.ldl`。
- **project origin**: entry module とプロジェクトローカルモジュールを含むソースルート。
- **pack origin**: `pack/<install-name>/` 配下にある、1 つの不変なインストール済みパック。
- **effective document**: entry module の明示的な import/export クロージャとそのセマンティック依存関係を通じて選択される宣言。
- **Master Graph**: 検証済みの EntityType、RelationType、Layer、Entity、Relation、および属性行ファクト。
- **Working Document**: 不完全または無効であり得る、一時的な共同編集ソース状態。
- **Committed Revision**: 完全な検証に合格した、不変のソースツリーリビジョン。
- **state**: LDL の外部に保存される、生成された provenance、freshness、actor、revision、および backend データ。
- **StateQuerySnapshot**: 1回のQuery/View評価に固定される、current system/provenance stateの型付き不変projection。backend設定、credential、audit event、lock、lease、presenceを含まない。

## 2. 言語境界

LDL は、型付き有向グラフ、二次元属性テーブル、保存済み Query レシピ、型付き View レシピ、および自然言語 Reference のための宣言型言語である。

FTS / Vector / HybridによるProject Searchは探索起点を発見する派生operationであり、LDL Query構文ではない。SearchHitを採用する場合は確認済みStableAddressを`select roots`へ明示するか、型付きpredicateへ一般化して保存する。PageRank、K-Core、Louvain、SCC、WCCのAnalysisResultも派生結果であり、LDL属性またはView selectionへ暗黙保存しない。意味分離は[search-query-and-analysis.md](search-query-and-analysis.md)を規範とする。

LDL は次を宣言する:

- Project メタデータ。
- EntityType および RelationType スキーマ。
- Layer。
- Entity および Relation ファクト。
- オーサリングされた安定行 ID を持つ Entity および Relation の属性行。
- 保存済み Query レシピ。
- 保存済み View およびプレーンエクスポートレシピ。
- Query/Viewが外部StateQuerySnapshotを必要とするか、および参照する標準state field path。
- Reference テキスト。
- import、export、アイデンティティ予約、および永続的 move。

LDL は次ではない:

- 汎用プログラミング言語。
- グラフデータベースクエリ言語。
- 描画座標のマスターフォーマット。
- バイナリアセットコンテナ。
- state snapshot、revision、audit、lock、または presence の保存フォーマット。
- credential または backend-binding のフォーマット。
- 実行可能拡張フォーマット。
- temporal graphまたはevent-time query言語。valid-time / transaction-time、時点指定、期間・window traversal、temporal relation / cardinality、bitemporal modelを定義しない。
- raw Cypher、Ladybug procedure、embedding vector、Search Index、SearchHit scoreを正本として記述する言語。

date / datetimeは通常のscalar型であり、typed predicateによる値比較に使える。ユーザー定義の`environment`、`scenario`、`phase`、日時column、tagにもCore固有の時間意味論を付与しない。revision、Audit、Time Machine、current StateQuerySnapshotはHost / Runtimeの履歴・状態境界であり、Master Graphの時間dimensionではない。

### 2.1 正準オーサリング制約

正準構文は、同時に適用される次の 3 つの要件に最適化されている:

1. 人間が生成ツールなしでレビューおよび編集できること。
2. 反復されるグラフデータが、実用上できる限り少ないテキストとモデルコンテキストを消費すること。
3. エディタまたはエージェントが、プロジェクト全体を読み込まずに、境界付けられたセマンティックスコープを発見、読み取り、パッチできること。

したがって LDL は、コンテキストが直ちに見えるままである場所でのみ反復コンテキストを除去する。EntityType と Layer は `entities` ヘッダーに置かれ、RelationType は `relations` ヘッダーに置かれ、行カラムは位置指定ヘッダーに置かれる。Stable ID、Relation の方向、row ID、型宣言、およびモジュール依存関係は、決して推論または省略されない。

マクロ、テキスト include、オーサリングされたファクトを変更する暗黙デフォルト、ディレクトリ全体 import、ワイルドカードソース発見、グローバル alias、およびコンテキスト依存の省略モードは禁止される。これらは局所的にはバイト数を削減し得るが、部分読み取り、決定論的編集、およびレビューを安全でなくする。コンパクトグループはセマンティック構文木ノードであり、テキスト置換ではない。

ソースツリー、明示的なモジュールクロージャ、生成されたセマンティックインデックス、StableAddress、およびバイト範囲は、部分アクセス契約を形成する。ファイルレイアウトは可能性の高いスコープを狭めるが、解決済み宣言だけが意味を決定する。

次の値は LDL に保存してはならない (MUST NOT)。Query/View recipe内の規範的なstate field path参照は値ではないため許可される。

- `created_at`、`updated_at`、`created_by`、または `updated_by`。
- observation、verification、confidence、source URI、または field ownership stateの値。
- audit event、operation log、lock、lease、または collaboration presence。
- backend credential または binding。
- 生成された ViewData、RenderData、index、preview、または export artifact。
- バイナリ画像データ。
- JavaScript、Go、WebAssembly、shell command、callback、または environment-variable read。

## 3. Project、Pack、および生成レイアウト

### 3.1 標準プロジェクトレイアウト

LayerDraw が提供する GUI、MCP、SDK、およびプロジェクトジェネレータは、新規プロジェクトをこのレイアウトで作成しなければならない (MUST):

```text
retail_platform/
  document.ldl
  schema/
    entity_types/
      application.ldl
      network.ldl
    relation_types/
      containment.ldl
      deployment.ldl
  layers/
    layers.ldl
    application/
      application_services.ldl
      databases.ldl
    network/
      vpcs.ldl
      subnets.ldl
  views/
    production_topology.ldl
  references/
    operating_rules.ldl
  pack/
  assets/
  layerdraw.resolved.json
  project.ldstate.json
  project.ldbackend.json
  layerdraw.index.json
```

標準所有ルール:

- プロジェクトローカル EntityType は `schema/entity_types/` 配下に属する。
- プロジェクトローカル RelationType は `schema/relation_types/` 配下に属する。
- Layer 宣言は `layers/layers.ldl` に属する。
- Entity は `layers/<its-layer>/` 配下に属する。
- Relation は、その `from` Entity と同じソースシャード、またはその Entity の Layer 配下にある別のシャードに属する。
- Entity 行は、その owner Entity と同じモジュールで宣言しなければならない (MUST)。
- Relation 行は、その owner Relation と同じモジュールで宣言しなければならない (MUST)。
- 同一 Layer およびクロス Layer の Relation は同じルールを使用する。
- プロジェクトローカル Query および View は `views/` 配下に属する。
- プロジェクトローカル Reference は `references/` 配下に属する。

このレイアウトはオーサリング規約であり、言語セマンティクスではない。プロジェクトは、owner/row の同一場所配置を保持する限り、1 ファイルまたは任意の非循環モジュール分割を使用してよい (MAY)。ソース位置は StableSymbol identity または正規化された意味に影響してはならない (MUST NOT)。既存のカスタムレイアウトは、明示的な workspace organization まで変更しないままでなければならない (MUST)。

ディレクトリの存在は inclusion を意味しない。entry module は、明示的な import と export を通じて、必要なすべてのモジュールを選択しなければならない (MUST)。コンパイラはすべての `.ldl` ファイルを glob してはならない (MUST NOT)。

### 3.2 Relationのソース所有

すべての Relation は有向の `from -> to` ファクトである。`from` endpoint は標準ソース所有だけを決定する。これはグラフセマンティクスを追加しない。

incoming lookup と cross-Layer classification は、セマンティックインデックスを通じて、解決済み endpoint から導出される。誤配置されているがそれ以外は有効な Relation も、なお parse、validate、query、render、および export されなければならない (MUST)。

### 3.3 `.layerdraw` コンテナ

`.layerdraw` は、同じ論理プロジェクトツリーを保持する ZIP コンテナである:

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

ソースディレクトリと `layerdraw.index.json` は、使用される場合に存在する。合法な単一ファイルプロジェクトはソースディレクトリを省略してよい (MAY)。`document.json` と `layerdraw.index.json` は生成物であり、編集可能ソースになってはならない (MUST NOT)。

`manifest.json`は詳細仕様の`LayerdrawManifest`に従い、entry、portable Project StableAddress、definition hash、resolved metadata digest、manifest自身を除く全file entry digestを固定する。Host Document ID、actor、credential、storage bindingはportable manifestへ入れない。

state依存のmaterialized ViewDataまたはplain exportを同梱するpackageは、それらが参照するcomplete StateQuerySnapshotを`state/query-snapshots/<snapshot-hash>.json`へ同梱する。plain export単体のSource Manifestは出自を束縛するが、snapshot本体なしにMaster GraphからViewDataを再materializeできるとは主張しない。package redactionはmanifestへ明示し、snapshot hashとそれを参照する全派生成果物を再計算する。

### 3.4 `.ldpack`成果物

`.ldpack` は ZIP 配布およびインストール artifact である。インストールは次を行わなければならない (MUST):

1. manifest、checksum、signature policy、および compatibility を検証する。
2. path traversal、absolute path、および escaping symlink を拒否する。
3. `pack/<install-name>/` にアトミックに展開する。
4. 依存関係をその隣の `pack/` 配下で解決する。
5. 正確な version と digest を `layerdraw.resolved.json` に書き込む。
6. 展開済みコンテンツを不変として扱う。
7. pack content を決して実行しない。

```text
aws_complete_1.4.0.ldpack
  -> pack/aws_complete/
       manifest.json
       pack.ldl
       modules/
       assets/
       references/
```

open、compile、query、render、および export は Registry に接続してはならない (MUST NOT)。ネットワークアクセスは、明示的な install、update、または repair の間にのみ許可される。

Pack 宣言 kind は詳細仕様によって閉じている。Pack は再利用可能な schema、Query/View レシピ、および guidance を含む。プロジェクト Layer と Entity/Relation ファクトは、プロジェクトまたは template に属する。

Pack単体compile/publishはProject document envelopeを偽造せず、詳細仕様の`NormalizedPackArtifact`を生成する。Pack Query/Viewはconsumer Master Graphなしで静的検証され、QueryResult/ViewDataはimport後にだけ生成される。

最小 manifest は次のとおりである:

```json
{
  "format": "layerdraw-pack",
  "format_version": 1,
  "id": "layerdraw/aws-complete",
  "name": "aws_complete",
  "version": "1.4.0",
  "language": 1,
  "entry": "pack.ldl",
  "dependencies": {}
}
```

Version と digest は解決メタデータであり、LDL import text では決してない。

`dependencies`の値は詳細仕様の`PackDependency { id, version }`で、versionはexact SemVerである。Pack sourceはdependency-local nameをimportし、Project install nameやRegistry URLを記述しない。range/tag/latestはLanguage 1 manifestでは使わず、Pack updateはmanifestとresolved treeを明示的に更新する。

命名は境界ごとに意図的に分割される:

- Registry pack ID は、小文字 ASCII kebab-case セグメントによる `<publisher>/<pack-name>` を使用する。
- manifest `id` は正準 PackOrigin authority である。別個の `publisher` フィールドが存在する場合、それは最初のセグメントと等しくなければならない (MUST)。
- archive filename は `<manifest-name>_<version>.ldpack` を使用するべきであり (SHOULD)、セマンティック identity ではない。したがって例は`aws_complete_1.4.0.ldpack`となる。
- manifest `name`、project install name、LDL namespace alias、module path segment、および source basename は小文字 ASCII snake_case を使用し、LDL identifier grammar に一致しなければならない (MUST)。
- install name はデフォルトで manifest `name` になるが、2 つの名前が衝突する場合は明示的な依存関係 mapping により置き換えてよい (MAY)。
- install name と alias は version を含んではならない (MUST NOT)。

### 3.5 セマンティックインデックス

`layerdraw.index.json` は生成され、置換可能であり、権威的ではない。これは高速 open のためにパッケージしてよい (MAY) が、source と resolved pack から再生成可能でなければならない (MUST)。

これは次を index する:

- source file と byte digest。
- StableAddress、owner address、module、および byte range ごとのすべての StableSymbol。
- local/export/import binding を semantic identity とは別に。
- EntityType / RelationType column。
- Entity および Relation row ownership。
- Layer および type membership。
- incoming および outgoing adjacency。
- Query schema dependency および View source dependency。
- Reference ID。
- own-subject および subtree semantic hash。

MCP、language server、および editor は、`list_modules`、`find_symbols`、`read_declarations`、`read_rows`、`get_neighbors`、`find_usages`、`read_scope`、および `apply_operations` と同等の scoped operation を公開するべきである (SHOULD)。部分アクセスは、全ドキュメントアクセスと同じセマンティクスを生成しなければならない (MUST)。

## 4. 字句文法

### 4.1 エンコーディングと空白

- Source は UTF-8 でなければならない (MUST)。
- BOM は受け入れてよい (MAY) が、フォーマットによって除去されなければならない (MUST)。
- 正準の行末は LF である。
- 正準のインデントは 2 スペースである。
- 改行は line statement を終了する。
- セミコロンは禁止される。
- 文字列外のタブは受け入れられるが、スペースにフォーマットされる。
- brace はネストした block を区切る。インデントにはセマンティックな意味はない。

### 4.2 コメントとドキュメント

```ldl
// 通常コメント
/// 次の宣言に付与されるドキュメント
//! モジュールドキュメント
```

- `//` は改行で終了する。
- `///` は、介在する宣言がない場合、次の declaration または group item に付与される。
- `//!` は module を文書化し、source file先頭の連続sectionとしてimportの前に現れる。
- Comment は lossless syntax tree と formatter output に保持される。
- Comment は semantic hash に影響しない。

### 4.3 識別子と参照

正準 identifier は次に一致する:

```text
[a-z][a-z0-9_]*
```

ユーザー向けテキストと大文字小文字を区別する外部値は文字列に属する。Declaration ID、row ID、constraint ID、Query parameter、View-local ID、alias、および bare enum atom は正準 identifier を使用する。Imported symbol reference は、正確に 1 つの namespace qualifier を使用する:

```ldl
application_service
aws.subnet
```

module/declaration control word である `import`、`from`、`as`、`export`、`project`、`layers`、`entity_type`、`relation_type`、`entities`、`rows`、`relations`、`relation_rows`、`query`、`view`、`reference`、`reserved`、`moves`、`true`、`false`、および `null` は、declaration ID および import alias として禁止される。他の field/operator word はコンテキスト依存であり、declaration-specific grammar が ID を期待する場所では ID であってよい (MAY)。衝突する enum string は常に quote できる。

ID は source path または version をエンコードしてはならない (MUST NOT)。ドット付き declaration ID は禁止される。`aws.subnet` は常に alias-qualified source binding であり、1 つの ID では決してない。

### 4.4 リテラル

```ldl
"UTF-8 string"
42
-2
3.14
true
false
prod
[prod, stg, "legacy-prod"]
{ owner: "platform", critical: true }
0..1
0..*
_
```

- String は JSON 互換の double quote を使用する。
- String interpolation は禁止される。
- Number は有限 decimal である。exponent notation は禁止される。
- Bare atom は、schema-known position における identifier 形状の enum または string value である。
- List は comma を使用し、multiline の場合は 1 つの trailing comma を許可する。
- Object は `key: value`、comma、および unique key を使用する。
- `_` は省略された optional table cell を意味し、`rows` および `relation_rows` value でのみ有効である。
- Cardinality および traversal range は `min..max` を使用し、`max` は integer、または明示的に許可される場合は `*` である。

Heredoc は plain text のみを含む:

```ldl
<<-TEXT
  補間または実行可能ディレクティブはない。
TEXT
```

closing marker は author が選ぶ identifier である。`<<-` は nonblank content line の共通 indentation を除去する。

### 4.5 正準句読点

正準 compact syntax は次を使用する:

- declaration field で assignment `=` を使用しない。
- typed predicate でのみ `==`、`!=`、`<`、`<=`、`>`、`>=` を使用する。
- object key と value の間、row owner/ID prefix と positional cell value の間、および Relation ID と endpoint の間に `:` を使用する。
- authored Relation direction と、`moves` 内の明示的な old-to-new entry に `->` を使用する。
- Layer/order placement header にのみ `@` を使用する。
- decimal/range literal の外では、1 つの import alias と 1 つの imported symbol の間、または `attribute.<column>`、`system.updated_at`、`provenance.source.kind`などgrammar-defined selectorの中でのみ`.`を使用する。
- Query parameter source binding の前でのみ `$` を使用する。
- list、object、および positional table row の中でのみ comma を使用する。

### 4.6 言語境界をまたぐ命名

Language 1 は、すべての public boundary における命名を定義する:

| 境界 | 正準規約 | 例 |
| --- | --- | --- |
| LDLキーワード、フィールド、作成ID、別名、enum atom | 小文字ASCIIのsnake_case | `relation_type`, `order_api`, `left_to_right` |
| Projectモジュールパス、ソースbasename、Packインストール名 | 小文字ASCIIのsnake_case | `application_services.ldl`, `aws_complete` |
| 正準JSON、state、manifest、HTTP、MCP、および操作のフィールド名 | 小文字ASCIIのsnake_case | `project_address`, `base_revision` |
| 通信形式のdiscriminatorおよび操作種別 | 小文字ASCIIのsnake_case | `entity_type`, `create_subject` |
| RegistryのpublisherおよびPack IDセグメント | 小文字ASCIIのkebab-case | `layerdraw/aws-complete` |
| StableAddressの種別セグメント | 小文字ASCIIのkebab-case | `entity-type`, `table-column` |
| 安定診断コード | 大文字`LDL`と4桁の数字 | `LDL1503` |
| 仕様書専用の抽象型名 | UpperCamelCase | `NormalizedDocument`, `StableAddress` |
| ユーザー向け表示文とスカラー文字列 | 有効なUTF-8を制限なく使用 | `"受注管理API"` |

UpperCamelCase abstract type name は表記であり、正準 serialized field name または discriminator として現れてはならない (MUST NOT)。Schema-defined wire name は `[a-z][a-z0-9]*(?:_[a-z0-9]+)*` に一致する。StableAddress、annotation key、external ID、filename、またはその他の user/external value を明示的に表す schema の key は data であり、このルールによって書き換えられない。

Language binding は TypeScript `applyOperations` や Go `ApplyOperations` などの host-idiomatic name を公開してよい (MAY) が、生成された binding adapter は、セマンティクスを変更せずにそれらを canonical wire `apply_operations` に map しなければならない (MUST)。Host-language naming は、source、normalized JSON、state、manifest、hash、MCP payload、または HTTP payload には決して入らない。

## 5. 構造文法

parser は semantic validation の前に lossless concrete syntax tree を構築しなければならない (MUST)。この EBNF は正準構造を定義する。declaration-specific field validation は後で定義する。

```ebnf
file                = { trivia | module-doc },
                      { trivia | import-decl },
                      { trivia | doc-comment | declaration } ;

import-decl         = namespace-import | named-import ;
namespace-import    = "import", identifier, "from", string ;
named-import        = "import", "{", import-items, "}", "from", string ;
import-items        = import-item, { ",", import-item }, [ "," ] ;
import-item         = identifier, [ "as", identifier ] ;

declaration         = project-decl | layers-decl | entity-type-decl
                    | relation-type-decl | entities-decl | rows-decl
                    | relations-decl | relation-rows-decl | query-decl
                    | view-decl | reference-decl | reserved-decl
                    | moves-decl | export-decl ;

project-decl        = "project", identifier, string, block ;
layers-decl         = "layers", layer-items-block ;
layer-item          = identifier, string, "@", integer, [ block ] ;
entity-type-decl    = "entity_type", identifier, string, block ;
relation-type-decl  = "relation_type", identifier, string, identifier, block ;

entities-decl       = "entities", symbol-ref, "@", symbol-ref,
                      entity-items-block ;
entity-item         = identifier, string, [ block ] ;
rows-decl           = "rows", symbol-ref, column-header, row-items-block ;
row-item            = identifier, identifier, ":", cells ;

relations-decl      = "relations", symbol-ref, relation-items-block ;
relation-item       = identifier, ":", symbol-ref, "->", symbol-ref,
                      [ string ], [ block ] ;
relation-rows-decl  = "relation_rows", symbol-ref, column-header,
                      row-items-block ;

query-decl          = "query", identifier, string, block ;
view-decl           = "view", identifier, string, identifier, block ;
reference-decl      = "reference", identifier, heredoc ;
reserved-decl       = "reserved", block ;
moves-decl          = "moves", move-items-block ;

move-items-block    = empty-block | "{", newline,
                      { ( trivia | top-move-item | child-move-item ), newline }, "}" ;
top-move-item       = top-move-kind, identifier, "->", identifier ;
child-move-item     = child-move-kind, identifier, identifier,
                      "->", identifier ;
top-move-kind       = "project" | "entity_type" | "relation_type"
                    | "layer" | "entity" | "relation" | "query"
                    | "view" | "reference" ;
child-move-kind     = "entity_type_column" | "entity_type_constraint"
                    | "relation_type_column" | "relation_type_constraint"
                    | "entity_row" | "relation_row" | "query_parameter"
                    | "view_table_column" | "view_export" ;

export-decl         = local-export | re-export-all | re-export-named ;
local-export        = "export", "{", export-items, "}" ;
re-export-all       = "export", "*", "from", string ;
re-export-named     = "export", "{", export-items, "}", "from", string ;
export-items        = export-item, { ",", export-item }, [ "," ] ;
export-item         = identifier, [ "as", identifier ] ;

layer-items-block   = empty-block | "{", newline,
                      { ( trivia | doc-comment | layer-item ), newline }, "}" ;
entity-items-block  = empty-block | "{", newline,
                      { ( trivia | doc-comment | entity-item ), newline }, "}" ;
row-items-block     = empty-block | "{", newline,
                      { ( trivia | row-item ), newline }, "}" ;
relation-items-block = empty-block | "{", newline,
                       { ( trivia | doc-comment | relation-item ), newline }, "}" ;

column-header       = "[", symbol-ref, { ",", symbol-ref }, [ "," ], "]" ;
cells               = cell, { ",", cell } ;
cell                = value | "_" ;
symbol-ref          = identifier, [ ".", identifier ] ;
qualified-token     = identifier, { ".", identifier } ;
value               = string | heredoc | integer | number | boolean
                    | parameter-ref | qualified-token | list | object | range ;
parameter-ref       = "$", identifier ;
list                = "[", [ value, { ",", value }, [ "," ] ], "]" ;
object              = "{", [ object-item, { ",", object-item }, [ "," ] ], "}" ;
object-item         = object-key, ":", value ;
object-key          = identifier | string ;
range               = integer, "..", ( integer | "*" ) ;
statement-arg       = value | column-header | predicate-operator | "->" ;
statement           = identifier, { statement-arg }, newline ;
nested-block        = identifier, { statement-arg }, block ;
block               = empty-block | "{", newline,
                      { trivia | doc-comment | statement | nested-block }, "}" ;
empty-block         = "{", "}" ;
predicate-operator  = "==" | "!=" | "<" | "<=" | ">" | ">=" ;

boolean             = "true" | "false" ;
```

`identifier`、`string`、`integer`、`number`、`heredoc`、`newline`、`trivia`、`module-doc`、および `doc-comment` は section 4 で定義される lexical production である。同じ identifier-shaped token は、schema-defined grammar position に基づいて、declaration reference、enum value、または string-like atom として解決される。semantic validation は、複数の expected type の間を決して推測しない。Declaration-specific section は、`contains`、`in`、`exists`、および `missing` などの word operator を含め、汎用 `statement` および `nested-block` production を制約する。

汎用 `statement` の引数位置で末尾の `{}` が `object` と `nested-block` の両方に一致する場合、`nested-block` の `empty-block` を優先する。したがって `select {}` や `source query q {}` は空の nested block であり、同位置の直接の空 object 引数は表現しない。空 object は、list 要素や object item value など block と曖昧でない value 位置では引き続き有効である。

`qualified-token`は汎用statement/value位置の構文上の受理単位である。通常のdeclaration source bindingは`symbol-ref`どおり最大2 segmentに制限し、3 segment以上を許すのは詳細仕様5.4節のexact StateFieldPathなど、declaration-specific grammarが明示したselectorだけである。未知のmulti-segment tokenを任意property pathとして受理してはならない。

LDL source fileはversion headerを要求しない。BOMを除いた最初のnon-trivia tokenは、module documentation、import、またはdeclarationでよい。`//!` module documentationはsource file先頭の連続sectionとしてだけ許可し、importまたはdeclarationより後に現れてはならない。

Language 1 は代替の HCL 風 assignment syntax を受け入れない。Compilerはsource内の世代宣言ではなく、自身が実装するLDL generation、release manifest、`.layerdraw`/`.ldpack` manifest、およびmigrator capabilityで互換性を判断する。

## 6. モジュールシステム

### 6.1 モジュール識別子

LDL には `module` 宣言がない。module identity は source origin と正規化された relative path から導出される。一方、宣言の semantic identity は StableSymbol 規則に従う `origin + kind + authored ID` であり、module path は寄与しない。

```text
document.ldl                                  -> project entry module
schema/entity_types/application.ldl           -> project module
layers/application/application_services.ldl   -> project module
pack/aws_complete/pack.ldl                    -> pack entry module
pack/aws_complete/modules/network.ldl         -> pack module
```

プロジェクトローカル宣言をモジュール間で移動しても、その StableAddress は変わらない。

### 6.2 インポート

名前空間インポート:

```ldl
import aws from "aws_complete"
```

名前付きインポート:

```ldl
import { vpc, subnet as private_subnet_type } from "aws_complete.network"
import { application_service } from "./schema/entity_types/application.ldl"
```

ルール:

- namespace import は、解決された module が export するすべての symbol を宣言された alias 配下に bind する。
- named import は、列挙された exported symbol だけを bind する。
- relative module specifier は `./` または `../` で始まり、`.ldl` で終わり、current origin の内部に留まる。
- `"aws_complete"` などの pack root specifier は `pack/aws_complete/pack.ldl` にのみ解決される。
- `"aws_complete.network"` などの pack module specifier は `pack/aws_complete/modules/network.ldl` にのみ解決される。
- 追加の dot segment はそれぞれ `modules/` 配下の 1 つの directory segment に map される。したがって `"aws_complete.network.vpc"` は `pack/aws_complete/modules/network/vpc.ldl` に map される。
- すべてのpack specifier segmentはLDL identifierである。ProjectOriginのmoduleでは最初のsegmentをproject-local install nameとして解決する。PackOriginのmoduleでは、最初のsegmentをそのPack自身のmanifest `name`、またはmanifestで宣言され`ResolvedPack.dependencies`へ固定されたdependency-local nameとして解決する。Pack内部importはconsumer Projectのinstall nameを直接参照してはならない。
- Pack自身のmanifest `name`とdependency-local nameは、そのPack内で一意かつ互いに異ならなければならない。dependency-local nameからProjectのinstall nameへの写像は`layerdraw.resolved.json`だけが保持し、Pack sourceを書き換えない。
- import syntax は source が pack か project module かを示さない。
- extension guessing、directory index、Registry fallback、parent-package search、および environment-dependent lookup は禁止される。
- side-effect import は存在しない。
- import は exported symbol のみを選択する。
- import graph は非循環でなければならない (MUST)。

### 6.3 エクスポート

```ldl
export { application_service, order_api }
export { subnet as network_segment }
export * from "./application_services.ldl"
export { allows } from "../../schema/relation_types/security.ldl"
```

宣言は export されない限り module-private である。Grouped syntax 内で宣言された Entity、Relation、Layer、Query、View、および Reference symbol は、local ID によって個別に export 可能なままである。owner を export すると、その完全な owner-scoped semantic subtree、すなわち type column/constraint、Entity/Relation row、Query parameter、または View table column/export が export される。Owner-scoped child は addressable StableSymbol であるが、独立して import または export されることはない。

Entity/Relation row group は owner declaration と同じ module に存在しなければならない (MUST)。これにより、side-effect import、filesystem discovery、または path-dependent augmentation なしに owner export closure が完全になる。owner とその row を分離する module split は無効である。workspace organization はそれらをアトミックに移動する。

export name は 1 つの module 内で declaration kind をまたいで一意でなければならない (MUST)。

### 6.4 有効文書の閉包

1. Project entryではentry module内のlocal declarationと、entryのnamed importで列挙したdeclaration、namespace import先の全public exportをdocument assembly rootとして選択する。
2. Pack entryではentryがpublic exportするdeclarationをclosure rootとして選択する。
3. 非entry moduleのimportはbindingを作るだけであり、そのmoduleの選択済みdeclarationから参照またはre-exportされたbindingだけが対応declarationを選択する。
4. 選択された declaration は transitive semantic dependency を引き込む。
5. 選択された owner declaration は、その owner-scoped child subtree を含む。
6. dependency は別途 export されない限り private のままである。
7. PackOrigin から任意の declaration を選択すると、その Pack root の manifest identity、reservation、および move も選択される。
8. Project entry以外の未使用import、関係のないpack example、未到達private declarationはeffective documentに入らない。
9. 選択された Reference は他の declaration と同様に入る。

Project entry importは実行side effectではなく、portable document manifestとしての明示的選択である。Pack entry moduleのpublic exportに同moduleのlocal declarationを含める場合、そのdeclarationは当然選択される。Pack内でexportされないprivate helperは、選択されたpublic declarationのsemantic dependencyである場合だけclosureへ入る。ただしRegistry publish validatorはreachable source module全体を構造/security validationし、privateまたは未選択という理由で禁止declarationやunsafe assetを見逃してはならない。

Entity はその EntityType と Layer に依存する。Relation はその RelationType と endpoint Entity に依存する。Query と View の依存関係は明示的 reference である。Relation によって表現される graph cycle は module cycle から独立している。

### 6.5 Packの解決

`layerdraw.resolved.json` は、各 install name を、canonical pack ID、exact version、artifact digest、installed path、expanded file digest、dependency mapping、Registry source、および compatibility result に map する。Credential は記録してはならない (MUST NOT)。

Path normalization、binding collision、Pack declaration、および resolved metadata のルールは詳細仕様に従う。

1 つの install name は、project ごとに正確に 1 つの version に解決される。Pack update は明示的な atomic change である。Import は version を決して含まない。

新しい version が pack-root `moves` を持つ場合、update tooling は pinned version を切り替える前に、それらを使用して影響を受ける project-local source binding と state address を書き換える。Move source は通常の compilation では alias として解決されない。resolved pack tree、`layerdraw.resolved.json`、および migrated project source はアトミックに commit される。そうでなければ old version が active のままである。

## 7. 名前、シンボル、および安定した識別子

### 7.1 識別子の語彙

LDL は 5 つの概念を区別する:

- **local ID**: `order_api` などのオーサリングされた identifier token。
- **source binding**: 1 つの module と expected kind の中で解決される local ID または import-qualified name。
- **StableSymbol**: 1 つの addressable semantic subject に対する、compiler の typed identity tuple。
- **StableAddress**: index、API、state、diagnostic、source ref、および audit のための StableSymbol の canonical string serialization。
- **external ID**: 別システムが所有する identifier。attribute または annotation として保存され、LDL identity として決して使用されない。

StableAddress text は、LDL source 内で代替 declaration または reference syntax として現れてはならない (MUST NOT)。Author は短い source binding を使用し、compiler がそれを解決する。これにより、project がコピーされたときにも source が portable に保たれ、storage/runtime identity が language に漏れ出すことを防ぐ。

### 7.2 構造化 StableSymbol

StableSymbol は構造的に次と同等である:

```text
Origin =
  ProjectOrigin { projectId }
  PackOrigin    { publisher, packName }

SymbolSegment { kind, localId }

StableSymbol {
  origin: Origin
  path: SymbolSegment[]
}
```

ProjectOrigin によって表される Project declaration、または PackOrigin によって表される manifest-defined Pack root では、`path` は空である。Pack root は addressable な resolution/publish subject であり、LDL declaration ではなく、source symbol として import できない。それ以外のすべての subject は 1 つの top-level segment を持ち、下で定義される 1 つの owner-scoped child segment を持つことができる。未知の origin kind、segment kind、parent/child combination、または追加 segment は Language 1 では無効である。

最上位セグメント種別:

| ソース宣言 | 安定セグメント種別 | 起点 |
| --- | --- | --- |
| `project` | segment なし。ProjectOrigin 自体 | project のみ |
| Pack manifest root | segment なし。PackOrigin 自体 | pack のみ |
| EntityType | `entity-type` | project または pack |
| RelationType | `relation-type` | project または pack |
| Layer | `layer` | project のみ |
| Entity | `entity` | project のみ |
| Relation | `relation` | project のみ |
| Query | `query` | project または pack |
| View | `view` | project または pack |
| Reference | `reference` | project または pack |

所有者スコープの子要素種別:

| 所有者種別 | 名前付き子要素 | 子セグメント種別 |
| --- | --- | --- |
| `entity-type` | 列 | `column` |
| `entity-type` | 一意制約 | `constraint` |
| `relation-type` | 列 | `column` |
| `relation-type` | 一意制約 | `constraint` |
| `entity` | 属性行 | `row` |
| `relation` | Relation属性行 | `row` |
| `query` | パラメーター | `parameter` |
| `view` | Table形状の列 | `table-column` |
| `view` | エクスポートレシピ | `export` |

異なる type 配下で同じ local ID を持つ column は異なる subject である。Entity row と Relation row は、owner kind がすでにそれらを曖昧性なく区別するため、同じ child kind を使用する。

owner-scoped child は常に owner の origin を継承する。Entity/Relation row は owner の module も共有しなければならない。Project source は、不変な pack-origin owner に row または reservation を追加できない。owner とそのすべての row group を一緒に移動しても identity は変わらない。

### 7.3 StableAddressの正準直列化

正準 serialization は、colon-delimited segment を持つ opaque ASCII URI である:

```text
Project declaration
  ldl:project:<project-id>

Project-local subject
  ldl:project:<project-id>:<top-kind>:<local-id>
  ldl:project:<project-id>:<top-kind>:<local-id>:<child-kind>:<child-id>

Pack subject
  ldl:pack:<publisher>:<pack-name>
  ldl:pack:<publisher>:<pack-name>:<top-kind>:<local-id>
  ldl:pack:<publisher>:<pack-name>:<top-kind>:<local-id>:<child-kind>:<child-id>
```

例:

```text
ldl:project:order_platform
ldl:project:order_platform:entity-type:application_service
ldl:project:order_platform:entity-type:application_service:column:environment
ldl:project:order_platform:entity:order_api
ldl:project:order_platform:entity:order_api:row:production
ldl:project:order_platform:relation:order_api_writes_db
ldl:project:order_platform:query:production_scope:parameter:environment
ldl:project:order_platform:view:production_topology:export:topology_svg
ldl:pack:layerdraw:aws-complete
ldl:pack:layerdraw:aws-complete:entity-type:subnet
```

address grammar は次のとおりである:

```ebnf
stable-address         = project-address | pack-address ;
project-address        = "ldl:project:", source-id,
                         [ ":", subject-path ] ;
pack-address           = "ldl:pack:", registry-segment, ":",
                         registry-segment, [ ":", subject-path ] ;
subject-path           = top-kind, ":", source-id,
                         [ ":", child-kind, ":", source-id ] ;
top-kind               = "entity-type" | "relation-type" | "layer"
                       | "entity" | "relation" | "query" | "view"
                       | "reference" ;
child-kind             = "column" | "constraint" | "row"
                       | "parameter" | "table-column" | "export" ;
source-id              = lower-snake-identifier ;
registry-segment       = lower-kebab-identifier ;
```

`lower-snake-identifier` は `[a-z][a-z0-9_]*` に一致する。`lower-kebab-identifier` は `[a-z][a-z0-9]*(?:-[a-z0-9]+)*` に一致する。Source ID、declaration ID、import alias、pack install name、および module path segment は最初の形式を使用しなければならない (MUST)。Registry publisher および pack-name segment は 2 番目の形式を使用しなければならない (MUST)。どちらの grammar も `:` を許可しないため、StableAddress serialization は escaping を必要とせず、percent-encoded または non-canonical な代替を拒否しなければならない (MUST)。

StableAddress の比較と順序付けは、locale-sensitive な display-string comparison ではなく、structured tuple `(origin kind rank, origin components, path length, path kind rank/id pairs)` を使用する。rankは次で固定する。

```text
origin: project < pack
top-level: entity-type < relation-type < layer < entity < relation < query < view < reference
child: column < constraint < row < parameter < table-column < export
```

rootのempty pathは同一originのsubject pathより前、1-segment pathは2-segment pathより前に並ぶ。Project ID、Pack publisher/name、local IDは各grammarがASCIIに限定するためunsigned ASCII byte順で比較する。kindが許可されないorigin/owner combinationはsort対象になる前にvalidation errorである。この順序をStableSymbol orderと呼び、normalized array、set、diagnostic、move、hash payload、ViewData source refの全箇所で共通使用する。

### 7.4 ソースの束縛と解決

effective document は、EntityTypes、RelationTypes、Layers、Entities、Relations、Queries、Views、および References のための、分離された top-level namespace を持つ。Owner-scoped subject は、それらの resolved owner と child kind の配下に分離された namespace を持つ。

Resolution は次のように進む:

1. grammar と schema context から expected declaration kind を決定する。
2. current module 内で local name または import alias を解決する。
3. declaration の project または canonical pack origin を解決する。
4. owner-scoped name を、すでに resolved された owner に対して解決する。
5. StableSymbol と canonical StableAddress を構築する。

unqualified declaration reference は、expected kind の source binding 1 つに正確に解決される。`aws.subnet` は `aws` を lexical import alias としてのみ使用する。この alias は resolution 中に canonical pack origin に置換される。Typed Query/View column selector は、statically known な owner type の集合をまたいで、互換性のある column ID 1 つを意図的に解決してよい。その normalized form は、解決された column StableAddress の ordered set を記録しなければならない (MUST)。

次は StableSymbol identity に決して寄与しない:

- display name、description、tag、annotation、または value。
- module path、source filename、byte range、または declaration order。
- import alias、named-import alias、または pack install name。
- pack version、Registry source、artifact digest、または signature。
- host storage path、SaaS record ID、revision、actor、または timestamp。

Pack version と content change は、resolved dependency digest と semantic hash によって別個に束縛される。同じ canonical pack ID を主張する 2 つの選択済み pack source が異なる content を持つ場合、それは resolution error であり、2 つの identity ではない。

### 7.5 一意性と予約

Top-level local ID は `(origin, top-level kind)` ごとに一意である。Child local ID は `(owner StableSymbol, child kind)` ごとに一意である。同じ local text は、異なる kind または owner をまたいで再利用してよい。

StableSymbol によって表されるすべての local ID は source でオーサリングされなければならない (MUST)。GUI または agent は ID を提案してよいが、commit 前に identifier を具現化しなければならない。Content hash、array index、source order、read time にのみ生成される display-name slug、database sequence value、および random runtime ID は、authored identity の代替になってはならない (MUST NOT)。

Committed Revision に現れた identity は、異なる semantic subject のために再利用してはならない (MUST NOT)。Committed Revision に到達しなかった create/delete は reservation を作成しない。committed subject を削除すると、その old local ID が最も近い authored reservation に具現化される。Rename は下で定義する durable `moves` mapping を使用する。すべての move source は暗黙の reservation でもある。

Project全体の最上位予約:

```ldl
reserved {
  entity_types [legacy_server]
  relation_types [legacy_link]
  layers [legacy_network]
  entities [old_gateway]
  relations [old_gateway_link]
  queries [legacy_query]
  views [legacy_view]
  references [legacy_guide]
}
```

Pack-wide reservation は同じ block を使用するが、PackOrigin で許可される declaration kind に一致して、`entity_types`、`relation_types`、`queries`、`views`、および `references` のみを含んでよい。

EntityType および RelationType child reservation:

```ldl
reserve {
  columns [legacy_runtime]
  constraints [legacy_environment_owner]
}
```

Entity および Relation row reservation は owner item に保存される:

```ldl
entities application_service @application {
  order_api "Order API" {
    reserve_rows [legacy_snapshot]
  }
}

relations writes_to {
  order_api_writes_db: order_api -> order_db {
    reserve_rows [legacy_rule]
  }
}
```

Query および View child reservation:

```ldl
query production_scope "Production Scope" {
  reserve {
    parameters [legacy_environment]
  }
  select {}
}

view production_topology "Production Topology" topology {
  reserve {
    table_columns [legacy_owner]
    exports [legacy_pdf]
  }
  source query production_scope {}
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
  }
}
```

Reservation entry は definition data であり、owner/root own-subject hash と normalized definition hash に入り、graph hash には入らない。Reserved ID は同じ origin 内で unreserve または reuse できない。Project-wide reservation は entry module に属し、Project root に所有される。pack-wide reservation は `pack.ldl` に属し、manifest-defined Pack root に所有される。

origin entry ごとに許可される top-level `reserved` declaration は最大 1 つである。type、Query、または View ごとに許可される `reserve` block は最大 1 つであり、Entity または Relation item ごとに許可される `reserve_rows` statement は最大 1 つである。空の reservation category と空の reservation block は formatter によって削除される。

すべての scope と kind について、current ID と reserved ID は一意かつ互いに素でなければならない (MUST)。存在したことのない ID に対する reservation は、migration history を import する場合にのみ合法である。tool は明示的な migration reason を要求するべきである (SHOULD)。Duplicate reservation、reserved ID を使用する active declaration、wrong owner kind 配下の child reservation、および cross-origin row augmentation は error である。

1 つの definition snapshot だけを与えられた validator は、unreserved absent ID が過去の履歴に存在したかどうかを証明できない。Snapshot validation は current/reserved/move の disjointness と reference integrity のみを強制する。runtime checkpoint、history-aware CI check、または Registry update publish は、parent Committed Revision/version と比較し、reservation を省略した deletion または move を省略した rename を拒否しなければならない (MUST)。trusted history のない definition-only LDL を import する場合、その authored reservation と move を完全な既知 identity history として受け入れ、heuristic に identity を捏造してはならない (MUST NOT)。

### 7.5.1 永続的な識別子移動

`moves` は、address migration table と同様に、Git-managed および definition-only LDL における rename intent を保持する。これは project entry module または `pack.ldl` に属し、origin-root metadata であって importable declaration ではない。

```ldl
moves {
  entity legacy_order_api -> order_api
  relation old_order_write -> order_api_writes_db
  entity_type old_service -> application_service
  entity_type_column application_service old_env -> environment
  entity_row order_api legacy_production -> production
  query_parameter production_scope old_environment -> environment
  view_export production_topology old_svg -> topology_svg
}
```

Top-level move entry は `<kind> <old-id> -> <new-id>` である。Child move entry は `<child-kind> <current-owner-id> <old-child-id> -> <new-child-id>` である。Child owner ID は terminal current owner を名指しする。owner 自体が move された場合、その owner move が old owner prefix mapping を自動的に提供する。

Source move kind は正確に次のように map される:

| 移動種別 | 安定種別 |
| --- | --- |
| `project` | ProjectOriginのルート |
| `entity_type`, `relation_type`, `layer`, `entity`, `relation`, `query`, `view`, `reference` | 対応する top-level kind |
| `entity_type_column`, `relation_type_column` | named current type 配下の `column` |
| `entity_type_constraint`, `relation_type_constraint` | named current type 配下の `constraint` |
| `entity_row`, `relation_row` | named current owner 配下の `row` |
| `query_parameter` | named current Query 配下の `parameter` |
| `view_table_column` | named current View 配下の `table-column` |
| `view_export` | named current View 配下の `export` |

ルール:

- origin entry ごとに許可される `moves` block は最大 1 つである。
- move source は active declaration から absent であり、reserved ID として機能する。
- terminal move target は、同じ kind/scope の active subject として存在する。
- move chain は許可され、非循環でなければならない (MUST)。authored immediate edgeでは各addressが最大1つのimmediate predecessorと1つのimmediate successorを持つ。normalized identity historyはこのedge列を保持し、別のderived `move_closure`へ各historical sourceからterminal currentへのmappingをmaterializeする。
- intermediate target は同じ chain 内の next source であってよい (MAY)。move source は `reserved` または active declaration にも現れることはできない。
- project move target は current Project ID と等しくなければならず (MUST)、project entry でのみ許可される。
- pack declaration move は同じ PackOrigin 内に留まる。canonical pack ID の変更は Registry-level cross-origin migration であり、LDL move ではない。
- move は root own-subject hash と normalized definition hash に入り、graph hash には入らない。
- history-aware tool は、すべての supported source revision について move を保持しなければならず (MUST)、それらを silently prune してはならない (MUST NOT)。

State restore、revision diff、source-ref reconciliation、および Registry update は、unmatched address を add/remove として扱う前に、transitive old/new StableAddress mapping を消費する。Move entry は active alias を決して作成しない。通常の LDL reference は current ID のみを使用する。
Address migration は、まず `project` root move を適用し、次に terminal current origin 内の top-level subject move、次に owner-derived prefix change、最後に explicit child move を適用する。この順序により、Project、owner、および child の同時 rename が決定論的になる。

### 7.6 名前変更、移動、および複製

- display text の変更は通常の field update である。
- declaration を module 間で移動しても identity は変わらない。
- Entity を別の Layer に移動しても identity は変わらない。
- 有効な `moves` entry なしに raw source text で ID を変更することは remove/add である。
- `rename_subject`は、選択されたすべてのproject-local source bindingを更新し、durable move entryを書き込むatomic project-local migrationである。
- pack-origin subject は consuming project に対して immutable であり、そこで rename できない。
- Project ID の変更は明示的な whole-project identity migration であり、state/audit binding を migrate するか、新しい identity lineage を開始しなければならない (MUST)。
- source を別の Project ID を持つ project に copy すると、新しい project-local identity が作成される。
- 同じ canonical pack を別の alias で import しても pack identity は保持される。

Rename は display name、source proximity、または structural similarity によって決して推測しない。LDL `moves` mapping は portable definition-level identity migration である。operation/audit record は、それがいつ誰によって適用されたかを追加で記録してよい。Alias は migration 後に active source binding として残らない。

ownerをrenameすると、child local IDが変わらない場合でも、すべてのaddressable childのStableAddress prefixが変わる。同じatomic definition migrationは、完全なsubtreeについてold/new mappingをemitし、source refとdefinition cacheを更新しなければならない (MUST)。外部stateとaudit indexは同じidempotency keyでmigrateし、同時完了できなければstaleとしてreconcileを要求する。childのrenameは、そのchild addressと直接のsource bindingだけを変更する。Project ID migrationはこのルールをすべてのproject-local subjectに適用する。

Project IDは通常の`rename_subject` targetではない。runtimeは、whole-document preview、state/backend capability check、`moves { project old -> current }` update、およびall-address migrationを伴う、別個の`migrate_project_identity` operationを公開する。Definition-only toolは代わりにProject ID editを新しいidentity lineageを持つforkとして扱ってよい。

### 7.7 匿名構造とフィールドパス

すべての syntax node が StableSymbol であるわけではない。Common field、projection/render block、Query predicate group、View axis/cell、sort entry、およびその他の unnamed structural node は、最も近い named subject に所有される。

Semantic operation は、schema-defined field path と owner StableAddress によってこれらの node を address する。Ordered anonymous list は owner の expected semantic hash 配下で replace または edit される。ordinal position は durable identity になってはならない (MUST NOT)。将来の機能が anonymous node に対する独立した reference、state、または concurrent edit を必要とする場合、language は position または content から identity を導出するのではなく、明示的な authored ID と child kind を追加しなければならない (MUST)。

field path の wire representation は、source-text path ではなく、normalized schema field token の array である:

```text
["display_name"]
["annotations", "owner"]
["projections", "diagram"]
["where"]
```

target StableSymbol kind に登録された field のみが有効である。Map key は final token であってよい。`projection diagram` など、grammar enum によって key 付けされる repeated singleton block は、その enum を token として使用する。Positional array index、byte offset、module path、import alias、および display label は無効な field-path token である。anonymous predicate tree または ordered sort list の更新は、将来の typed operation が non-positional edit contract を定義しない限り、その complete registered field を置き換える。

### 7.8 ホスト文書のスコープ

project StableAddress は portable logical identity であり、globally unique SaaS/storage object identifier ではない。2 つの独立した document がどちらも `project order_platform` を宣言し、そのため等しい project StableAddress を含むことがあり得る。

document boundary をまたぐすべての API、state backend、audit record、lease、および operation log は、external host Document ID によって address を scope しなければならない (MUST):

```text
DocumentSubjectRef {
  document_id: <host-owned immutable ID>
  address: <StableAddress>
}
```

host Document ID は LDL に決して保存されず、Project declaration が formatted または moved されても変わらない。すでに 1 つの document に scoped されている API route は、各 nested reference から `document_id` を省略してよい (MAY)。Cross-document LDL reference は Language 1 では禁止される。

runtime は StableAddress を構造的に parse し、その project origin が選択された effective document と一致すること、またはその pack origin が pinned dependency closure に存在することを検証しなければならない (MUST)。String-prefix check は不十分である。wrong kind、owner chain、document origin、unselected pack、removed subject、または stale migration lineage の address は、structured diagnostic で拒否される。

Local host は Document ID を sidecar state または host metadata に永続化する。external-drive host は provider item identity と connection scope を使用する。server host は immutable record ID を割り当てる。host metadata のない definition-only file は、最初に永続化されるときに新しい Document ID を受け取る。`.layerdraw` を新しい document として import する場合、package metadata が source document を記録していても、新しい host Document ID を割り当てなければならない (MUST)。既存 document への restore には、その document に対する明示的な revision/import operation が必要である。

## 8. スカラー型と属性テーブル型

### 8.1 スカラー型

| 型 | LDL行値 | 正規化値 |
| --- | --- | --- |
| `string` | クォートされた文字列 | UTF-8文字列 |
| `integer` | 整数 | `[-9007199254740991, 9007199254740991]` 内の正確な整数 |
| `number` | 整数または小数 | 有限のIEEE-754 binary64数値 |
| `boolean` | `true` / `false` | ブール値 |
| `enum` | 裸のatomまたはクォートされた文字列 | 文字列 |
| `date` | クォートされた `YYYY-MM-DD` | 文字列 |
| `datetime` | オフセット付きのクォートされたRFC 3339 | UTC正規化文字列 |

暗黙の変換はない。JSON安全範囲外の整数リテラルはエラーである。より広い精度を必要とする大きなカウンターおよびプロバイダーIDは、文字列または将来の明示的なスカラー型を使用する。`number` はNaNおよび無限大を拒否し、負のゼロは正のゼロに正規化される。`null` は行値ではない。`_` または省略された列は不在を表し、空文字列は明示的なデータである。

`date` は有効なグレゴリオ暦の `YYYY-MM-DD` でなければならない。`datetime` は明示的な `Z` または数値オフセットを持ち、0から3桁の小数秒を持つRFC 3339でなければならない。これは `Z` 付きのUTCに正規化され、末尾の小数ゼロを削除し、最大ミリ秒精度を保持するため、TypeScript、Go、正規化JSON、およびLadybugDB WASM/JavaScript境界が同じ瞬間を比較できる。

### 8.2 列構文

```ldl
columns {
  environment "Environment" enum [prod, stg, dev] reserve_values [legacy] required default dev
  owner "Owner" string required
  cidr "CIDR" string format cidr
  capacity "Capacity" number min 0 max 100
}
```

正規の修飾子順序は次のとおりである。

```text
<id> <display-string> <type> [enum-options]
  [reserve_values <enum-values>] [required] [default <value>] [format <format>]
  [min <number>] [max <number>]
  [min_length <integer>] [max_length <integer>]
```

アクティブなオプションは `enum` の直後に書かれ、少なくとも1つのアクティブなオプションが必要である。`reserve_values` はenum列/パラメーターにのみ許可され、同じ所有者内で再びアクティブにできない、削除済みのenum文字列を記録する。アクティブ値と予約値は一意で互いに素である。裸の正規atomとクォートされた文字列は同じ文字列値に正規化される。コミット済みのenumオプションを削除するには、既存の値/参照を移行し、それを `reserve_values` に追加する履歴対応操作が必要である。Enum値はデータリテラルであり、StableSymbolsではないため、StableAddressesや `moves` エントリを受け取らない。

サポートされる文字列フォーマットは `uri`、`email`、`hostname`、`ipv4`、`ipv6`、および `cidr` である。デフォルトはunspecified row cellまたは省略Query argumentのnormalized valueへ適用されるが、formatterはsourceへ自動挿入しない。明示的なmaterialization operationだけがdefault値をsourceへ書き出せる。enum defaultはアクティブかつ非予約でなければならない。

各フォーマットの正確な検証と正規化は詳細仕様に従う。フォーマット正規化値は、型付き等価性、一意性、Query述語、正規化JSON、およびハッシュに参加する。

### 8.3 安定行と位置セル

```ldl
rows application_service [environment, owner] {
  order_api production: prod, "Commerce Platform"
  payment_api production: prod, "Payments"
}
```

直ちに見えるヘッダーは、各位置セルを列IDに束縛する。これはLDLにおける唯一の位置データ構成である。

規則:

- 所有者Entity/Relation IDおよび行IDは常に明示的である。
- 行IDは1つの所有者内で一意であり、値から導出されることはない。
- ヘッダー列は一意であり、参照される型によって宣言される。
- セル数はヘッダーと完全に一致する。
- `_` は任意列にのみ許可され、省略を意味する。
- 異なるヘッダーを持つ複数の行グループが許可される。
- 1つの `(owner, row-id)` は正確に1つの行項目に現れ、グループをまたいで継続できない。その行のヘッダーから省略された宣言済み列は不在である。
- 行順序に意味論上の効果はない。
- ビジネス一意性は行アイデンティティとは別である。

headerから省略されたColumnはunspecifiedであり、Column defaultがあればnormalized rowへmaterializeする。`_`はoptional Columnの明示absentでありdefaultを抑止する。default適用後もrequired Columnがabsentならerrorである。Entity/Relationがrowを1件も持たないこと自体はrequired違反ではない。

列または一意制約の削除は、所有者の `reserve` ブロックを更新しなければならない (MUST)。既存の行値は明示的な移行によってのみ変更される。

### 8.4 一意制約スコープ

`unique <constraint-id> [<column-id>...]` は一度に1つの所有者属性テーブルを検証する。

- EntityType制約は、その型の各Entity内で独立して評価される。
- RelationType制約は、その型の各Relation内で独立して評価される。
- 異なるEntitiesまたはRelationsによって所有される行は、この制約を通じて競合しない。
- 制約対象のすべての列が存在する行のみが参加する。
- `_` または省略された任意値は、別の欠損値と等しいのではなく、その行を制約から除外する。
- 比較は、ソース表記や表示フォーマットではなく、正規化された型付きスカラー等価性を使用する。
- 列を持たない制約は無効である。

EntitiesまたはRelationsをまたぐプロジェクト全体のビジネス一意性は、グラフ構造、作成されたID、明示的なホスト検証ポリシー、または将来の型付き制約種別によって表現される。これは `unique` から推論してはならない (MUST NOT)。

## 9. 共通宣言規則

共通の任意フィールドは、この構文と正規順序を使用する。

```ldl
description "Plain-text description"
tags [critical, customer_facing]
annotations { owner: "platform-team", external_id: "svc-42" }
```

- `description` はプレーンテキストである。
- `tags` は文字列集合である。重複はエラーであり、正規化順序は辞書順である。
- `annotations` は `map<string,string>` の統合メタデータである。
- annotation keyは非空のNFC UTF-8 stringで、identifier形ならbare、それ以外はquoted stringを使う。NFC後の重複はerrorである。Query argumentsなどschemaがidentifier keyを要求するobjectではquoted keyを許可しない。
- コア意味論は、未登録のannotationキーに依存してはならない (MUST NOT)。
- 資格情報、生成された状態、ソースコード、およびコールバックはannotationsでは禁止される。

表示名はヘッダー文字列であり、繰り返される本文フィールドではない。正規フィールド順序は宣言ごとに固有であり、フォーマッターによって強制される。

## 10. Project宣言

project entry moduleは正確に1つのproject宣言を持たなければならず、他のproject moduleにproject宣言を置いてはならない。プロジェクト有効文書にはその1件だけが選択される。Packsはprojectsを宣言しない。

```ldl
project retail_platform "Retail Platform" {
  description "Production retail platform"
  tags [production]
  annotations { owner: "platform-team" }
}
```

Project IDは、すべてのプロジェクトローカルStableAddressのProjectOriginコンポーネントを形成する。それを変更することは、明示的なプロジェクト全体の移行である。

## 11. Layer宣言

Layersは1つ以上のグループ化ブロックで宣言される。

```ldl
layers {
  application "Application" @20
  network "Network" @40 {
    description "Virtual and physical network boundaries"
    tags [infrastructure]
  }
}
```

各項目は、安定ID、必須の表示名、および必須の符号付き整数順序を含む。同じ順序は許可され、Layer StableAddressによって同順を解決する。Layerメンバーシップは包含を含意しない。

複数の `layers` ブロックは合法である。1つのLayer IDはoriginごとに一度だけ宣言できる。

## 12. EntityType宣言

EntityTypeはEntityインスタンスのクラス/スキーマである。

```ldl
/// デプロイ可能なアプリケーションサービス。
entity_type application_service "Application Service" {
  icon "app-window"
  image "../assets/application-service.png"
  color "#2F6B62"
  representation shape rounded

  columns {
    environment "Environment" enum [prod, stg, dev] required
    owner "Owner" string required
  }

  unique environment_owner [environment, owner]
  reserve {
    columns [legacy_runtime]
    constraints [legacy_owner_rule]
  }
  tags [application]
}
```

EntityType本文フィールド:

| フィールド | カーディナリティ | 意味 |
| --- | --- | --- |
| `icon` | 0または1 | ホストがサポートするアイコン名 |
| `image` | 0または1 | 宣言モジュール相対のアセットパス |
| `color` | 0または1 | `#RRGGBB` または `#RRGGBBAA` |
| `representation` | 正確に1 | `container`、`table`、または `shape <kind>` |
| `columns` | 0または1 | 型付き属性テーブルスキーマ |
| `unique <id> [columns]` | 0以上 | ビジネス一意性制約 |
| `reserve` | 0または1 | 削除済みの列IDおよび制約ID |
| 共通フィールド | 任意 | description、tags、annotations |

Shape種別は `rect`、`rounded`、`ellipse`、`diamond`、`cylinder`、`cloud`、`hexagon`、`person`、および `device` である。

Working Documentの未解決imageは診断を生成し、previewではicon、次にrepresentation defaultへフォールバックしてよい。Committed Revisionでは解決失敗をerrorとする。authored pathやmodule位置は型identityへ寄与せず、解決済みasset content digestがEntityType semanticsへ寄与する。

暗黙の組み込みEntityType preludeは存在しない。ホストプリセットは、明示的なプロジェクトローカルEntityTypeを実体化するか、pack typeをインストールしてimportしなければならない (MUST)。

## 13. EntityおよびEntity Row宣言

### 13.1 Entityグループ

Entityインスタンスは高密度のグループ化宣言を使用する。

```ldl
entities application_service @application {
  order_api "Order API" {
    description "Accepts and validates orders"
    tags [critical]
  }
  payment_api "Payment API"
}
```

グループヘッダーは、すべての項目に対して1つのEntityTypeとLayerを提供する。各項目は安定Entity IDと表示名を提供する。項目ブロックは共通フィールドおよび任意の `reserve_rows [<row-id>...]` を許可する。

規則:

- typeおよびLayer参照は解決しなければならない (MUST)。
- Entityは正確に1つの `entities` 項目に現れることができる。
- 複数のグループが同じtype/Layerを使用できる。
- 物理的なグループ位置は意味論上のLayerを決定しない。
- Entity包含は `parent` または `children` フィールドを使用してはならない (MUST NOT)。
- 正規canvas座標はEntityに属さない。

### 13.2 Entity行

```ldl
rows application_service [environment, owner] {
  order_api production: prod, "Commerce Platform"
  payment_api production: prod, "Payments"
}
```

グループtypeは、すべての所有者Entityの解決済みtypeと等しくなければならない (MUST)。別の列サブセットを持つ行は別のグループを使用する。

すべての行所有者は同じモジュール内で宣言されなければならない (MUST)。Entityの選択またはエクスポートは、そのモジュール内のそのEntityのすべての行グループを含む。

## 14. RelationType宣言

RelationTypeは、有向バイナリRelation事実の意味とスキーマを定義する。

```ldl
relation_type allows "Allows traffic" network {
  allow_self false
  duplicate_policy deny_same_type_between_same_endpoints

  from source types [subnet, security_group, application_service]
  to destination types [subnet, security_group, application_service]

  cardinality {
    to_per_from 0..*
    from_per_to 0..*
  }

  label "allows"
  reverse "is allowed by"

  columns {
    protocol "Protocol" enum [tcp, udp, icmp] required
    port "Port" string required
    cidr "Source CIDR" string required format cidr
    action "Action" enum [allow, deny] required
  }

  unique traffic_rule [protocol, port, cidr]

  traversal {
    default_direction outgoing
    participates_in_impact true
    participates_in_dependency_matrix true
  }

  projection diagram {
    mode edge
    source_endpoint from
    target_endpoint to
    edge_label forward_label
    include_relation_type true
  }

  render edge {
    arrow forward
    line solid
    label forward_label
  }

  export {
    include_endpoints true
    include_relation_rows true
  }
}
```

### 14.1 必須スキーマ

| フィールド | カーディナリティ |
| --- | --- |
| header semantic kind | 正確に1 |
| `allow_self` | 0または1。デフォルトは `false` |
| `duplicate_policy` | 0または1 |
| `from` / `to` | それぞれ正確に1 |
| `cardinality` | 0または1。デフォルトは両方向とも `0..*` |
| `label` | 正確に1 |
| `reverse` | 0または1 |
| `columns` | 0または1 |
| `unique` | 0以上 |
| `traversal` | 0または1 |
| `projection <primitive>` | primitiveごとに0または1 |
| `render <primitive>` | primitiveごとに0または1 |
| `export` | 0または1 |
| `reserve` | 0または1。削除済みの列IDおよび制約ID |
| 共通フィールド | 任意 |

Semantic kindは次のいずれかである。

```text
dependency data_flow control_flow deployment network security containment
ownership sequence impact reference governance
```

ドメイン固有の意味は、個別のprojectまたはPack RelationTypeに属する。Language 1は安定したsemantic vocabularyと閉じたprojection / render primitive identifierだけを規定する。LayerDraw製品ではGo Engineがsemantic / projection primitiveを検証し、TS Render capability profileがrender primitive対応可否を検証する。

省略されたすべての値は詳細仕様のデフォルトを使用する。特に、duplicate-policyのデフォルトは `deny_same_type_between_same_endpoints` であり、traversal participation flagsはデフォルトでfalseであり、明示的に空のendpoint selectorはエラーである。

### 14.2 エンドポイント契約

正規endpoint構文は次のとおりである。

```ldl
from <role> types [<EntityType>...] layers [<Layer>...]
to <role> types [<EntityType>...] layers [<Layer>...]
```

`types` と `layers` は独立に任意である。

```ldl
from workload types [application_service] layers [application]
to placement types [aws.subnet] layers [network]
```

欠損した `types` または `layers` は、その次元に対して無制限を意味する。明示的な空リストはエラーである。両方のリストを持つendpointは両方を満たさなければならない。

すべてのRelationsは有向のままである。ER風レンダリングは矢印を隠してもよい (MAY) が、正規化アイデンティティ、traversal、cardinality、およびLadybugDB mappingは `from` と `to` を保持する。

### 14.3 カーディナリティ

Cardinalityは作成された方向に相対的である。

```ldl
cardinality {
  to_per_from 0..1
  from_per_to 0..*
}
```

- `to_per_from`: 1つの適格な `from` Entityに対する `to` Entitiesの数。
- `from_per_to`: 1つの適格な `to` Entityに対する `from` Entitiesの数。
- 最小値は `0` または `1` である。
- 最大値は `1` または `*` である。
- 最小値チェックはendpoint制約を満たす各Entityに適用される。

Cardinalityはグラフ事実を検証する。これはrender hintではない。

### 14.4 重複ポリシー

許可される値:

```text
allow
deny_same_type_between_same_endpoints
deny_any_between_same_endpoints
```

重複比較は順序付きendpointを使用する。`deny_any_between_same_endpoints` は、typeにかかわらず同じ順序付きペア上の2つ目のRelationを禁止する。

### 14.5 ラベルと行

`label` は順方向の自然言語読みである。`reverse` は任意の逆方向読みである。Labelsは方向を変更しない。

RelationTypeはEntityTypeと同じ `columns`、`unique`、および `reserve` 規則を使用する。Relation rowsは作成された安定IDを使用する。

### 14.6 探索ポリシー

```ldl
traversal {
  default_direction outgoing
  participates_in_impact true
  participates_in_flow false
  participates_in_hierarchy false
  participates_in_dependency_matrix true
}
```

`default_direction` は `outgoing`、`incoming`、または `both` である。Flagsは生成されたQuery/View recipesのデフォルトであり、明示的なQuery selectorを暗黙に広げることはない。

### 14.7 投影プリミティブ

Projection rulesはRelation factsを型付きViewDataに変換する。これらはMaster Graphを変更しない。

合成投影:

```ldl
projection composed {
  mode nest
  parent_endpoint to
  child_endpoint from
  priority 0
  conflict diagnostic
  keep_edge false
}
```

- mode: `edge`、`nest`、`overlay`、`badge`、または `hide`。
- `priority` は任意の符号付き整数であり、デフォルトは `0` である。
- nestのフィールド: `parent_endpoint`、`child_endpoint`、`priority`、`conflict`、`keep_edge`。
- overlayのフィールド: `overlay_endpoint`、`target_endpoint`。
- badgeのフィールド: `badge_endpoint`、`target_endpoint`。

Diagram投影:

```ldl
projection diagram {
  mode edge
  source_endpoint from
  target_endpoint to
  edge_label forward_label
  include_relation_type true
}
```

Table投影:

```ldl
projection table {
  row_mode relation_rows
  include_from true
  include_to true
  include_relation_type true
}
```

Matrix投影:

```ldl
projection matrix {
  row_endpoint from
  column_endpoint to
  include_relation_rows true
}
```

Tree投影:

```ldl
projection tree {
  parent_endpoint from
  child_endpoint to
}
```

Flow投影:

```ldl
projection flow {
  source_endpoint from
  target_endpoint to
  connector_kind sequence
  branch_value_column outcome
}
```

Context投影:

```ldl
projection context {
  fact_template "{from.display_name} depends on {to.display_name}"
  reverse_fact_template "{to.display_name} is required by {from.display_name}"
  include_attribute_rows true
}
```

有効なplaceholderはこれらのみである。

```text
{from.id} {from.display_name} {from.type} {from.layer}
{to.id} {to.display_name} {to.type} {to.layer}
{relation.id} {relation.display_name} {relation.type}
```

Template `.id` は読みやすい散文のための作成されたローカルIDであり、StableAddressではない。RenderData/ViewData source refsはStableAddressesを別に持つ。`.type` と `.layer` は参照される宣言のローカルIDをレンダリングする。pack aliasesはidentityとして出力されない。

### 14.8 描画およびエクスポートのヒント

```ldl
render edge {
  arrow forward
  line solid
  color "#526D82"
  label forward_label
}

render nested {
  frame_label parent
  frame_style subtle
}

render overlay {
  kind shield
  position top_right
  max_items 4
}

render badge {
  icon "shield-check"
  label count
  position top_right
}
```

Render hintsはViewData semanticsを変更しない。新しいsemantic / projection primitiveにはLanguage / ViewData contract change、新しいvisual primitiveにはversioned Render capability profile changeが必要である。Packは実行可能rendererを提供できない。

Relation export hintsは、View exports内のRelation rowsのデフォルトである。

```ldl
export {
  include_endpoints true
  include_relation_rows true
  sheet_name "Network Rules"
}
```

## 15. RelationおよびRelation Row宣言

### 15.1 Relationグループ

```ldl
relations deployed_to {
  order_deployment: order_api -> private_subnet
  payment_deployment: payment_api -> private_subnet "Payment placement" {
    tags [critical]
  }
}
```

グループヘッダーは1つのRelationTypeを提供する。すべての項目は、安定Relation ID、明示的な `from -> to` endpoints、任意の表示名、および共通フィールドと `reserve_rows` を含む任意ブロックを含む。

規則:

- RelationTypeおよびendpoint Entitiesは解決しなければならない (MUST)。
- endpoint constraints、self policy、duplicate policy、およびcardinalityが適用される。
- 1つのRelation IDは正確に一度だけ現れる。
- directionは意味論上のものであり、ソース位置や視覚レイアウトから推論されることはない。
- 同じendpoints間の複数Relationsは、duplicate policyが許可する場合にのみ合法である。

### 15.2 Relation行

```ldl
relation_rows allows [protocol, port, cidr, action] {
  public_to_private https: tcp, "443", "10.0.1.0/24", allow
  public_to_private health: tcp, "8080", "10.0.1.0/24", allow
}
```

グループtypeは、すべての所有者Relationの解決済みRelationTypeと等しくなければならない (MUST)。Relation row identityは、所有者Relation StableSymbolに作成されたrow IDを加えたものである。

すべての行所有者は同じモジュール内で宣言されなければならない (MUST)。Relationの選択またはエクスポートは、そのモジュール内のそのRelationのすべてのrelation-row groupsを含む。

## 16. Query宣言

Queryは保存された型付きselectionおよびtraversal recipeである。これはbackend query textではない。

```ldl
query production_network "Production network neighborhood" {
  parameters {
    environment enum [prod, stg, dev] required default prod
  }

  select {
    layers [network, application, security]
    entity_types [aws.vpc, aws.subnet, application_service, security_group]
    relation_types [aws.contains, deployed_to, protects, allows]
    roots [order_api]
  }

  where all {
    field display_name exists
    rows any types [application_service] {
      cell environment == $environment
    }
  }

  relation_where any {
    field type in [aws.contains, deployed_to, protects, allows]
    rows any types [allows] {
      cell protocol == tcp
    }
  }

  traverse both 0..3 visit_once
  result [seed_entities, traversed_entities, path_relations, induced_relations]
}
```

生のCypher、SQL、JavaScript、正規表現コード、およびプロバイダー固有のquery stringsは禁止される。Runtimesは、この型付きrecipeをLadybugDBまたは別のengineへコンパイルしてもよい (MAY)。

Selector、predicate、seed、traversal、path、cycle、result inclusion、およびordering semanticsは詳細仕様に従う。Predicatesは、seedsだけでなく、完全な適格traversed subgraphを制約する。

Queryはstate fieldを参照する場合だけ、`state_input required`または`state_input optional`をちょうど1つ宣言する。省略時は`none`へ正規化し、state predicateを禁止する。`required`は評価開始時にAccess判定済みの互換StateQuerySnapshotを要求する。`optional`はsnapshotが無い場合に明示的なno-state入力を使い、すべてのstate fieldをmissingとして評価する。backend、credential、現在時刻、session actorを暗黙に読み取ってはならない。

### 16.1 パラメーター

```ldl
parameters {
  environment enum [prod, stg, dev] required default prod
  minimum_capacity number default 0 min 0
}
```

Parameter構文は、表示名なしでcolumn modifier grammarを使用する。参照は `$environment` を使用する。Runtime argumentsは実行前にチェックされる。デフォルトのない欠損required parametersおよびunknown argumentsはエラーである。Parametersは環境変数を暗黙に読み取らない。

削除済みparameter IDsはQuery本文内に保持される。

```ldl
reserve {
  parameters [legacy_environment]
}
```

### 16.2 選択

```ldl
select {
  layers [application, network]
  entity_types [application_service, aws.subnet]
  relation_types [deployed_to]
  roots [order_api]
}
```

各selectorについて:

- 不在は無制限を意味する。
- 空リストは何も選択しないことを意味する。
- 参照は解決しなければならない (MUST)。
- 重複はエラーである。

`roots` がない場合、selectorsと `where` に一致するすべてのEntityがseedである。明示的なrootsもselectorsおよびpredicatesを満たさなければならない。

### 16.3 型付き述語ツリー

Entity predicatesは `where` に置かれる。Relation predicatesは `relation_where` に置かれる。

```ldl
where all {
  field display_name contains "API"
  any {
    field tags contains critical
    field layer == application
  }
  not {
    field description missing
  }
}
```

ブールグループ:

- `all { ... }`: すべてのchild。空はtrueである。
- `any { ... }`: 少なくとも1つのchild。空はfalseである。
- `not { ... }`: 正確に1つのchild。

正規predicate operators:

```text
== != < <= > >= in not_in contains starts_with ends_with exists missing
```

Orderingにはnumber/date/datetimeが必要である。String operatorsにはstringsが必要である。`in` および `not_in` にはlistsが必要である。Operator compatibilityは静的にチェックされる。

フィールド型は固定である。

| 所有者 | Field | Predicate type |
| --- | --- | --- |
| Entity | `id` | 作成されたlocal-ID string |
| Entity | `address` | Entityのシンボル参照 |
| Entity | `display_name`, `description` | `string` |
| Entity | `type` | EntityTypeのシンボル参照 |
| Entity | `layer` | Layerのシンボル参照 |
| Entity | `tags` | stringsの集合 |
| Relation | `id` | 作成されたlocal-ID string |
| Relation | `address` | Relationのシンボル参照 |
| Relation | `display_name`, `description` | `string` |
| Relation | `type` | RelationTypeのシンボル参照 |
| Relation | `from`, `to` | Entityのシンボル参照 |
| Relation | `tags` | stringsの集合 |

Symbol-reference comparisonsはsource bindingsを使用し、StableAddressesに正規化される。

```ldl
field address == order_api
field type == application_service
field layer == application
```

Local-ID comparisonは明示的にstring comparisonである。

```ldl
field id == "order_api"
```

State predicateは現在評価中のEntityまたはRelationのStateSubjectRefを参照する。

```ldl
state system.updated_at >= $updated_since
state provenance.source.kind == api
state provenance.verified_at missing
```

標準pathは`system`と`provenance` namespaceに分かれ、exact field、型、欠損、stale、redactionの規則は詳細仕様に従う。`created_at`/`updated_at`はLayerDraw subject recordの時刻であり、業務対象の作成・更新日時ではない。業務日時はEntityType/RelationType ColumnとしてLDLに宣言する。`now`、実行時clock、filesystem timestamp、Git commit timeからの暗黙値生成は禁止する。

### 16.4 相関行述語

```ldl
where all {
  rows any types [application_service] {
    all {
      cell environment == prod
      cell status == active
      state provenance.verified_at exists
    }
  }
}
```

上記の両方のcellsは同じ行に適用される。Quantifiersは次のとおりである。

- `any`: 少なくとも1つの一致する行。
- `all`: すべての行。空テーブルではfalse。
- `none`: 一致する行がない。

`types` は `where` では適用可能なEntityTypesを、`relation_where` ではRelationTypesを制限する。選択されたtypesが同じ列を互換性のないscalar typesで定義している場合、Queryは `types` を制限しなければならない。そうでなければ検証は失敗する。

参照された列を型が持たない所有者については、そのcellはmissingである。`missing` はtrue、`exists` はfalse、その他すべてのoperatorsはfalseである。

row predicate内の`state`はowner Entity/Relationではなく、現在bindされているrow StableAddressを参照する。同じrow predicate内の`cell`と`state`は必ず同じrowへbindする。field/cell単位provenanceとaudit eventはcurrent StateQuerySnapshotの標準fieldではなく、通常Queryから参照できない。

### 16.5 State入力

```ldl
query recently_updated "Recently updated" {
  state_input required

  parameters {
    updated_since datetime required
  }

  select {}

  where all {
    state system.updated_at >= $updated_since
  }

  result [seed_entities]
}
```

StateQuerySnapshotはRuntimeがQuery開始前に1件へ固定し、評価中にheadを読み直してはならない。`required`でsnapshotが無い、Project identityが異なる、または参照対象のstate recordがstaleならQueryは診断付きで失敗する。`optional`でsnapshotが無い場合はno-state入力、互換hashを持たないrecordはそのsubjectについて全state field missingとし、stable warningを返す。Access拒否またはredactされたfieldの参照はpolicyにかかわらず失敗させ、missingへ偽装しない。record自体または個別fieldが単に存在しない場合はpolicyにかかわらず通常のmissing semanticsを使う。

State queryはcurrent system/provenance projectionだけを対象とする。Audit event列、Time Machine revision列、lock、lease、pending operation、presenceをGraph Queryへ混ぜない。変更履歴の時系列検索はAudit/Time Machine機能の責務である。

### 16.6 探索

```ldl
traverse outgoing 0..4 visit_once relations [depends_on, calls]
```

正規順序は次のとおりである。

```text
traverse <outgoing|incoming|both> <min..max>
  <error|visit_once|include_cycle_ref>
  [relations [<RelationType>...]]
```

保存されたtraversalには有限の整数最大値が必要である。`both` はincomingおよびoutgoing directed expansionを明示的に組み合わせる。任意のRelationTypeリストは `select.relation_types` を狭めてもよいが、決して広げない。

Traversal candidatesはRelationType StableAddress、Relation StableAddress、`from` StableAddress、次に `to` StableAddressの順に並ぶ。

### 16.7 結果包含

```ldl
result [seed_entities, traversed_entities, path_relations]
```

許可されるmembersは `seed_entities`、`traversed_entities`、`path_relations`、および `induced_relations` である。デフォルトは最初の3つである。Induced Relationは、適格であり、result Entity set内に両方のendpointsを持ち、traversal path edgeとして使用されなかったRelationである。
## 17. View宣言

View は Query 結果またはリビジョンの組と、recipeが明示的に要求する場合は同じStateQuerySnapshotを、1つの型付きViewData形状へ変換する。

```text
Master Graph + Query + ViewRecipe + optional immutable StateQuerySnapshot
  -> ViewData
    -> RenderData or ExportArtifact
```

Exporter は ViewData を迂回してはならない (MUST NOT)。

### 17.1 共通構文

```ldl
view production_topology "Production Topology" topology {
  intent "Review network, security, and application placement"
  source query production_network { environment: prod }

  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
  }

  export topology_svg svg "production-topology.svg" {
    fidelity visual_only
    source_refs
  }
}
```

すべての View は以下を持つ。

- ヘッダー内の安定 ID、表示名、およびカテゴリ。
- 任意の `intent`。
- shapeが直接state fieldを参照する場合は、ちょうど1つの`state_input required|optional`。
- ちょうど 1 つの source。
- ちょうど 1 つの型付き shape ブロック。
- 0 個以上の RelationType projection override。
- 0 個以上の一意に命名された export recipe。

View は、削除された `table_columns` と `exports` のために 1 つの `reserve` ブロックを含んでもよい (MAY)。現在の shape が `table` でない場合、または export が残っていない場合でも、reservation は有効なままである。
View-local reservationはcanonical ASCII identifierだけを含み、normalized wireではbyte-lexicographic昇順のsetとしてserializeする。active Export IDは`reserved_export_ids`に現れてはならない (MUST NOT)。

カテゴリは以下である。

```text
topology inventory dependency hierarchy flow impact diff context
```

shape は `diagram`、`table`、`matrix`、`tree`、`flow`、`context`、および `diff` である。カテゴリは理由を説明し、shape は ViewData 型を定義する。

`diff` の category/source/shape は同時に出現する。それ以外のすべてのカテゴリは、詳細仕様の shape 固有の materialization contract を満たす任意の非 diff shape を使用できる。

View自身の`state_input`はshape内の直接state参照だけを制御する。Runtimeはsource QueryとViewのpolicyから`required > optional > none`でsnapshot取得要否を決めるが、Query readにはQuery自身、shape readにはView自身のpolicyを適用し、両方へ同じsnapshotを渡す。state参照がないQuery/Viewへsnapshotを暗黙注入してcache identityを変えてはならない。Diff source/shapeとstate依存Queryまたは直接state参照の組み合わせは禁止する。Definition Diffへのaudit enrichmentはReview/Time Machine契約であり、このView recipeではない。

### 17.2 ソース

Queryソース:

```ldl
source query production_network { environment: prod }
```

インラインオブジェクトは Query parameter を型付き値へ対応付ける。必須引数の欠落および未知の引数は error である。

Diffソース:

```ldl
source diff "revision:2026-06-30" -> "revision:current" {
  query migration_scope
  arguments { environment: prod }
}
```

Revision selector は opaque な runtime/state reference である。`source diff` は category `diff` と shape `diff` を要求する。

### 17.3 Relation投影の上書き

```ldl
relation_projection aws.contains {
  composed {
    mode nest
    parent_endpoint from
    child_endpoint to
  }
}
```

overrideは、1つのViewについて、詳細仕様の`ProjectionOverride`で閉じたprojection fieldとrender hintだけを変更してよい。Tree/Flowのcycle policyとTreeのshared-child policyはView shapeが所有し、RelationType projectionまたはoverrideには置かない。overrideはsemantic kind、endpoint contract、cardinality、duplicate policy、row schema、またはauthored directionを変更してはならない (MUST NOT)。

有効値は詳細仕様のfallback、解決済みRelationType、View overrideの順にフィールド単位で解決する。shape fieldはprojectionへ暗黙mergeしない。importされたPack declarationはimmutableのままである。

### 17.4 Diagram形状

```ldl
diagram {
  layout layered
  direction left_to_right
  abstraction normal
  composed
  place order_api 120 80 240 120
}
```

- layout: `layered`、`force`、`grid`、`radial`、または `manual`。
- direction: `left_to_right`、`right_to_left`、`top_to_bottom`、または `bottom_to_top`。
- abstraction: `summary`、`normal`、または `detail`。
- `composed` は composed Relation projection を有効化する。
- `place <Entity> <x> <y> <width> <height>` は View presentation 専用である。

Manual layout は、すべての visible root occurrence に対する placement を要求する。他の layout は placement を stable pin として使用してよい。

1 つの placement は、ちょうど 1 つの visible root occurrence を持つ Entity のみを対象にできる。Language 1 は、derived ViewData occurrence key に対する placement を永続化しない。

### 17.5 Table形状

```ldl
table {
  rows entity_rows
  entity_types [application_service]
  entity_id
  type
  layer

  column environment {
    source attribute environment
  }

  column owner {
    source attribute owner
  }

  column updated_at {
    source state system.updated_at
  }

  sort updated_at descending nulls last
}
```

Row sourceは`entity`、`entity_rows`、`relation`、`relation_rows`、または`automatic_relations`である。`automatic_relations`だけがRelationTypeのeffective TableProjectionを使い、Relationごとにgrainとendpoint/type固定列を決める。他のrow sourceはViewがgrainを明示的に上書きする。flag `entity_id`、`type`、および`layer`はEntity系row sourceの固定Columnを含める。

Column source の形式:

```text
source field <field>
source attribute <column> [entity_types [...]] [relation_types [...]]
source relation_endpoint <from|to> <field>
source derived_count <outgoing|incoming|both> relations [<RelationType>...]
source state <system|provenance>.<field-path>
```

`source state`はTableの現在row grainに対応するstate subject addressを使う。`entity`/`relation`はそのsubject、`entity_rows`/`relation_rows`はrow subject、`automatic_relations`は実際にmaterializeされたrelationまたはrelation-row subjectを参照する。columnは`label "..."`と`aggregate <none|count|count_distinct|min|max|join_unique>`を追加してよい。State fieldの型がColumn型になる。Sort statementはauthored orderのまま保持され、state datetime/number/stringはそれぞれのtotal order、state enumは詳細仕様のregistry option orderを使い、いずれも`nulls first|last`を適用する。Row-based ViewDataはdefinition SourceRefsに加え、実効StateInputRef、subject address、state field pathをStateRefsとして保持する。

### 17.6 Matrix形状

```ldl
matrix {
  row_axis {
    entity_types [application_service]
    label display_name
  }

  column_axis {
    entity_types [database]
    label display_name
  }

  cell {
    relation_types [reads, writes]
    direction outgoing
    semantic relation_refs
    display relation_types
  }
}
```

semantic value は `relation_refs` または `path_refs` である。display value は `exists`、`count`、`relation_types`、または `attribute_summary` である。`attribute_summary`を使うcellは`attributes [<RelationType Column>...]`をちょうど1つ持ち、その他のdisplayでは`attributes`を禁止する。typed selectorは互換ColumnをStableAddress setへ解決する。すべての cell は source Relation ID または ordered path を保持する。

### 17.7 Tree形状

```ldl
tree {
  relation_types [contains, owns]
  cycle_policy truncate
  shared_child_policy duplicate_occurrence
}
```

Tree ViewData は、移動または複製された Master Entity ではなく、source-derived occurrence を含む。選択されたすべての RelationType は、曖昧でない tree projection を提供しなければならない。

### 17.8 Flow形状

```ldl
flow {
  relation_types [next, calls, sends]
  lane_by layer
  cycle_policy include_cycle_ref
  preserve_parallel
}
```

`lane_by` は `none`、`layer`、`entity_type`、または `attribute.<column>` である。Flow ViewData は branch、join、loop、parallel connector、および source ref を保持する。

### 17.9 Context形状

```ldl
context {
  group_by layer
  entity_rows
  relation_rows
  incoming
  outgoing
}
```

Context ViewData は UI、SDK、および MCP のための構造化された fact である。Markdown または prompt text は export であり、semantic source ではない。

### 17.10 Diff形状

```ldl
diff {
  include [
    project,
    pack,
    entity_type,
    relation_type,
    layer,
    entity,
    relation,
    query,
    view,
    reference,
    entity_type_column,
    entity_type_constraint,
    relation_type_column,
    relation_type_constraint,
    entity_row,
    relation_row,
    query_parameter,
    view_table_column,
    view_export,
  ]
  detect_moves
}
```

DiffはStableAddressを比較する。`detect_moves`がある場合だけリビジョン間で選択されたLDL `moves` mappingを適用し、省略時はID変更をremove/addとして扱う。include kind、Query scope、比較順序は詳細仕様に従う。heuristic identity changeは禁止されているため、`detect_renames`は意図的に存在しない。

## 18. 単純エクスポートレシピ

Export recipe header は stable ID、format、および basename を含む。

```ldl
export topology_xlsx xlsx "production-topology.xlsx" {
  fidelity traceable_summary
  source_refs
  profile composed_diagram_workbook
  lookup_sheets
  hidden_ids
  formulas
}
```

これは body fragment であり、`view` ブロック内でのみ有効である。Top-level `export` は、section 6.3 で定義された module export declaration のみであり続ける。

形式:

```text
json yaml svg png pdf html csv tsv xlsx markdown pptx docx mermaid bpmn drawio
```

`.ldl` と `.layerdraw` は document-level I/O であり、View format ではない。

Fidelity は `lossless`、`traceable_summary`、`visual_only`、または `lossy` である。要求された fidelity は shape/format capability matrix によってサポートされなければならない (MUST)。

共通 body field:

- `fidelity <value>`;
- `source_refs`フラグ。
- optional `exporter_profile "<profile-id>"`。省略時は詳細仕様のformat別canonical profileへ正規化される。

format 固有の canonical field:

| 形式 | フィールド / フラグ |
| --- | --- |
| SVG/PNG | `width`, `height`, `scale`, `background` |
| PDF | `page_size`, `orientation`, `fit`, `legend` |
| XLSX | `profile`, `lookup_sheets`, `hidden_ids`, `formulas`, `view_data_json` |
| CSV/TSV | `bundle`, `header`, `source_manifest` |
| JSON/YAML | `diagnostics`, `state_summary` |
| HTML | `interactive`, `embed_assets` |
| PPTX/DOCX | `page_size`, `orientation`, `fit`, `legend` |
| Markdown/Mermaid/BPMN/draw.io | `source_manifest` |

flag は true を意味し、不在は false を意味する。未知の option は error である。filename は canonical extension を持つ basename でなければならない。path separator と parent traversal は禁止される。
Export recipe ID と正規化された case-sensitive filename は、それぞれ 1 つの View 内で一意でなければならない (MUST)。複数の View を 1 つの directory へ export する host は、View-address-derived directory を使用するか、明示的な collision check を行わなければならない。別の artifact を黙って上書きしてはならない (MUST NOT)。
正規化済みrecipeでは`id`はtyped `address`の末尾IDと一致し、`format`、`options.kind`、および`exporter_profile.format`は同一でなければならない (MUST)。`extension`はformatのcanonical extensionと一致し、`filename`はそのexact suffixを持たなければならない (MUST)。Exporter profile IDは`[a-z0-9][a-z0-9._/-]*@[1-9][0-9]*`に一致しなければならない (MUST)。

XLSX profileは`type_workbook`、`diagram_workbook`、`composed_diagram_workbook`、`matrix_workbook`、`tree_workbook`、`impact_workbook`、`flow_workbook`、`diff_workbook`、`context_workbook`、および`diagram_inventory_workbook`である。省略時のshape別profileは詳細仕様で一意に決める。

Plain export は 1 つの ViewData を別の媒体で表現する。Language 1は決定的なExportPlan、fidelity、Source Manifestを定義し、visual artifactのfont/layout/rasterization/format serializerはversioned exporter profileへ委ねる。state依存ViewDataのplain exportはfidelityやrecipe flagにかかわらずSource Manifestを伴い、利用したsnapshotまたはoptional no-state入力を束縛する。Source ManifestだけではMaster Graphからの再materializeを保証せず、完全再実行には同じStateQuerySnapshot本体を持つ`.layerdraw`が必要である。Multi-View business document generation は別機能である。

Language 1のYAML plain exportは一般的なpretty YAMLではなく、YAML 1.2が受理するRFC 8785 canonical JSON subset bytesを`.yaml`で出力する。human-oriented YAML emitterはLanguage 1のlossless canonical profile外である。

## 19. Reference宣言

Reference は、人間および MCP client のための first-class な natural-language guidance である。

```ldl
reference operating_rules <<-TEXT
  Update the Master Graph instead of exported files.
  Use containment Relations instead of Entity parent fields.
TEXT
```

Reference は stable ID と text のみを持つ。Entity、Relation、または field ごとには attach されない。Documentation comment は declaration を説明する。Reference は発見可能な project または pack guidance を提供する。

MCP surface は `list_references` によって Reference StableAddress と local ID を列挙し、`read_references` によって要求された Reference StableAddress の正確な text を返さなければならない (MUST)。選択された Reference は normalized definition hash に参加するが、graph hash には参加しない。

## 20. アセット

Asset path は、宣言元 source module からの相対 stringである。これはauthoring locatorであってsemantic identityではなく、正規化時にcontent digestとmedia typeを持つ`AssetRef`へ解決される。

```ldl
image "../../assets/application-service.png"
```

解決順序:

1. 宣言元 project または pack source origin。
2. 対応する `.layerdraw` `assets/` または `pack/` entry。
3. 宣言された icon。
4. representationのフォールバック。

path は origin 内に留まらなければならない (MUST)。remote URLはimage pathとして禁止し、compile/open時にfetchしてはならない (MUST NOT)。Committed Revisionでmissing、digest不一致、unsupported、またはunsafeなassetはvalidation errorであり、Working Document previewだけが宣言されたicon、次にrepresentationへfallbackできる。asset path自体はsubject identityに寄与せず、解決済みcontent digestがEntityType semanticsとhashへ寄与する。

packaging は、すべての参照済みproject-local assetとimmutable expanded pack treeを含まなければならない (MUST)。Registryまたはhost cacheへのruntime fallbackは禁止される。asset manifestはorigin-relative packaging path、raw byte digest、media type、byte lengthを記録し、normalized `AssetRef`からcontentを一意に取得できなければならない。

## 21. 解決、型付け、および正規化

compilation stage は deterministic である。

1. UTF-8 を decode し、lossless syntax tree を構築する。
2. module specifier を解決し、pack digest を検証する。
3. import と export を bind する。
4. grouped item symbol を含む declaration symbol table を構築する。
5. effective document closure を選択する。
6. EntityType、RelationType、Layer、endpoint、Query、View、および Reference dependency を解決する。
7. scalar value、row、predicate、shape field、および export option を type-check する。
8. graph constraint を validate する。
9. normalize し hash する。
10. generated semantic index を更新する。

name resolution は、normalized output 内のすべての semantic source binding を、その structured StableSymbol または canonical StableAddress に置換しなければならない (MUST)。import alias、install name、および module path は source/index metadata のままであり、semantic identity として serialize されてはならない (MUST NOT)。複数の compatible column を意図的に解決する typed selector は、canonically ordered StableAddress set を格納する。

logical normalized project は、必須の巨大な in-memory object ではなく、partition 可能な collection である。

```text
NormalizedProjectDefinition
  schema index
  Layer index
  Entity partitions
  Relation partitions
  attribute-row partitions
  Query / View definitions
  Reference index
```

上のpartition表現はlogical collection境界であり、in-memory object layout、cache、incremental invalidation、streaming、database indexを規定しない。language semanticsは、正規結果が同じである限りfull materializationもincremental evaluationも許可し、特定の実装戦略を要求しない。

正確な normalized envelope、required/optional field、ordering、default、source/index boundary、および schema version は詳細仕様で定義される。

## 22. 検証と診断

Diagnostic は少なくとも以下を持つ。

```text
code
severity: error | warning | info
stable message_key と typed arguments
optional localized message
source origin（project sentinelまたはPack root StableAddress）とorigin-relative module path
start/end byte range
subject StableAddress when available
owner StableAddress for owner-scoped subjects when available
related ranges
```

source compile、View materialization、export validation、またはsemantic operationの公開前validationで返るerrorは、その処理が要求した新しいCommitted Revisionまたはartifactの公開を妨げる。公開後のstate/recovery protocol errorは既存revisionを遡及的に無効化せず、詳細仕様のOperationResult statusで扱う。必須 validation class には以下が含まれる。

- 字句構文と構造構文。
- 未対応の言語値。
- unknown、duplicate、reserved、または ambiguous symbol。
- active/reserved/move identity collision、invalid move chain、または invalid owner/child move kind。
- import/export cycle、escape、または digest failure。
- 無効なProject・Layer・型・グループのヘッダー。
- Entity所有者の型またはLayerの不一致。
- owner/row module separation または cross-origin row augmentation。
- Relation endpoint、self、duplicate、cardinality、または direction error。
- missing、duplicate、または generated stable row ID。
- unknown、duplicate、incompatible、または invalid row column/value。
- active/reserved enum value collision または invalid default。
- 一意制約違反。
- invalid Query parameter、predicate、row correlation、または traversal bound。
- 無効なViewカテゴリ・ソース・形状の組み合わせ。
- 未知または型不一致のstate field path、state参照に対する`state_input`欠落、必須snapshot欠落、stale state record、または拒否/redactされたstate field。
- 未解決の投影エンドポイント対応。
- 未対応のエクスポート形式・形状・忠実度・オプション。
- 未知のReference。
- escaping または missing required asset。
- 許可された`state_input`、state predicate、Table `source state`以外のstate値/storage field、またはLDL内で見つかったcredential、revision、backend field。
- edit operation 中の stale revision または own-subject semantic hash。

Language 1 では、未知の field、block、modifier、enum value、および option は error である。compiler は forward compatibility のためにそれらを無視してはならない (MUST NOT)。

## 23. 決定性とハッシュ

同一の実効source bytes、resolved pack tree、Query argument、およびstate依存評価では同一のStateInputRefと対応するStateQuerySnapshotが与えられた場合、すべての適合implementationはbyte-equivalentなnormalized JSON、ordered diagnostic identity、QueryResult、ViewData semantic payload、SourceRefs、およびStateRefsを生成しなければならない (MUST)。`optional`でsnapshotが無い場合のStateInputRefは規範的なno-state sentinelであり、hostごとの現在stateへfallbackしてはならない。localized diagnostic messageだけは比較対象外である。exportへ明示添付するExternalStateSummaryはStateQuerySnapshotと別入力で、portable再実行の代替にならない。compilerの実装言語、実装者、version、build、および実行形態は規範入力ではなく、結果差を正当化してはならない (MUST NOT)。

filesystem enumeration、source discovery order、locale、timezone、randomness、network availability、および process identity は結果に影響してはならない (MUST NOT)。

正規化規則:

- string は Unicode NFC と LF に normalize する。
- JSON object keyはRFC 8785のUTF-16 code unit lexicographic orderでsortする。LDLのsemantic setをarrayへ直列化する順序とは別である。
- semantically unordered declaration は StableAddress で sort する。
- row は row StableAddress で sort する。
- validated tag、selector set、reservation、およびその他の semantic set は canonical に sort する。duplicate は黙って削除されるのではなく error である。
- column、View sort statement、およびその他の presentation-significant sequence は authored order を保持する。
- finite number は shortest round-trippable decimal を使用する。
- absent optional value は absent のままであり、決して `null` にならない。
- 意味上のdefaultは詳細仕様に従ってnormalized JSONへ必ずmaterializeする。source formatterはdefaultをsourceへinjectせず、明示的なmaterialization operationだけがsourceを書き換えられる。したがって、省略形とdefaultを明記したsourceは同じnormalized semanticsとsemantic hashを持つ。
- normalized JSON は LDL 固有の normalization の後に RFC 8785 を使用する。

ハッシュ:

| ハッシュ | 対象 | 目的 |
| --- | --- | --- |
| ソースファイルダイジェスト | 正確なバイト列 | 完全性検証 |
| 正規化定義ハッシュ | 選択されたすべての宣言とReference | 再現可能な文書識別 |
| グラフハッシュ | 型、Layer、Entity、Relation、行 | グラフ変更の検出 |
| Queryハッシュ | Query、参照スキーマ、input graph、argument、StateInputRef | QueryResultキャッシュ |
| StateQuerySnapshotハッシュ | Query可能なcurrent system/provenance projection | state入力の固定と検証 |
| Viewハッシュ | グラフ・Query依存関係、StateInputRefとView | ViewDataキャッシュ |
| ViewDataハッシュ | complete typed ViewData | export artifactとSource Manifestの入力束縛 |
| 単一対象の意味ハッシュ | 1対象の正規化フィールド、参照、予約 | 対象指定の同時実行制御とstate照合 |
| サブツリー意味ハッシュ | 所有者ハッシュとアドレス指定可能な子要素のハッシュ | 依存関係の無効化とキャッシュ束縛 |

comment は semantic hash から除外されるが、source byte digest には含まれる。Reference text の変更は definition hash を変更するが、graph hash は変更しない。
formatting、comment-only change、module move、import alias rename、および pack install-name change は source digest/index metadata を変更し得るが、resolved StableSymbol と semantics が不変である場合、normalized definition、graph、Query、View、own-subject、または subtree hash を変更してはならない (MUST NOT)。

すべてのStableSymbolはown-subject semantic hashを持つ。これは、そのsubjectのnormalized field、StableAddressとしてのreference、および直接所有されるreservation setを含むが、別個にaddressableなchild payloadは除外する。すべてのownerは、自身のown-subject hashに加えて、ordered `(child StableAddress, child hash)` pairに対するsubtree semantic hashも持つ。childがさらにaddressable childを持つ場合はそのsubtree hash、leafならown-subject hashを使い、Project rootから全project-local subjectへ再帰的に閉じる。Graph、Query、およびView dependency hashは、child semanticsがexecutionに影響する場合にsubtree hashを使用する。

Project root own-subject hash は Project field、top-level reservation、および move を対象とする。Pack root own-subject hash は canonical manifest semantic field、pack-wide reservation、および move を対象とする。release version、Registry location、signature bytes、および artifact digest は identity の外側にある resolution/integrity metadata のままであり、resolved dependency digest が正確に installed content を bind する。

Semantic operationは、直接対象となる各existing subjectのown-subject hashを比較する。owner-scoped childのcreate/deleteはbase revisionとcurrent owner child-address setも比較し、addressable childを持つownerの削除はsubtree hashも比較する。これにより、無関係なrowまたはColumn changeをsame-subject scalar conflictとして扱うことなくdisjoint child editをmergeし、owner削除とchild更新は確実に競合させる。

すべての semantic hash は、詳細仕様で定義される SHA-256 domain-separated preimage と canonical payload を使用する。

## 24. 正準フォーマット

### 24.1 単一の正準表層構文

Language `1` は 1 つの authoring syntax を持つ。代替 alias が存在しないため、formatter は verbose alias と compact alias のどちらかを選択してはならない (MUST NOT)。

formatter は以下を行わなければならない (MUST)。

- BOM なし UTF-8 と LF を emit する。
- 2-space indentation を使用する。
- assignment `=` または semicolon を emit しない。
- すべての display string とすべての `string`、`date`、または `datetime` value を quote する。enum value は canonical non-keyword identifier の場合のみ bare で emit する。
- required display name を quote したままにする。
- canonical declaration field/modifier order を emit する。
- body block を持たない限り、1 つの Layer、Entity、Relation、または row item を 1 logical line に置く。
- group boundary と declaration order を保持する。
- comment と `///` / `//!` attachment を保持する。
- alias を変更せずに named import/export item を lexical に sort する。
- reservation ID、reserved enum value、および move entry を canonical kind/owner/source-value order で sort する。
- column order、View sort order、およびその他の order-significant list を保持する。
- trailing comma は multiline list/object でのみ emit する。
- byte-idempotent である。

deterministic line width、wrapping、blank-line、comment、heredoc、および invalid-Working-Document rule は詳細仕様で定義される。

optional field は skip されるが、残りの field group はこの canonical body order を使用する。

| 所有者 | 正準フィールド / ブロック順序 |
| --- | --- |
| Project | `description`, `tags`, `annotations` |
| Layer項目 | `description`, `tags`, `annotations` |
| EntityType | `icon`, `image`, `color`, `representation`, `columns`、反復可能な`unique`, `reserve`, `description`, `tags`, `annotations` |
| RelationType | `allow_self`, `duplicate_policy`, `from`, `to`, `cardinality`, `label`, `reverse`, `columns`、反復可能な`unique`, `reserve`, `traversal`、反復可能な`projection`、反復可能な`render`, `export`, `description`, `tags`, `annotations` |
| Entity項目 | `description`, `tags`, `annotations`, `reserve_rows` |
| Relation項目 | `description`, `tags`, `annotations`, `reserve_rows` |
| Query | `state_input`, `reserve`, `parameters`, `select`, `where`, `relation_where`, `traverse`, `result`, `description`, `tags`, `annotations` |
| View | `intent`, `state_input`, `reserve`, `source`、反復可能な`relation_projection`、型付き形状、反復可能なViewエクスポート、`description`, `tags`, `annotations` |
| Table形状の列 | `source`, `label`, `aggregate` |
| Viewエクスポート | `fidelity`、`source_refs`、`exporter_profile`、続いて18節に列挙した順序の形式固有フィールド |
| 最上位`reserved` | `entity_types`, `relation_types`, `layers`, `entities`, `relations`, `queries`, `views`, `references` |
| ルート`moves` | `project`, `entity_type`, `relation_type`, `layer`, `entity`, `relation`, `query`, `view`, `reference`、続いて7.5.1節の表順の子移動種別 |
| 型の`reserve` | `columns`, `constraints` |
| Queryの`reserve` | `parameters` |
| Viewの`reserve` | `table_columns`, `exports` |

repeated entry は、その owning section が set ordering を明示的に定義していない限り、authored order のままである。Projection primitive block は `composed`、`diagram`、`table`、`matrix`、`tree`、`flow`、`context` を使用する。render primitive block は `edge`、`nested`、`overlay`、`badge` を使用する。canonical group position の外側に出現する field は parser に受け入れられるが、semantic validation の成功後に formatting によって移動される。

```text
format(format(source)) == format(source)
```

formatting は以下を行ってはならない (MUST NOT)。

- stable ID を create、change、または remove する。
- reference または Relation direction を変更する。
- declaration を file 間で移動する。
- non-adjacent group を merge する。
- kind ごとに group 化するためだけに fact item を reorder する。
- side effect として unchanged file を rewrite する。

### 24.2 フォーマット境界

| 境界 | 必須動作 |
| --- | --- |
| open/parse | rewrite せず diagnose する |
| GUI/MCP/SDK structured operation | canonical complete syntax node を create する |
| raw text edit | Working Document を保持し、parse 成功後に complete affected node を format する |
| editor save | format-on-save が有効な場合、affected file を format する |
| explicit format | 1 つの revision-protected source transaction |
| checkpoint | changed file のみを canonicalize する |
| `.layerdraw` export / Registry publish | canonical valid Committed Revision を require する |

collaborative host は、keystroke ごとに whole file を format してはならない (MUST NOT)。

### 24.3 ワークスペースの整理

Workspace organization は formatting とは別である。これは declaration を standard layout へ移動し、group を split/join し、import/export を update し、asset を relocate してよい。

これは explicit、atomic across files、revision-protected、previewable、semantic-preserving、comment-preserving、かつ all-or-nothing でなければならない (MUST)。Entity または Relation を移動するときは、owner/module co-location invariant が true のままになるよう、すべての owner row group も移動する。Pack-origin declaration は immutable のままである。

## 25. 共同編集

### 25.1 作業中状態とコミット済み状態

```text
Working Document
  -> parse / resolve / validate
  -> canonical checkpoint
  -> Committed Revision
```

Working Document は、一時的に incomplete syntax、duplicate ID、unresolved reference、または invalid row を含んでもよい (MAY)。diagnostic は visible のままであるが、その state は successful ordinary query target、portable export、または Registry artifact として exposed されてはならない (MUST NOT)。

Committed Revision は以下を満たさなければならない (MUST)。

- parent/base revision を identify する。
- complete project-local source manifest を bind する。
- 正確な `layerdraw.resolved.json` と expanded dependency tree digest を bind する。
- すべての validation に pass する。
- canonical changed source bytes を含む。
- normalized definition と changed own-subject/subtree hash を record する。
- immutable である。

Ordinary Query、View、render、export、および read-only MCP operation は latest Committed Revision を使用する。editor は diagnostic と revision status が exposed される場合のみ Working Document を preview してよい (MAY)。

### 25.2 意味編集操作

Collaborative runtime は以下と等価な revision-protected operation をサポートする。

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

existing named subject を対象とするすべての operation は、その StableAddress と expected own-subject semantic hash を運ぶ。`create_subject` は、owner-scoped child を作成するときに owner StableAddress、新しい`subject_kind`とlocal ID、および identity に影響しない source placement hint を運ぶ。operation value 内の reference は module-local alias ではなく StableAddress を使用する。field update は section 7.7 で指定される schema-defined field path を使用する。Entityの`layer_address`は`move_entity_to_layer`、Relationの`from_address`/`to_address`は`update_relation_endpoint`だけで変更し、汎用field updateで迂回してはならない。任意の JSONPath と durable ordinal addressing は禁止される。

operation payload、transactional overlay evaluation、conflict class、precondition、および identity side effect は詳細仕様に従う。

`upsert_row`はEntityまたはRelation owner StableAddress、authored row ID、Column StableAddress keyed value map、defaultを抑止するoptional Column setを受け取り、row全体を置換または作成する。`delete_row`はrow StableAddressを受け取る。`create_relation`はRelation専用のtyped operationであり、そのRelationTypeとendpoint valueはStableAddressである。delete operationはreservationをmaterializeし、rename operationは同じatomic source transaction内でsection 7.5によって要求される`moves` entryをmaterializeする。

`delete_subject` は implicit semantic cascade を行わない。referenced type、Layer、Entity、Relation、Query、column、parameter、または View export を削除する batch は、dependent source binding と row value のすべてを削除または migrate しなければならず、そうでなければ validation は batch を reject する。owner を削除すると、その owner deletion の一部として addressable child が削除され、owner の top-level ID が reserve され、到達不能な child reservation を LDL へ copy するのではなく、child tombstone が audit/state に記録される。Host Project/Document deletion は host operation であり、LDL Project declaration 上の `delete_subject` ではない。

wire algorithm は command log、CRDT、OT、または別の adapter を使用してよい (MAY)。これは LDL validation と identity semantics を保持しなければならない (MUST)。

operation batch は 1 つの project revision について atomic である。Rename は選択されたすべての project-local reference を update するか、失敗する。rename targetの親child-set、owner subtree、および書換え対象となる全参照元subjectは詳細仕様のexpected hashで保護する。Move は semantic Layer を変更する。optional standard-layout relocation は同じ workspace transaction 内で起こる。

disjoint subject/field edit は merge してよい (MAY)。Stale base revision/hash、same-scalar concurrent write、delete-versus-update、duplicate ID、および incompatible schema/row edit は、silent overwrite ではなく、deterministic valid merge または structured conflict を生成しなければならない (MUST)。

revision history、actor data、operation log、lock、lease、presence、cursor、focus、および transport state は LDL の外側に留まる。

## 26. 規模とリソース制限

LDL は任意の finite graph を表現する。この language は module、Layer、type、Entity、Relation、row、Query、View、または Reference に固定 count limit を設定しない。

implementation は source bytes、nesting、import、declaration、row、heredoc、Query result size、および rendering に resource policy を設定してよい (MAY)。limit は discoverable でなければならず (MUST)、truncation や partial compilation なしに明示的に fail しなければならない (MUST)。

停止規則:

- saved traversal は常に finite maximum depth を持つ。
- `visit_once` は finite graph 上で terminate する。
- path enumeration は finite depth と host result limit を要求する。
- module import cycle は error である。Language 1 は recursive Query または View composition を持たない。
- Master Graph Relation cycle は、RelationType/View contract が禁止しない限り合法である。

compiler と host は ZIP slip、source-origin escape、executable pack、credential resolution、および implicit network access を防がなければならない (MUST)。

## 27. 可搬性とstate境界

| 共有物 | 再現可能性 |
| --- | --- |
| importとimage参照を持たない単一の`.ldl` | 完全なdefinition。stateは含まない |
| image参照を持つ単一の`.ldl` | sourceだけでは不完全。参照asset treeを同伴する必要がある |
| エントリとProjectのモジュール・Pack・アセット・解決情報 | 完全なディレクトリ定義 |
| 必須Packが欠けたLDL | 未解決import診断を伴う不完全な定義 |
| LDLと`.ldstate.json` | 定義とstateスナップショット |
| `.layerdraw` | 可搬なソースツリー、Pack、アセット、および任意のstate・成果物 |

stateなしでLDLを開くとfreshness/provenanceはunknownになる。state非依存Query/Viewは通常どおり評価できる。`state_input required`を持つQuery/Viewは規範diagnosticで失敗し、`optional`はno-state sentinelを使ってstate fieldをmissingとして評価する。Query/View recipe自体はLDLに残るため、stateを再接続すれば同じrecipeを再実行できる。editingはsystem fieldをLDLへinjectせず、configured stateをupdateする。

LDL export は pack declaration を inline して canonical origin を消去してはならない (MUST NOT)。flattened review artifact は存在してよい (MAY) が、canonical editable LDL ではない。

## 28. 旧仕様からの移行

assignment field、singular Entity/Relation block、implicit type/Layer header、duplicated Entity hierarchy field、または verbose predicate sub-block を使用する pre-canonical syntax はすべて legacy input である。

migration importer は以下を行わなければならない (MUST)。

1. explicit legacy grammar selection を要求する。
2. heuristic fallback なしで parse する。
3. missing EntityType/RelationType placeholder を visible に materialize する。
4. Entity hierarchy field を explicit containment Relation へ変換する。
5. stable row ID を assign し write する。
6. legacy View mode を category、Query source、および typed shape へ変換する。
7. すべての Relation について deterministic direction を要求する。
8. Entity、row、Relation、および Relation-row fact を canonical compact declaration へ group 化する。
9. 既知の deleted ID を reservation として、既知の rename を推測なしで `moves` として materialize する。
10. canonical formatter を通じてのみLanguage 1の正準LDLをemitする。
11. operational migration detail を LDL の外側に保持する。

Language 1 parser は、legacy form を推測するのではなく reject しなければならない (MUST)。

## 29. 標準レイアウトの完全な例

### 29.1 インストール済みPackモジュール

```ldl
// ファイル: pack/aws_complete/modules/network.ldl
//! 再利用可能な AWS network schema。

entity_type vpc "VPC" {
  image "../assets/vpc.png"
  color "#2F6B62"
  representation container
  columns {
    environment "Environment" enum [prod, stg, dev] required
    cidr "CIDR" string required format cidr
  }
}

entity_type subnet "Subnet" {
  image "../assets/subnet.png"
  color "#5B7C99"
  representation container
  columns {
    cidr "CIDR" string required format cidr
  }
}

relation_type contains "Contains" containment {
  duplicate_policy deny_same_type_between_same_endpoints
  from container types [vpc, subnet]
  to contained types [subnet]
  cardinality {
    to_per_from 0..*
    from_per_to 0..1
  }
  label "contains"
  reverse "is contained by"
  traversal {
    default_direction outgoing
    participates_in_impact true
    participates_in_hierarchy true
  }
  projection composed {
    mode nest
    parent_endpoint from
    child_endpoint to
    conflict diagnostic
  }
  projection tree {
    parent_endpoint from
    child_endpoint to
  }
  render nested {
    frame_label parent
    frame_style subtle
  }
}

reference network_usage <<-TEXT
  Use contains from VPC to Subnet.
  Do not represent containment with Entity parent fields.
TEXT

export { vpc, subnet, contains, network_usage }
```

### 29.2 Packエントリ

```ldl
// ファイル: pack/aws_complete/pack.ldl
//! 公開 pack entry。

export * from "./modules/network.ldl"
```

### 29.3 ProjectのLayer

```ldl
// ファイル: layers/layers.ldl
layers {
  application "Application" @20
  network "Network" @40
}

export { application, network }
```

### 29.4 ProjectローカルのEntityType

```ldl
// ファイル: schema/entity_types/application.ldl
entity_type application_service "Application Service" {
  icon "app-window"
  color "#8A5A44"
  representation shape rounded
  columns {
    environment "Environment" enum [prod, stg, dev] required
    owner "Owner" string
  }
}

export { application_service }
```

### 29.5 ProjectローカルのRelationType

```ldl
// ファイル: schema/relation_types/deployment.ldl
import aws from "aws_complete"
import { application_service } from "../entity_types/application.ldl"
import { application, network } from "../../layers/layers.ldl"

relation_type deployed_to "Deployed To" deployment {
  from workload types [application_service] layers [application]
  to placement types [aws.subnet] layers [network]
  cardinality {
    to_per_from 0..1
    from_per_to 0..*
  }
  label "is deployed to"
  reverse "hosts"
  projection composed {
    mode nest
    parent_endpoint to
    child_endpoint from
  }
}

export { deployed_to }
```

### 29.6 Network Layerの事実

```ldl
// ファイル: layers/network/network.ldl
import aws from "aws_complete"
import { network } from "../layers.ldl"

entities aws.vpc @network {
  production_vpc "Production VPC"
}

rows aws.vpc [environment, cidr] {
  production_vpc primary: prod, "10.0.0.0/16"
}

entities aws.subnet @network {
  private_subnet "Private Subnet"
}

rows aws.subnet [cidr] {
  private_subnet primary: "10.0.2.0/24"
}

relations aws.contains {
  vpc_contains_private_subnet: production_vpc -> private_subnet
}

export { production_vpc, private_subnet, vpc_contains_private_subnet }
```

### 29.7 Application Layerの事実

```ldl
// ファイル: layers/application/application_services.ldl
import { application_service } from "../../schema/entity_types/application.ldl"
import { deployed_to } from "../../schema/relation_types/deployment.ldl"
import { application } from "../layers.ldl"
import { private_subnet } from "../network/network.ldl"

entities application_service @application {
  order_api "Order API" {
    tags [critical]
  }
}

rows application_service [environment, owner] {
  order_api production: prod, "Commerce Platform"
}

relations deployed_to {
  order_api_deployment: order_api -> private_subnet
}

export { order_api, order_api_deployment }
```

### 29.8 QueryとView

```ldl
// ファイル: views/production_topology.ldl
import aws from "aws_complete"
import { application, network } from "../layers/layers.ldl"
import { application_service } from "../schema/entity_types/application.ldl"
import { deployed_to } from "../schema/relation_types/deployment.ldl"
import { production_vpc, private_subnet } from "../layers/network/network.ldl"
import { order_api } from "../layers/application/application_services.ldl"

query production_scope "Production Scope" {
  select {
    layers [application, network]
    entity_types [aws.vpc, aws.subnet, application_service]
    relation_types [aws.contains, deployed_to]
    roots [production_vpc]
  }
  traverse both 0..3 visit_once
}

view production_topology "Production Topology" topology {
  intent "Review application placement inside the production network"
  source query production_scope {}
  diagram {
    layout layered
    direction left_to_right
    abstraction normal
    composed
  }
  export topology_svg svg "production-topology.svg" {
    fidelity visual_only
    source_refs
  }
  export topology_xlsx xlsx "production-topology.xlsx" {
    fidelity traceable_summary
    source_refs
    profile composed_diagram_workbook
    lookup_sheets
    hidden_ids
  }
}

export { production_scope, production_topology }
```

### 29.9 Referenceとエントリモジュール

```ldl
// ファイル: references/operating_rules.ldl
reference operating_rules <<-TEXT
  Update the Master Graph instead of exported files.
  Rebuild Views after changing placement or containment.
TEXT

export { operating_rules }
```

```ldl
// ファイル: document.ldl
//! production retail platform の source of truth。

import { application, network } from "./layers/layers.ldl"
import { application_service } from "./schema/entity_types/application.ldl"
import { deployed_to } from "./schema/relation_types/deployment.ldl"
import { production_vpc, private_subnet, vpc_contains_private_subnet } from "./layers/network/network.ldl"
import { order_api, order_api_deployment } from "./layers/application/application_services.ldl"
import { production_scope, production_topology } from "./views/production_topology.ldl"
import { operating_rules } from "./references/operating_rules.ldl"

project retail_platform "Retail Platform" {
  description "Production order platform"
}
```

## 30. コンパイラ適合性

grammar と declaration schema は、canonical compiler を validate するために使用される 1 つの machine-readable source を持たなければならない (MUST)。line-oriented regular-expression parser は conforming ではない。Host client は generated type と protocol binding を expose してよいが、別の LDL parser または semantic implementation を定義してはならない (MUST NOT)。

Golden fixture は以下を cover しなければならない (MUST)。

- valid parsing と lossless CST round-trip。
- Language 1でのすべての legacy alternate form の rejection。
- formatter idempotence と canonical compact output。
- グループ化されたEntity・行・Relation・Relation行の宣言。
- module/import/export と pack closure。
- project、pack、およびすべての owner-scoped child kind のための structured StableSymbol construction と canonical StableAddress serialization。
- alias/module/version independence、host-document scoping、identity reservation、rename、copy、および migration。
- `moves` chain normalization、implicit reservation、owner-subtree rename、cycle/fan-in/fan-out rejection、および history-aware deletion check。
- owner/row module co-location と complete owner export closure。
- scalar、column、stable row、uniqueness、および missing-cell rule。
- JSON-safe integer bound、binary64 normalization、Gregorian date、および UTC millisecond datetime normalization。
- enum option removal、`reserve_values`、default compatibility、および history-aware value-reuse rejection。
- Relationのエンドポイント・カーディナリティ・重複・投影規則。
- Query predicate、row correlation、finite traversal、および parameter。
- すべての View shape と export capability。
- Referenceの列挙。
- normalized JSON、semantic index、および hash。
- targeted concurrent edit と structured conflict。
- canonical compiler を呼び出すすべての SDK、server、editor、および MCP host を通じた equivalent result と diagnostic。

Generated normalized JSON schema と semantic index schema は、equivalent semantics を保持しながら、人間向け LDL syntax とは独立して versioning される。

完全な Language 1 freeze criteria は詳細仕様に列挙される。

## 31. 関連契約

- [blueprint.md](blueprint.md): product architecture と delivery boundary。
- [architecture.md](architecture.md): Go Engine、TypeScript presentation、framework shell、およびdelivery artifactの実装境界。
- [compiler-architecture.md](compiler-architecture.md): canonical Compiler / Workbenchの閉じた入出力とbuild target。
- [ai-integration.md](ai-integration.md): MCPのscoped read、semantic operation、scoped LDL fragment、およびcontext budget。
- [ldl-language-detailed-specification.md](ldl-language-detailed-specification.md): normative normalized、evaluation、formatting、hash、diagnostic、および operation semantics。
- [entity-type-system.md](entity-type-system.md): EntityTypeのプロダクト動作。
- [relation-type-system.md](relation-type-system.md): RelationType product behavior と LadybugDB mapping。
- [view-conversion-contract.md](view-conversion-contract.md): Master Graph から ViewData、artifact への contract。
- [views-and-projections.md](views-and-projections.md): View と no-code Query UX。
- [registry-and-templates.md](registry-and-templates.md): Registry、pack、および template behavior。
- [system-fields-and-provenance.md](system-fields-and-provenance.md): authored definition と state。
- [state-backends.md](state-backends.md): backend binding と state contract。

## 32. 設計上の参照資料

この design は、完全な data model を copy することなく、確立された idea を借用している。

- Terraform/HCL: declarative definition/state separation、resource addressing、および explicit moved mapping。
- Pulumi: logical name、canonical URN、physical name、および provider ID separation。
- Turtle: graph fact のための repeated-context grouping。
- GraphQL: typed selection、fragment、および introspection-oriented tooling。
- CUEとGo modules: 明示的で再現可能なモジュール解決。
- Protocol Buffers: stable name、reservation、および schema evolution。
- Tree-sitter: 情報を失わず、エラー耐性を持つ増分構文ツリー。
- Language Server Protocol: hierarchical symbol と lazy workspace resolution。
- CEL: general-purpose execution のない bounded typed predicate。
- SysML v2: presentation から分離された textual semantics。
- RFC 8785: JSONの正準直列化。

主要な参照資料:

- <https://www.w3.org/TR/turtle/>
- <https://spec.graphql.org/September2025/>
- <https://tree-sitter.github.io/tree-sitter/>
- <https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/>
- <https://developer.hashicorp.com/terraform/language/state>
- <https://developer.hashicorp.com/terraform/cli/state/resource-addressing>
- <https://developer.hashicorp.com/terraform/language/block/moved>
- <https://www.pulumi.com/docs/iac/concepts/resources/names/>
- <https://cuelang.org/docs/reference/modules/>
- <https://protobuf.dev/programming-guides/proto3/#deleting-fields>
- <https://cel.dev/overview/cel-overview>
- <https://www.omg.org/spec/SysML/2.0>
- <https://www.rfc-editor.org/rfc/rfc8785.html>
