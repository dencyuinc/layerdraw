# LayerDraw Licensing Use Cases

## 1. 目的

本書はLayerDraw License 1.0の代表的な利用例を、判定と必要な対応策の組で示す。法的な権利義務はroot [LICENSE](../../LICENSE)が規範であり、本書が競合する場合はライセンス条文を優先する。

判定:

| Status | Meaning |
| --- | --- |
| `FREE` | LayerDraw利用料なしで許可する |
| `CONDITIONAL` | 記載条件を満たす場合だけ`FREE` |
| `COMMERCIAL` | Commercial / OEM Licenseが必要 |
| `SEPARATE` | code licenseとは別のcontent / trademark / SaaS Termsで判断する |

有償、無料、Multi-tenant、server利用だけでは判定しない。最初に次を確認する。

1. Your OrganizationまたはCustomer自身の内部利用か。
2. 第三者向けHosted Offeringか。
3. End Userが任意Schema Definitionを直接・間接に変更できるか。
4. 主商品がdomain productか、汎用LayerDraw / Semantic Layer Builderか。

## 2. Internal / Self-host

| Use case | Status | Required response |
| --- | --- | --- |
| 個人がDesktopで利用 | `FREE` | local ownerを`full_authoring`にできる |
| 会社が社内serverへSelf-host | `FREE` | 全Authoring Capabilityを利用可能 |
| 同一企業内で複数Organizationを管理 | `FREE` | internal multi-tenantとして扱う |
| 支配関係にある企業groupで共通利用 | `FREE` | Your Organizationの範囲を記録する |
| 社員が任意EntityType / RelationType / Layerを定義 | `FREE` | internal useなので`schema:write`を許可可能 |
| 社内AIがsemantic modelを構築 | `FREE` | agent delegationとAuditを使う |
| Customer自身のAWS / Azure / GCPへ導入 | `FREE` | Customer-Controlled Deploymentとする |
| on-premisesへ導入 | `FREE` | Customer内部利用として扱う |
| SIerがCustomer環境へ導入・保守 | `FREE` | Customer環境とCustomer内部利用を契約で明確化する |
| SIerがCustomer環境をremote operation | `FREE` | 共通Hosted platformへ転用しない |
| 大学・研究室内部で利用 | `FREE` | internal / research use |
| 改造版を社内配布 | `FREE` | License、Notice、変更表示を維持する |

## 3. SDK / AI embedding

| Use case | Status | Required response |
| --- | --- | --- |
| Viewer SDKでgraphを表示 | `FREE` | Viewer packageのLicenseを同梱する |
| Render / Exportだけを製品へ組み込む | `FREE` | authoringを提供する場合はFixed Model条件、閲覧専用ならRead-Only Viewing Service条件を守る |
| AI agentの長期記憶backendとして使う | `FREE` | ProviderがSchemaを管理する |
| Data / AI productの内部semantic layerとして使う | `FREE` | End Userへ`schema:write`を渡さない |
| 複数AIが同じgraphを共同編集 | `FREE` | contributionごとのActor / grantを保持する |
| End UserがEntity / Relation / rowを編集 | `FREE` | `graph:write`とGraph constraintを使う |
| End UserがQuery / Viewを作る | `FREE` | optional `query:write` / `view:write`を使う |
| End Userが画像assetを追加 | `FREE` | optional `asset:write`を使う |
| End UserがProvider定義modelをcatalogから選択 | `FREE` | model closureをProviderが固定する |
| End Userが任意EntityTypeを作る | `COMMERCIAL` | Platform / OEM Licenseを取得する |
| End Userが任意RelationTypeを作る | `COMMERCIAL` | Platform / OEM Licenseを取得する |
| End Userが任意Layerを作る | `COMMERCIAL` | Platform / OEM Licenseを取得する |
| End UserがColumn / Constraintを追加 | `COMMERCIAL` | Schema authoringとして扱う |
| AIがEnd UserのpromptでSchemaを生成 | `COMMERCIAL` | 間接General-Purpose Schema Authoringとして扱う |
| 固定Meta-schemaのinstanceでEnd User独自の型・field・関係規則を定義 | `COMMERCIAL` | 保存形式や`graph:write`という操作名にかかわらず、実質的な任意Schema authoringとして扱う |
| End UserがSchemaを含む任意`.ldpack`をinstall | `COMMERCIAL` | Pack経由のSchema変更として扱う |
| End UserがProvider許可済みのasset / styleだけを導入 | `FREE` | Schema Definitionを変更せず、Fixed Modelのdomain境界を維持する |
| End Userが任意`.layerdraw`をuploadしてSchemaごとauthoring | `COMMERCIAL` | arbitrary Schema importとして扱う |
| End Userが固定Schemaへ適合するinstance dataをimport | `FREE` | import時にSchema closureを検証し、未知の定義を拒否する |
| LayerDrawの全MCP toolsを顧客へ公開 | `COMMERCIAL` | Hosted Platform契約を取得する |
| Fixed Model用MCP toolsだけを公開 | `FREE` | schema / package / generic source経路をRuntimeで拒否する |

