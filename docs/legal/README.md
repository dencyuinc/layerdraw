# LayerDraw License Design

## 1. Status and authority

### 1.1 Document map

| File | Role | Authority |
| --- | --- | --- |
| root [`LICENSE`](../../LICENSE) | LayerDraw License 1.0本文 | 製品中核の法的権利義務を定める唯一の正本 |
| root [`NOTICE`](../../NOTICE) | 配布notice | 著作権・third-party noticeの入口 |
| 本書 | license設計、適用path、判断順序 | repository内の適用範囲を定める設計規範 |
| [`use-cases.md`](use-cases.md) | 具体的な利用判定例 | 非拘束の解説。条文と競合する場合は`LICENSE`優先 |
| [`trademarks.md`](trademarks.md) | 商標方針 | copyright licenseとは独立した商標規範 |
| [`contributor-license-agreement.md`](contributor-license-agreement.md) | Contributor契約 | 外部Contributionへ適用するCLA 1.0 |
| [`contributor-privacy-notice.md`](contributor-privacy-notice.md) | Contributor privacy notice | CLA同意とContribution記録に関する個人情報の取扱い |
| [`licenses/Apache-2.0.txt`](licenses/Apache-2.0.txt) | Apache-2.0本文 | matrixで明示したinteroperability surfaceだけの第二ライセンス |

rootには一般的な配布toolと利用者が最初に探す`LICENSE`と`NOTICE`だけを置く。説明・判断例・運用設計・個別policyは通常のrepository文書として本directoryへ集約する。

### 1.2 Status

LayerDrawはmixed-licenseのsource-available productである。製品中核はOSI承認Open Source Licenseではないため、LayerDraw全体を`Open Source`、`OSS`、`Open Core`と表示しない。

本リポジトリのライセンス判断は次の順で行う。

1. 対象file / package / artifactのSPDX identifier
2. 本書のpath / artifact matrix
3. 明示指定がなければroot [LayerDraw License 1.0](../../LICENSE)
4. Apache指定surfaceは[Apache License 2.0](licenses/Apache-2.0.txt)

[use-cases.md](use-cases.md)は具体例と運用指針である。条文と競合する場合はライセンス条文が優先する。

OSI承認の有無、source-availableであること、custom licenseであることだけを理由として、弁護士レビュー、行政承認、利用者の署名が法律上一律に要求されるわけではない。本repositoryでは、独自のHosted制限、Commercial / OEM境界、特許条項、Contributorから得る権利を事業上意図したとおりにするため、株式会社DENCYUが公開する条文と運用をrepositoryの規範とする。外部弁護士のreviewを承認材料にするかは株式会社DENCYUのgovernance判断であり、OSI要件ではない。

本書とuse caseでいう`Provider` / `Publisher`は、該当productまたはserviceを提供し、そのSchema Definitionを管理するLicense上の`You`または`Your Organization`を指す。`End User`、`Schema Definition`、`General-Purpose Schema Authoring`、`Fixed Model Application`、`Read-Only Viewing Service`、`Customer-Controlled Deployment`、`Restricted Hosted Offering`の規範定義はLayerDraw License 1.0 Section 2に従う。

## 2. License model

### 2.1 LayerDraw License 1.0

LayerDraw License 1.0のSPDX表現は`LicenseRef-LayerDraw-1.0`とする。

これはElastic License 2.0の「製品を実質的なHosted Serviceとして再提供させない」という境界を設計上の参考にした独立したsource-available licenseである。Elastic License 2.0そのものではなく、`Elastic-2.0`と表示してはならない。

LayerDraw固有の差分は次である。

