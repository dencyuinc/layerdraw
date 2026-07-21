# Registry / Packs / Templates 方針

Registry source、download、signature、dependency resolution、install transaction、rollback、repair、Runtime handoffは[system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) 7章を規範とする。

## 1. 基本方針

LayerDraw は全提供形態で使える共通の拡張配布面を持つ。

対象:

- Pack Registry
- Template Registry

Icon / image asset は独立したRegistry itemにせず、`.ldpack`または`.layerdraw`へ同梱する。

これらはSaaS、Self-host、Desktop、VSCode、LayerDraw SDK、MCP Apps、Marketplace Integrationsで同じpackage formatとmetadataを使えるようにする。SDK内部のViewer / Browser Editor / Server variantでもformatは共通である。

LayerDraw の知識表現は Markdown note ではなく、typed graph と 2D attribute table を正本にする。そのため、registry で配布するものも「文章テンプレート」ではなく、EntityType、RelationType、Layer、View、attribute schema、starter graph を中心にする。

```text
LayerDraw Registry
  -> Packs
  -> Templates
    -> SaaS / Self-host / Desktop / VSCode
    -> LayerDraw SDK / MCP Apps / Marketplace Integrations
```

## 2. Pack

Pack は、特定ドメインの EntityType、RelationType、再利用可能なQuery / View、projection / render hints、Reference、関連 asset の詰め合わせである。Registryに登録する実体は常に`.ldpack`であり、型、recipe、Referenceの正本は`pack.ldl`から明示的にexportされる`.ldl` moduleに置く。Project固有のLayer、Entity、Relation、rowはPackへ入れず、Templateに置く。

例:

- AWS infrastructure pack
- Azure infrastructure pack
- Google Cloud infrastructure pack
- Kubernetes pack
- Data platform pack
- Security architecture pack
- Product management pack
- AI agent memory pack
- Enterprise architecture pack

Pack に含めるもの:

- EntityType definitions
- recommended RelationType definitions with projection / render hints
- supported projection primitive declarations
- supported render primitive declarations
- default column schema
- icon references
- optional square image assets
- color palette
- Reference declarations
- example snippets

Pack に含めないもの:

- ユーザー固有の project data
- revision history
- authentication settings
- storage settings
- long-form Markdown knowledge body

概念例:

```json
{
  "format": "layerdraw-pack",
  "format_version": 1,
  "id": "layerdraw/aws-core",
  "name": "aws_core",
  "display_name": "AWS Core",
  "publisher": "layerdraw",
  "version": "1.0.0",
  "license": "LicenseRef-Publisher-Pack-License",
  "copyright_holders": ["Example Publisher"],
  "asset_provenance": "assets/provenance.json",
  "language": 1,
  "entry": "pack.ldl",
  "dependencies": {},
  "compatibility": {
    "requires_core": ">=0.1.0",
    "projection_primitives": ["edge", "nest", "overlay", "badge", "hide"],
    "render_primitives": ["edge", "nested", "overlay", "badge"]
  }
}
```

Manifestはartifactの識別、解決、互換性、license、asset provenance、検証だけを担う。EntityType、RelationType、projection / render rule、Referenceをmanifestへ重複格納してはならない。Registryへpublishするartifactはlicense identifier、copyright holder、third-party asset provenanceを必須とし、利用条件不明のassetを含むartifactは拒否する。LayerDraw code licenseはPack / Template contentへ自動伝播せず、Publisherがcontent licenseを選ぶ。製品codeとcontentの境界は[legal/README.md](legal/README.md)、具体例は[legal/use-cases.md](legal/use-cases.md)を規範とする。

## 3. Template

Template は、EntityType / RelationType / Layer / View / starter graph を組み合わせた、利用開始点である。

Pack が「部品カタログ」なら、Template は「この構造を埋めれば目的を達成できる作業台」である。

例:

- AI agent shared memory template
- coding agent project context template
- microservice architecture template
- incident impact analysis template
- data lineage template
- AWS landing zone template
- product requirement graph template
- decision and constraint tracking template

Template に含めるもの:

- required packs
- project skeleton
- layers
- views
- starter entities
- starter relations
- required columns
- validation rules
- Reference declarations for MCP and human guidance

Template に含めないもの:

- 個別組織の秘密情報
- user token
- server connection
- revision history
- Actor grant、ACL、AuthoringPolicy binding

AI agent 向け template は、agent が長期記憶として使うべき schema と操作語彙を定義する。

Template / PackはAuthoringPolicyをgrantしない。Registry metadataは推奨Policy profileを案内できるが、Host administratorまたはMarketplace publisherがHost metadataとして明示的にbindする。portable `.layerdraw` importだけで`schema:write`等を取得してはならない。

例:

```text
Template: Coding Agent Memory

Layers:
  - repository
  - runtime
  - decision
  - task

EntityTypes:
  - repository
  - package
  - module
  - service
  - api
  - database
  - decision
  - constraint
  - task

Relations:
  - contains
  - depends_on
  - implements
  - decided_by
  - blocked_by
  - affects
```

この template を使うことで、複数端末・複数コーディングエージェントが同じ構造化記憶を共有できる。

## 4. Registry 種別

### 4.1 Public Registry

LayerDraw が公開する標準 registry。

- official packs
- community packs
- verified publisher
- semantic versioning
- compatibility metadata
- package signatures

### 4.2 Private Registry

企業や個人が自分の pack / template を配布するための registry。

- self-host server 上で提供できる
- private storage adapter 上に置ける
- organization / workspace 単位で公開範囲を制御できる
- enterprise policy により allowlist / denylist を設定できる

### 4.3 Local Registry

VSCode、Desktop、local web、MCP local bridge で使うローカル registry。

- filesystem directory
- Git repository
- `.layerdraw-registry` directory