## 4. Vertical SaaS / Fixed Model Application

| Use case | Status | Required response |
| --- | --- | --- |
| Structurizr相当のarchitecture SaaS | `FREE` | C4等のSchemaをProviderが固定する |
| AWS infrastructure管理SaaS | `FREE` | AWS modelをPublisherだけが更新する |
| 業務flow管理SaaS | `FREE` | End Userは業務instanceだけを編集する |
| Data catalog SaaS | `FREE` | Provider定義Type内でdataを管理する |
| CMDB / IT資産管理SaaS | `FREE` | fixed semantic modelとして提供する |
| AI記憶管理SaaS | `FREE` | Providerが記憶Schemaを管理する |
| 医療・製造・金融向けdomain graph | `FREE` | domain modelとvalidationをProviderが所有する |
| 有償Multi-tenant Vertical SaaS | `FREE` | 有償 / Multi-tenant自体は制限しない |
| Tenantが固定Templateを選ぶ | `FREE` | Providerが選択肢を事前定義する |
| ProviderがCustomerごとにSchemaを設計して固定 | `CONDITIONAL` | professional serviceとして設計し、納品後に汎用Schema Editorを開放しない |
| CustomerがProvider定義済みの拡張枠へ独自field値をrowとして追加 | `FREE` | `ExtensionAttribute`等の意味・型・適用先をProviderが制約し、任意の型システムとして解釈しない |
| CustomerがMetaEntityType / MetaRelationType等のinstanceで独自Schemaを構築 | `COMMERCIAL` | instance dataに符号化しても実質的なGeneral-Purpose Schema Authoringに該当する |
| CustomerがColumnそのものを追加 | `COMMERCIAL` | `schema:write`を提供するためPlatform契約が必要 |
| Customer adminだけがSchemaを編集 | `COMMERCIAL` | End User組織へのSchema authoring提供として扱う |
| End UserがチャットでSchema変更を依頼し自動反映 | `COMMERCIAL` | AI / service actorによる迂回とみなす |
| End Userがpublic RegistryからSchemaを含む任意Packを導入 | `COMMERCIAL` | `package:manage`をEnd Userへ与えない |
| Provider / PublisherだけがPackを導入 | `FREE` | service actorへ必要capabilityを限定する |

## 5. Hosted LayerDraw

| Use case | Status | Required response |
| --- | --- | --- |
| Stock LayerDrawをpublicにhost | `COMMERCIAL` | Hosted LayerDraw Licenseを取得する |
| Stock Self-host bundleを第三者へmanaged提供 | `COMMERCIAL` | Commercial契約を取得する |
| Vendor所有cloudでCustomer専用のStock LayerDrawを運用 | `COMMERCIAL` | single-tenantでもCustomer-Controlled Deploymentではなく、汎用機能のmanaged提供として扱う |
| Customer所有VPCでCustomer専用のStock LayerDrawを運用代行 | `FREE` | Customer-Controlled DeploymentとしてCustomer内部利用に限定する |
| 無料LayerDraw Cloudを公開 | `COMMERCIAL` | 料金の有無にかかわらずHosted扱い |
| 非営利団体が汎用LayerDraw serviceを一般公開 | `COMMERCIAL` | 非営利かどうかではなく提供能力で判定する |
| White-label LayerDraw | `COMMERCIAL` | OEM LicenseとTrademark許諾を取得する |
| logoだけ変更したLayerDraw | `COMMERCIAL` | 実質的機能で判定する |
| theme / Pack / brandingだけ変更 | `COMMERCIAL` | 汎用機能集合が残るためHosted扱い |
| LayerDraw API as a Service | `COMMERCIAL` | Platform Licenseを取得する |
| LayerDraw MCP Server as a Service | `COMMERCIAL` | 汎用MCP operation提供として扱う |
| 汎用Semantic Layer Builder | `COMMERCIAL` | Platform / OEM Licenseを取得する |
| 汎用Graph / Diagram Editor SaaS | `COMMERCIAL` | LayerDraw代替serviceとして扱う |
| domain productの内部実装としてserverを使う | `FREE` | Fixed Model Application条件を守る |
| 他製品の付加機能としてreadonly graphを表示 | `FREE` | Read-Only Viewing Serviceの範囲を守る |
| 汎用`.layerdraw` Viewer site | `FREE` | 閲覧、埋込み、render、download、exportと、それに付随する保存・認証・共有だけに限定する |
| Viewer siteで編集、共同作業、履歴、Project管理を提供 | `COMMERCIAL` | Read-Only Viewing Serviceを超える汎用Hosted LayerDrawとして扱う |
| Viewer siteから汎用SDK / API / MCPを顧客へ公開 | `COMMERCIAL` | read-only UIでも汎用programmatic capabilityを提供しない |

