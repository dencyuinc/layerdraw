# エンティティ型システムとビュー合成

status: EntityTypeの設計背景とproduct behavior。本書は規範的なLDL grammarを再定義しない。正確なfield、validation、module、identity、migration規則は[ldl-language-specification.md](ldl-language-specification.md)、defaultsとnormalized semanticsは[ldl-language-detailed-specification.md](ldl-language-detailed-specification.md)に従う。

本書のcamelCase TypeScript interfaceはhost-language binding例であり、canonical normalized/wire fieldは規範仕様のlower_snake_caseへadapterで変換する。

## 0. 動機

現状のエンティティは種類があるように見えて全て同一構造で、プリセットは実体のないラベルに過ぎない。
LayerDraw の価値は「任意のドメイン型(クラス)を定義し、属性を持つ実体(インスタンス)として多層に配置し、
レイヤの組合せから図(ビュー)を自動生成できる」ことにある。本仕様はその背骨を定義する。

## 1. ドメインモデル（言語非依存）

### 1.1 EntityType (クラス)

クラス = メタデータ + アイコン + 描画方法 + **アトリビュート列定義(データ形式)**。

```ts
type StableAddress = string;

interface EntityType {
  id: string;                  // authored local ID
  address: StableAddress;
  displayName: string;         // 表示名 (例: "サブネット")
  description?: string;
  icon?: string;               // host-supported icon 名
  image?: string;              // 正方形画像 asset 参照 (例: "assets/aws-ec2.png")
  color?: string;              // アクセント色
  representation: Representation;
  columns: AttributeColumn[];  // インスタンスに付く CSV の列定義
  uniqueConstraints?: AttributeUniqueConstraint[];
  reservedColumns?: string[];
  reservedConstraints?: string[];
  tags?: string[];
  annotations?: Record<string, string>;
}

interface AttributeColumn {
  id: string;                  // owner-scoped authored local ID
  address: StableAddress;
  displayName: string;         // 表示ラベル (例: "CIDR")
  valueType: "string" | "integer" | "number" | "boolean" | "enum" | "date" | "datetime";
  options?: string[];          // enum のとき
  reservedOptions?: string[];  // removed enum values; reuse forbidden
  required?: boolean;          // 行において空を許さない
  default?: AttributeScalar;   // explicit materialization時だけsourceへ書く
  format?: "uri" | "email" | "hostname" | "ipv4" | "ipv6" | "cidr";
  min?: number;
  max?: number;
  minLength?: number;
  maxLength?: number;
}

interface AttributeUniqueConstraint {
  id: string;
  address: StableAddress;
  columns: StableAddress[];
}

type Representation =
  | { kind: "container" }                     // 枠。他エンティティを内包描画
  | { kind: "table" }                         // 属性 CSV を表として展開
  | { kind: "shape"; shape: ShapeKind };      // draw.io 的シェイプノード

type ShapeKind =
  | "rect" | "rounded" | "ellipse" | "diamond"   // フローチャート系
  | "cylinder" | "cloud" | "hexagon"             // DB / クラウド / 汎用
  | "person" | "device";                         // 人 / PC・機器
```

### 1.2 Entity (インスタンス) と属性 CSV

- インスタンスの属性は「任意の形式の CSV がくっついている」イメージ。**列形式はクラスが定義し、
  同一クラスの全インスタンスは同一形式**(構造上強制される)。行数はインスタンスごとに任意(0行可)。

```ts
interface Entity {
  address: StableAddress;
  typeAddress: StableAddress;
  layerAddress: StableAddress;
  // displayName, tags?, annotations?
  attributeRows?: AttributeRowData[];  // CSV の行。列は type.columns が正本
}

type AttributeScalar = string | number | boolean;

interface AttributeRowData {
  address: StableAddress;                      // Entity owner + authored row ID
  values: Record<StableAddress, AttributeScalar>; // key = column StableAddress
}
```