- 無償Self-host、内部利用、顧客管理環境への導入を明示的に許可する。
- SDK、WASM、native binary、MCP、Serverを商用製品へ無償組み込みできる。
- 固定semantic modelを提供する商用・Multi-tenant Vertical SaaSを`Fixed Model Application`として明示的に許可する。
- 編集、共同作業、Project管理、汎用API等を持たない`Read-Only Viewing Service`を明示的に許可する。
- End Userへ任意Schema定義能力を提供する汎用Hosted LayerDraw / Semantic Layer BuilderはCommercial License対象とする。
- Schemaをinstance dataへ符号化しても、End Userが実質的に任意の型システムを定義できる場合はGeneral-Purpose Schema Authoringとして扱う。
- 作成した`.ldl`、`.layerdraw`、`.ldpack`、図、exportへproduct licenseを伝播させない。

### 2.2 Apache License 2.0

LayerDraw License 1.0はApache-2.0を内包せず、Apache-2.0をbase licenseとして追加制限を重ねる構成でもない。Apache-2.0 surfaceはthird-party integrationとprotocol interoperabilityのために別途許諾する狭い境界である。Apache packageへEngine / Runtime / Access / Registry semanticsを移し、Hosted制限を実質的に無効化してはならない。

### 2.3 Commercial / OEM License

次の用途はLayerDraw License 1.0だけでは許可せず、株式会社DENCYUとの別書面契約を必要とする。

- Stockまたはsubstantially stockなLayerDrawのHosted / Managed Service
- White-label LayerDraw、LayerDraw Cloud互換service
- End Userが任意のEntityType、RelationType、Column、Constraint、Layerを定義できるHosted product
- 汎用Semantic Layer Builder、汎用Graph / Diagram Authoring Service
- LayerDrawの主要API、SDK、MCP、Registry、Project管理をservice自体の主商品として提供すること
- license key、entitlement、notice、branding保護を外す必要があるOEM提供

Commercial Licenseの価格、seat、usage、revenue share、support、SLAはPublic Source Licenseへ埋め込まず、個別契約またはLayerDraw SaaS Termsで定める。

### 2.4 既存licenseをそのまま採用しない理由

| Candidate | 採用しない理由 |
| --- | --- |
| Elastic License 2.0 | Hosted制限は参考になるが、LayerDraw固有のFixed Model Application、Schema定義権限、成果物非伝播を条文上明示できない |
| n8n Sustainable Use License | internal / non-commercial中心の許諾では、LayerDrawが無償許可したいcommercial embedding、顧客環境への導入、Fixed Model SaaSまで狭めてしまう |
| Difyのmodified Apache terms | Multi-tenant自体を境界にすると、LayerDrawが許可したい有償Multi-tenant Vertical SaaSもCommercial対象になり、Authoring Capabilityによる境界と一致しない |
| Business Source License 1.1 / Functional Source License 1.1 | 一定期間後のOpen Source licenseへの自動転換はLayerDrawの現行事業要件ではない。releaseごとのChange Date管理も不要なversion軸を増やす |
| Apache-2.0単独 | interoperability surfaceには適するが、製品中核のRestricted Hosted Offeringを制限できない |

Apache-2.0本文へ独自制限を追記して「modified Apache」とは表示しない。独自条件を持つ製品中核と、変更しないApache-2.0 surfaceをpath単位で分離する。

## 3. Path and package matrix

### 3.1 Current repository

| Path / artifact | License |
| --- | --- |
| `docs/**` | `LicenseRef-LayerDraw-1.0` unless expressly stated otherwise |
| root repository metadata | `LicenseRef-LayerDraw-1.0` unless expressly stated otherwise |
| `.github/**` | `LicenseRef-LayerDraw-1.0` unless expressly stated otherwise |
| `docs/legal/licenses/Apache-2.0.txt` | Apache-2.0 license text |
| dependency code | upstream license |

### 3.2 Target monorepo