## 5. Package Format

Registry artifact は Pack を `.ldpack`、Template を `.layerdraw` に固定する。`.ldl` を Registry に直接登録しない。

```text
aws_core_1.0.0.ldpack
  manifest.json
  pack.ldl
  modules/
    network.ldl
    compute.ldl
  assets/
  previews/
  checksums.json
  signature.json

coding-agent-memory.layerdraw
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
```

`.ldpack` は配布・検証・install のための ZIP artifact である。install 時に `pack/<install-name>/` へ展開し、通常の open / compile / query / render / export は展開済み `.ldl` module と asset だけを読む。元の `.ldpack` を runtime に mount しない。

`document.ldl`をentry moduleとして含むtemplateは通常projectとしてimportできる。標準templateはproject-local sourceを`schema/`、`layers/`、`views/`、`references/`へ分割する。PackのEntityType / RelationTypeはproject sourceへコピーせず、展開済みpack moduleをimportしてcanonical pack identityのまま参照する。
Registry / pack update は既存 project の解決 version や EntityType / RelationType を暗黙に書き換えない。
更新が必要な場合は migration と diagnostics を出し、ユーザーまたは host runtime が明示的に適用する。

Canonical pack ID (`publisher/pack-name`)は同一Pack lineage内でimmutableである。同じPack IDのversion更新は宣言StableAddressを維持し、semantic hashとresolved digestを更新する。Pack ID変更は別PackOriginになるため、LDL `moves`では扱わない。Registryが署名付きreplacement metadataを提示し、consumer projectがimportとproject-local参照を明示migrationした場合だけ置換する。
Pack versionの`moves`は旧source bindingをactive aliasとして残さない。update workflowは新Pack tree、`layerdraw.resolved.json`、影響するproject-local source references、state address mappingを一つのpreviewable transactionで移行し、成功するまで旧versionをcurrentとして維持する。

module / import / export と `layerdraw.resolved.json` の規範仕様は [ldl-language-specification.md](ldl-language-specification.md) に従う。

Pack manifestは、LayerDraw Engineが解釈できるprojection primitiveとTS Renderが解釈できるrender primitiveだけを宣言できる。
Go Registry componentとEngineはinstall / open時にcompatibilityを検証する。Hostが未対応primitiveを検出した場合はdiagnosticsを出し、そのPack由来のViewをdegraded renderingまたはdisabledとして扱う。

## 6. Install / Use Flow

Registry clientはartifact bytesを取得する。Go Registry componentはsource identity、publisher / signature policy、dependency graph、install transactionを所有し、LayerDraw Engine package componentはZIP safety、artifact manifest、LDL、resolved tree、canonical digestを検証・生成する。TS Library UI、provider shell、MCP adapterはこの検証を再実装しない。

Web / Desktop:

- registry browser から pack / template を検索する
- template から新規 project を作る
- pack を検証して project の `pack/` tree へ展開する
- 必要なsource moduleへ明示importを書き、解決結果を`layerdraw.resolved.json`へ保存する

VSCode:

- command palette から pack / template を install する
- `.ldl` の completion / validation / preview に反映する
- workspace local registry を参照できる

MCP Apps:

- readonly preview でも pack icon / image assets を解決できる
- agentがtemplateを選択し、標準構成のproject-local `.ldl` source treeを生成できる

Server / Self-host:

- organization registry を持てる
- approved packs / templates を workspace に配布できる
- registry access は admin policy で制御できる
- Pack install / update / removeは`package:manage`に加え、resulting semantic diffが要求する`schema:write`、`query:write`、`view:write`等を全て検証する
- fixed semantic modelのProjectはAuthoringPolicy bindingをstageした後、schema capabilityを持つservice actorがTemplateを適用し、両方を同じbootstrap transactionで公開する

## 7. Entity UI 表現

EntityType は lucide icon だけでなく、正方形画像 asset を持てるようにする。

目的:

- AWS / Azure / GCP など公式風アイコンを使う
- ユーザー独自の製品アイコンを使う
- MCP Apps / preview / export で同じ見た目にする
- type registry と package export の可搬性を保つ

表現の優先順位:

1. `image` があり、asset を解決できる場合は画像を表示する
2. `icon` がある場合は lucide icon を表示する
3. どちらもない場合は shape default を表示する

要件:

- 画像は正方形を推奨する
- PNG / SVG / WebP を候補とする
- package export 時は使用 asset を `.layerdraw` に含める
- `.ldl` では asset reference を保持する
- asset が解決できない場合でも文書は壊れず、fallback icon / shape で表示する
- external URL 参照は privacy / offline / export 再現性の観点から既定では避ける

概念例:

```ldl
entity_type ec2_instance "EC2 Instance" {
  icon "server"
  image "assets/aws-ec2.png"
  color "#FF9900"
  representation table
  columns {
    instance_type "Instance Type" string
  }
}
```

## 8. 配布範囲

Registry は収益化を前提にせず、次の配布範囲を共通モデルで扱う。

- official public packs / templates
- community public packs / templates
- organization private packs / templates
- self-host private packs / templates
- local packs / templates

artifactの価格、購入、決済、売上分配、ランキング等はRegistry artifact / install protocolの意味論へ含めない。将来外部の配布サービスが追加されても、Registryは解決済みのaccess decisionとartifact sourceを受け取り、`.ldpack` / `.layerdraw`の検証・取得・導入契約を変えない。

## 9. セキュリティ

Pack / Template はコード実行を含めない。

許可するもの:

- schema
- `.ldl`
- static assets
- metadata
- validation metadata

禁止するもの:

- arbitrary JavaScript
- remote code execution
- credential embedding
- hidden external callbacks

外部 asset URL を許可する場合でも、明示的な policy と user consent を必要とする。