- 各行はauthored stable row IDを持ち、normalized modelではEntity ownerと組み合わせたrow StableAddressになる。値はcolumn StableAddress keyedで保持し、state / ViewData / exportのsource referenceにも同じaddressを使う。業務列の値やrow indexからidentityを導出しない。これにより:
  - クラスの列定義変更は全インスタンスのvalidation / display schemaへ反映されるが、既存row sourceを暗黙に書き換えない。
    optional列追加 → 既存行は値を省略したまま有効。required列追加 → explicit materializationでdefaultを書き込むかdiagnosticを解消する。
    列削除 → EntityTypeの`reserve { columns [...] }`へIDを残し、明示migrationで既存row keyを除去する。
    ラベル変更 → identityを変えない。型変更 → 既存値を再検証する。列の並び替え → presentation orderだけを変える。
  - schema変更operationはmigration preview、対象row、source patch、diagnosticsを提示し、ユーザーまたは明示write scopeを持つagentが原子的に適用する。
- 包含は Entity の `parent` / `children` field ではなく、`containment` semantic kind を持つ typed Relation で表す。レイヤ跨ぎの包含 Relation を許可する。
- バリデーション: 未定義型参照(error)、required 列の欠落(error)、enum 逸脱(error)、
  未知列キー(error)、包含サイクル(error)。
- `createdAt` / `updatedAt` / provenance は属性値の列として混ぜない。system fields は [system-fields-and-provenance.md](system-fields-and-provenance.md) に従い、state layer で default Entity 単位 tracking とする。
- Attribute row単位の鮮度・出所が必要な型だけ、row StableAddressに紐づくstate metadataを有効化する。行ID自体は全行で必須だが、row-level state trackingはoptionalとする。cell単位trackingは対応しないが、source traceではrow StableAddressとcolumn StableAddressの組を使える。

### 1.3 Authoring preset

Host UIは作成補助用のpreset catalogを持てるが、presetは暗黙のEngine built-in typeでもLDL preludeでもない。選択時にproject-local EntityTypeとしてmaterializeするか、展開済みPackのEntityTypeをimportする。

| id | name | icon | representation | columns |
|---|---|---|---|---|
| generic | 汎用ノード | box | shape rounded | (なし) |
| subnet | サブネット | network | container | cidr, vlan |
| boundary | 境界 | square-dashed | container | (なし) |
| vm | 仮想マシン | server | table | os, cpu, memory, owner |
| host | 物理サーバー | hard-drive | table | model, location, owner |
| service | サービス | app-window | shape rounded | owner, sla |
| db | データベース | database | shape cylinder | engine, version |
| person | 担当者 | user | shape person | role, contact |
| org | 組織 | building | shape rect | contact |
| pc | 端末 | monitor | shape device | os, user |

- preset IDや`builtin` flagは正本へシリアライズしない。正本identityはmaterializeされたproject-local EntityTypeまたはcanonical pack EntityTypeで決まる。
- `icon` は lucide icon 名、`image` は package / registry asset への参照。`image` が解決できる場合は
  UI 表示で画像を優先し、解決できない場合は `icon`、さらに fallback shape へ落とす。
- AWS / Azure / GCP などの表現力が必要な pack は、EntityType と正方形画像 asset を `.ldpack` Pack として配布する。

## 2. DSL 拡張

```ldl
project infrastructure_example "インフラ構成" {}

layers {
  hardware "ハードウェア" @10
}

entity_type subnet "サブネット" {
  icon "network"
  image "assets/subnet.png"
  color "#4F7075"
  representation container
  columns {
    cidr "CIDR" string required format cidr
  }
}

entity_type vm "仮想マシン" {
  icon "server"
  representation table
  columns {
    os "OS" string
    cpu "CPU" number
  }
}

entities vm @hardware {
  app_prod_01 "app-prod-01"
}

rows vm [os, cpu] {
  app_prod_01 primary: "Ubuntu 24.04", 4
}
```