| Path / published surface | License |
| --- | --- |
| `internal/engine/**`、`internal/runtime/**`、`internal/access/**`、`internal/registry/**` | `LicenseRef-LayerDraw-1.0` |
| `internal/application/**`、`internal/adapter/**`、`cmd/**`、`apps/**` | `LicenseRef-LayerDraw-1.0` |
| `@layerdraw/client-sdk`、`@layerdraw/server-sdk`とその他handwritten `@layerdraw/*` package | `LicenseRef-LayerDraw-1.0` unless expressly assigned below |
| Editor、Viewer、Composer、Render、Export、React UI、Library UI、MCP Apps | `LicenseRef-LayerDraw-1.0` |
| `layerdraw-engine`、`layerdraw-host`、`layerdraw-server`、`layerdraw-registry` binary / container | `LicenseRef-LayerDraw-1.0` |
| Go WASM、native sidecar、Wails Desktop、VSCode extension | `LicenseRef-LayerDraw-1.0` |
| `schemas/**`のprotocol / container / release wire schema | `Apache-2.0` |
| `gen/go/**`、`@layerdraw/protocol/**`のgenerated wire bindings | `Apache-2.0` |
| low-level transport-only client | `Apache-2.0` |
| protocol conformance fixture | `Apache-2.0` |
| designated integration examples | `Apache-2.0` |

`schemas/semantic/**`をApache-2.0にしても、Compiler、Workbench、Query / View semantics、AuthoringImpact分類、Runtime transaction、Access decision等のbehavior implementationはLayerDraw Licenseに残す。

Target packageとして定義していないlegacy package名はpublish対象にしない。compatibility facadeを別途残す決定をした場合も、LayerDraw Engineを呼ぶ薄いartifactに限定し、TS semantic implementationを保持してはならない。

### 3.3 Published package metadata

- custom licensed npm package: `"license": "SEE LICENSE IN LICENSE"`
- Apache package: `"license": "Apache-2.0"`
- source header: `SPDX-License-Identifier: LicenseRef-LayerDraw-1.0`または`Apache-2.0`
- binary / container: root license、本書、third-party notice、SBOMを同梱する
- mixed packageを曖昧な`UNLICENSED`、`Other`、`Elastic-2.0`としてpublishしない

`SEE LICENSE IN LICENSE`を使うnpm artifactは、pack staging時に正準[LayerDraw License 1.0](../../LICENSE)の全文をartifact rootの`LICENSE`へ複製する。repository内のworkspace相対pathをpublished manifestへ残してはならない。CIはtarballを展開し、同梱`LICENSE`のdigestが正準条文と一致することを検証する。

## 4. Hosted safe harbors

### 4.1 Fixed Model Application

Fixed Model Applicationは、Provider / PublisherがSchema Definitionを管理し、End UserとそのAI agentにはProvider定義済みmodel内の操作だけを提供するproductである。

```text
Provider / Publisher
  schema:write
  package:manage
  model release / migration

End User / End User Agent
  graph:write
  optional query:write / view:write / asset:write
  no schema:write
  no arbitrary schema-changing Pack / Template install
  no generic source / import bypass for Schema Definition
```

次を満たす場合、商用・有償・Multi-tenantでもLayerDraw利用料なしで提供できる。

- End Userが任意Schema Definitionを直接作れない。
- End UserのAI、service account、import、Pack、operator automationを使った間接作成も提供しない。
- Providerが複数の固定modelをcatalogとして提示することはできる。
- End UserはEntity、Relation、row、Query、View、layout、assetを許可範囲で編集できる。
- productはdomain application、workflow、analysis、AI product等として提供され、汎用LayerDraw代替を主商品にしない。

`fixed_semantic_model` preset名、disabled button、契約上のrole名だけではSafe Harborにならない。実際の全write経路と提供能力を評価する。

MetaEntityType、MetaRelationType、ExtensionAttribute等を固定Schemaとして定義していても、そのinstanceをproductがEnd User固有の型、field、関係、constraint、semantic ruleとして解釈する場合はSafe Harborにならない。内部data modelや`graph:write`というoperation名ではなく、End Userが得る実質能力で判定する。Providerが意味、型、適用先、validationを固定したbounded extensionへ値を入れるだけなら許可範囲に残る。