## 6. LayerDraw Marketplace

| Use case | Status | Required response |
| --- | --- | --- |
| Publisherが固定Schema Appを出品 | `SEPARATE` | LayerDraw SaaS / Marketplace Termsへ従う |
| PublisherがAppを有料販売 | `SEPARATE` | SaaS Termsでprice / revenue shareを定める |
| App End UserがEntity / Relationを編集 | `FREE` | app policyで`graph:write`を付与する |
| App End UserがViewを作る | `FREE` | Publisher判断で`view:write`を付与する |
| PublisherがApp Schemaをupdate | `FREE` | Publisher service actorへ`schema:write`を付与する |
| App End UserがSchemaを変更 | `COMMERCIAL` | Generic Platform capabilityとして扱う |
| Publisherが同じFixed Appを自分でhost | `FREE` | LayerDraw LicenseのFixed Model条件を守る |
| 汎用BuilderをMarketplaceへ出品 | `SEPARATE` | Official SaaS Termsと個別commercial条件で判断する |

## 7. Distribution / resale

| Use case | Status | Required response |
| --- | --- | --- |
| Docker imageを配布 | `FREE` | License、LICENSING、third-party noticesを同梱する |
| Customer-operated VM imageを販売 | `FREE` | Customer-Controlled Deploymentにする |
| Helm chartを配布 | `FREE` | chart自体のSPDXを明示する |
| LayerDraw入りapplianceを販売 | `FREE` | Customer自身が運用する形にする |
| Vendorがapplianceを運用代行 | `CONDITIONAL` | Customer専用ならFREE、共通汎用HostedならCommercial |
| 改造Desktopを有料配布 | `FREE` | Noticeと変更表示を維持し、商標を変更する |
| LayerDrawをforkして別名配布 | `FREE` | Hosted Offeringにしない |
| forkを汎用SaaSとして公開 | `COMMERCIAL` | Hosted LayerDraw Licenseを取得する |
| proprietary productへbinary同梱 | `FREE` | LayerDraw部分のLicenseを同梱する |
| proprietary codeとlink / IPC接続 | `FREE` | 周辺codeのsource公開義務はない |
| support / consultingを販売 | `FREE` | Software自体の禁止Hosted Offeringを提供しない |

## 8. File / Pack / content

| Use case | Status | Required response |
| --- | --- | --- |
| 作成した`.ldl`を販売 | `SEPARATE` | authorがcontent licenseを選ぶ |
| `.layerdraw`成果物をCustomerへ納品 | `SEPARATE` | LayerDraw Licenseは成果物へ伝播しない |
| `.ldpack`を有料販売 | `SEPARATE` | Publisherがartifact licenseを設定する |
| OSS Packを公開 | `SEPARATE` | Apache-2.0 / MIT等をPublisherが選ぶ |
| Organization限定Pack | `SEPARATE` | private Registryと契約で管理する |
| AWS / Azure / Google iconを同梱 | `SEPARATE` | provider asset termsを確認する |
| third-party imageを同梱 | `SEPARATE` | redistribution rightとattributionを確認する |
| Official Registry contentをfork | `SEPARATE` | Registry codeとcontent licenseを分けて確認する |
| private RegistryをSelf-host | `FREE` | Registry serviceの内部利用として扱う |
| Registry機能自体を汎用SaaSとして販売 | `COMMERCIAL` | Restricted Hosted Offeringに該当するか評価する |

## 9. Development / contribution