- `entity_type` は project schema の第一級要素。`icon` / `image` / `color` / `representation` / `column` を持つ。
- entity の `row <row_id> { ... }` が CSV の1行。row ID は Entity 内で一意かつ不変とし、各値のキーは型の column に一致必須とする。不一致は validate error。
- 後方互換は legacy importer の責務とする。未定義型は importer が明示的な placeholder EntityType と diagnostics へ変換し、language `"1"` compiler は暗黙型を生成しない。
- `.layerdraw` パッケージは DSL が正本のため、型定義(アイコン・描画情報込み)は自動的に可搬。

完全な文法は [ldl-language-specification.md](ldl-language-specification.md) に従う。

## 3. ビュー合成とレイアウト

### 3.1 原則

- 編集の意味論は不変: 2D 編集キャンバスは選択レイヤ内のみ。**重ね合わせは view の生成結果**。
- View は typed Query で対象 Layer を選択し、`shape diagram` の composed projection で重ね合わせる。正確な構文は [ldl-language-specification.md](ldl-language-specification.md) に従う。

### 3.2 表現解決規則 (per view)

| 型の representation | 単一レイヤ view | 複数レイヤ view (包含子が view 内に存在) |
|---|---|---|
| container | 表 (name + 属性CSV) ※子は出さない | **枠**。内部に子エンティティをネスト |
| table | 表 | 表 (親 container 内にネスト、親不在なら平置き) |
| shape | シェイプ | シェイプ (同上) |

- 「ネットワーク図ではサブネット=表、インフラ図(ネットワーク+ハードウェア)では
  サブネットの枠の中にハードウェアの表が入る」を規則として一般化したもの。

### 3.3 ネストレイアウト (core の ProjectionLayout 拡張)

- container 枠 = タイトルバー(アイコン+名前+主要属性1行) + 子のグリッド配置 + padding。サイズは子から自動算出。
- 子は行優先グリッド(列数は子数から平方近似)。孫ネスト可(再帰)。
- view 内で親を持たないエンティティは既存の階層レイアウトで枠と同列に配置。
- 決定的(同一入力→同一出力)であること。生成図は自動配置で、手調整はスコープ外。
- SVG レンダラは枠(角丸矩形+タイトルバー)、表(ヘッダ行+データ行)、各 ShapeKind を描画できること。

## 4. UI 要件

- **型マネージャ**: サイドバーのプリセット欄を実体化。組み込み型は本物のスキーマ付きで配置でき、
  「カスタム型を作成」ダイアログで 名前/説明/**アイコン(lucide ピッカー)**/色/representation(シェイプ選択含む)/
  列定義(key・label・型・required・default の行エディタ) を定義・編集できる。
- **lucide アイコンピッカー**: 検索入力+グリッドの picker コンポーネント。lucide-react の既存依存のみで実装
  (icons エクスポートから動的解決)。型のアイコンはサイドバー・カード・枠タイトル等で一貫して表示。
- **型の列定義編集**: schema変更前に影響row、required/default、型不一致、削除列をpreviewし、必要なrow migrationを一つのsemantic operation batchとして明示適用する。
- **インスペクター**: インスタンスの属性 CSV を表エディタで編集(行追加/削除、セル編集、required 空値の警告)。
- **2D 編集キャンバス**: representationに応じた描画を行う。tableは主要列を数行previewし、container occurrenceはRelationTypeのcontainment projectionに従って子occurrenceを枠内表示する。包含編集はEntityの親fieldではなくtyped Relation作成・endpoint変更として行う。
- **ビュー**: View一覧、no-code Query builder、typed shape設定、RelationType projection override、preview、対応artifact exportを提供する。View条件はMaster Entityへ書き戻さない。

## 5. 対応しないもの

- グラフDB的クエリ言語、depth/abstraction の高度化 (既存 view パラメータは維持)
- 生成図上での手動レイアウト調整・保存
- 型の継承/合成、参照型属性 (entity 参照)、列の型変換時のデータマイグレーション以上の整合処理