### 4.2 Read-Only Viewing Service

汎用またはdomain固有のHosted Viewerは、LayerDraw project / contentの閲覧、埋込み、render、download、exportだけを提供する場合、無料で運営できる。これらに必要な保存、認証、access control、共有linkは付随機能として含められる。

次を一つでもEnd Userへ提供する場合はRead-Only Viewing Serviceではない。

- Schema、Graph instance、Relation、row、Query、View、layout、assetの作成または変更
- collaboration、revision history、Registry / Pack管理
- 閲覧対象を選択・保存する範囲を超えた汎用Workspace / Project管理
- 汎用SDK、API、MCP access

ReadonlyというUI labelだけでは足りず、upload、import、URL parameter、API、AI agent等の全経路で変更能力を公開しない。

## 5. Customer-Controlled Deployment

次は無償Self-hostとして扱う。

- 個人またはOrganization自身が運用する環境
- Customer自身のAWS、Azure、GCP、on-premises、private cloud
- SIerがCustomer環境へ導入、保守、remote operationする構成
- Customer専用deploymentでCustomerの内部利用だけに供する構成

Provider所有の共通platform上で複数Customerへ汎用LayerDrawを提供する場合はCustomer-Controlled Deploymentではない。

## 6. SDK and embedding

Viewer、Browser Editor、Server SDK、WASM、native sidecar、MCP adapterの取得と商用組み込み自体に利用料を要求しない。

- proprietary周辺codeのsource公開義務はない。
- LayerDraw部分を配布する場合はLicenseとNoticeを同梱する。
- Fixed Model Application、Read-Only Viewing Service、Customer-Controlled Deploymentなら各許可範囲でserver capabilityも利用できる。
- End UserへGeneral-Purpose Schema Authoringまたは汎用LayerDraw APIをserviceとして提供する場合はCommercial Licenseが必要になる。

## 7. User content and Registry content

ユーザーが作成した`.ldl`、`.layerdraw`、`.ldpack`、画像、図、Excel、PDF、PPTX等は、LayerDrawを使っただけではLayerDraw Licenseの対象にならない。

- author / publisherがartifact licenseを選ぶ。
- `.ldpack` / `.layerdraw` manifestはlicense identifier、copyright holder、asset provenanceを持つ。
- AWS、Azure、Google等のiconはprovider asset termsへ従う。
- LayerDraw codeをartifactへ含めた場合、そのcode部分にはLayerDraw Licenseが残る。
- Official Registry contentとProduct sourceのライセンスを混同しない。

## 8. Trademark

Copyright licenseは商標利用権を与えない。商標利用の規範方針は[trademarks.md](trademarks.md)だけに置き、本書へ条件を重複定義しない。

## 9. Contributions and relicensing

将来のCommercial / OEM distributionとlicense変更に必要な権利を株式会社DENCYUが保持できるよう、外部Contributionのmerge条件として個人または法人CLAへの同意を要求する。これはOSIまたは法律が全projectへ要求する手続ではなく、LayerDrawのinbound contribution policyである。Contributorは著作権を保持しつつ、株式会社DENCYUへ次を許諾する方式を採る。

- public source-available distribution
- Commercial / OEM Licenseでのdistribution
- patent grant
- 将来のlicense変更またはdual licensing

このgrantは意図的に広く保ち、Commercial / OEMまたはproprietary termsでの再許諾権を削らない。CLAは本repositoryへmergeするContributionのinbound policyであり、Registryへ独立artifactとして公開するPack / Templateのcontent licenseとは分離する。