| Use case | Status | Required response |
| --- | --- | --- |
| sourceを読む・学習する | `FREE` | Source Availableとして利用する |
| forkして修正 | `FREE` | LicenseとNoticeを維持する |
| fixを内部利用 | `FREE` | source公開義務なし |
| modified productを配布 | `FREE` | LayerDraw Licenseと変更表示を維持する |
| Pull Requestを送る | `CONDITIONAL` | CLAへ同意する |
| protocol bindingだけ利用 | `FREE` | Apache-2.0 surfaceを利用する |
| independent protocol clientを作る | `FREE` | Apache-2.0 surfaceを利用する |
| Apache surfaceだけを使い独立互換backendを作る | `SEPARATE` | Apache-2.0に従う。LayerDraw product codeを使わない限りLayerDraw LicenseのHosted制限は適用しない |
| LayerDraw codeで互換SaaSを作る | `COMMERCIAL` | Hosted Platform契約を取得する |

## 10. Indirect Schema Authoring

次はEnd Userへ`schema:write` buttonを表示していなくてもGeneral-Purpose Schema Authoringとして扱う。

- End User promptをAIがEntityType / RelationType / Layerへ変換する。
- End User uploadの`.layerdraw` / `.ldpack`を自動installする。
- End Userの依頼ごとにservice accountが定型的・即時に任意Schemaを生成する。
- generic source patch / import APIでSchemaを変更できる。
- role名だけPublisher / Adminに変え、実質的にはCustomer userが操作する。
- 固定されたMetaEntityType、MetaRelationType、ExtensionAttribute等のinstanceとして型、field、関係、constraint、semantic ruleを保存し、productがそれをEnd User固有のSchemaとして解釈する。

内部保存形式がSchema declarationかinstance dataか、operation名が`schema:write`か`graph:write`かでは判定しない。End Userが実質的に任意の型システムを定義できるかで判定する。Providerがfieldの型、意味、適用対象、validationを事前に固定したbounded extensionへ値を入れるだけならGeneral-Purpose Schema Authoringではない。

一方、Provider personnelがprofessional serviceとして要件を分析し、Providerが責任を持つfixed model releaseとしてCustomerへ提供することは、それだけでGeneral-Purpose Schema Authoringにならない。

## 11. Technical response matrix

| License intent | Product control |
| --- | --- |
| Fixed Model Application | `fixed_semantic_model`を完全なAuthoringPolicyへ展開する |
| End User instance編集 | constraint付き`graph:write` |
| optional View / Query | `view:write` / `query:write`を個別grant |
| Schema保護 | End User / agentへ`schema:write`を付与しない |
| Pack経由の迂回防止 | End Userへ`package:manage`を付与しない |
| source / import迂回防止 | AuthoringImpactをpublication直前に再評価する |
| AI経由の迂回防止 | delegated scopeをEnd User grantより拡張しない |
| Meta-schema迂回防止 | Engineの`graph:write`分類だけに依存せず、HostがEnd Userへ公開するMeta-modelの実質能力をproduct capability reviewで拒否する |
| local fileの限界 | filesystem DRMを主張せず、強制時はRuntimeを唯一のpublication pathにする |
| Commercial Platform | contract entitlementとAccess decisionを分離し、Commercial Licenseで明示許可する |

技術controlは証拠と運用を支援するが、license判定をbutton visibilityやpreset名だけへ委ねない。

## 12. Proprietary product boundary

LayerDraw Licenseはproprietary productへの組み込みを許可し、周辺codeへ同じlicenseを伝播させない。境界は次で判定する。

| Code / artifact | Applicable license |
| --- | --- |
| LayerDrawの対象source fileをそのまま配布 | LayerDraw Licenseを維持する |
| LayerDrawの対象source fileを変更したfork部分 | LayerDraw Licenseと変更表示を維持する |
| documented SDK / APIを呼び出す独立した自社code | 自社licenseを選択できる |
| LayerDrawを同一binaryへlink / embedするが、LayerDraw implementationを自社codeへ複製しない | link / embedだけを理由に自社codeへLayerDraw Licenseは伝播しない |
| LayerDraw implementationをcopyして自社moduleへ取り込む、またはその派生実装を作る | 取り込んだLayerDraw部分とその変更はLayerDraw License |
| Apache-2.0指定のprotocol schema / generated wire bindingだけを使う | Apache-2.0 |
| `.ldl`、`.layerdraw`、`.ldpack`、render / export成果物 | artifact authorが選ぶcontent license。LayerDraw利用だけではproduct licenseは伝播しない |

LayerDraw部分を含む配布物はroot License、applicable licensing matrix、third-party noticeを同梱する。単一repository、単一process、単一container、単一binaryであることだけからproprietary codeをLayerDrawの派生物とみなさない。