CLA同意記録なしの外部Contributionをmergeしない。Contributorは各Pull RequestでCLA 1.0への同意checkboxを明示的に選択し、Contributionに必要な権利と勤務先等の承認を表明する。GitHub account、Pull Request、対象commit、同意文、CLA version、記録日時を同意記録とする。法人自身をContributorとする場合は、Section 8に従い権限ある代表者による署名またはDENCYUが指定する電子的同意記録を別途保持する。Dependabotが作成した依存更新Pull Requestは、bot自身が著作権を主張するContributionではないためcheckbox対象外とする。required repository-policy checkは対象Pull Requestの同意欠落を拒否する。

個人情報の取扱いは[LayerDraw Contributor Privacy Notice](contributor-privacy-notice.md)に従う。CLA本文は[contributor-license-agreement.md](contributor-license-agreement.md)を正本とする。

## 10. Release and CI enforcement

- 機械可読のlicense policy正本を[`tools/license-policy.json`](../../tools/license-policy.json)とし、`make license-check`を必須CI gateにする。
- `make license-check` / `make license-report`はGo/npmのruntime・development全依存を統合した`reports/dependency-licenses.json`を生成する。reportはpackage/module、version、scope、検出license、解決後license、review evidence、policy / lockfile digestを持ち、時刻とmachine固有pathを含めない。
- third-party licenseは明示allowlistだけを自動許可し、denylist該当だけでなく未分類・未知・新しいlicense expressionもreview完了まで拒否する。
- Go moduleはmodule path、version、SPDX license、license file path、本文SHA-256をreview記録として固定する。module graphの追加・更新・削除とpolicyのdriftを拒否し、未定義の`replace`で審査済みsourceを差し替えられないようにする。
- npm dependencyはpnpmが実際に解決したpackage / version / licenseを検査する。`Unknown`等のmetadata不備はpackage / version単位のoverrideとlicense本文SHA-256がある場合だけ許可する。
- development dependencyも検査対象に含めるが、配布NOTICEとartifact SBOMには実際に配布物へ組み込まれたruntime dependencyだけを含める。
- pathごとのlicense allowlistをCIで検証する。
- Apache packageがproduct implementationをimportしたら失敗させる。
- package manifest、SPDX header、release manifest、container noticeのdriftを拒否する。
- npm tarball内の`package.json`が`SEE LICENSE IN LICENSE`を指し、artifact rootの`LICENSE`が正準条文とdigest一致することを検証する。
- Go binaryはcompiled build infoからlinked moduleを再取得し、正準`LICENSE`、`NOTICE`、`LICENSING.md`、`THIRD_PARTY_NOTICES.txt`、CycloneDX SBOMを同じartifact bundleへ生成する。source module graph全体をruntime依存として記載してはならない。
- TS package、container、VSIX、Desktop、WASM等も各artifactの実際のruntime closureからthird-party notice / SBOMを生成し、repository全体のdevelopment SBOMを代用しない。
- `.ldpack` / `.layerdraw` publishingでcontent license不足を拒否する。
- public release前に株式会社DENCYUが承認したlicense versionと承認記録をrelease metadataへ残す。外部弁護士の関与を必須metadataにしない。

## 11. References

- [LayerDraw License 1.0](../../LICENSE)
- [Apache License 2.0](licenses/Apache-2.0.txt)
- [Licensing use cases](use-cases.md)
- [Authoring Access Control](../authoring-access-control.md)
- [Elastic License 2.0](https://www.elastic.co/licensing/elastic-license)
- [n8n Sustainable Use License](https://github.com/n8n-io/n8n/blob/master/LICENSE.md)
- [Dify License](https://github.com/langgenius/dify/blob/main/LICENSE)
- [Business Source License 1.1](https://mariadb.com/bsl11/)
- [Functional Source License 1.1](https://fsl.software/)
- [npm package.json license field](https://docs.npmjs.com/files/package.json/)
- [SPDX custom LicenseRef](https://spdx.github.io/spdx-spec/v2.3/using-SPDX-short-identifiers-in-source-files/)
- [OSI Approved Licenses](https://opensource.org/licenses)
