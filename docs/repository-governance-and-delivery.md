# Repository Governance / Build / Release / Delivery 規範

## 1. 文書の位置づけ

この文書は、LayerDraw の To-Be リポジトリ構成、開発ガバナンス、CI、リリース、配布、SaaS CD、ライセンス適用の機械的強制を定める規範仕様である。法的な適用範囲は[legal/README.md](legal/README.md)を規範とし、現行リポジトリ構成を移行先の根拠にはしない。

この文書が固定するもの:

- public / private repository の責務境界
- public product monorepo の正準構成
- Go / TypeScript / generated artifact の build orchestration
- maintainer、reviewer、approver、contributor の権限境界
- pull request、merge queue、CI の信頼境界
- release set、artifact、配布先、署名、provenance
- public release と private SaaS deployment の分離
- source-available product と interoperability surface のライセンス境界

この文書は LDL、Engine、Runtime、Registry、SDK の意味論を再定義しない。package の責務と依存方向は [component-package-boundary-specification.md](component-package-boundary-specification.md)、protocol と release manifest は [system-boundary-contracts-specification.md](system-boundary-contracts-specification.md) を規範とする。

## 2. 基本決定

### 2.1 公開モデルの正式名称

LayerDraw は、ソースコードを public GitHub repository で公開する **source-available product** とする。製品中核が OSI 承認ライセンスでない限り、公式文書、Web site、README、release note で LayerDraw 全体を `Open Source` または `OSS` と表現しない。

`Open Core` も、OSI 承認の open-source core と proprietary extension を組み合わせる意味で誤解を生むため、LayerDraw 全体の正式分類には使わない。説明が必要な場合は、次の事実を個別に記載する。

- source code は公開される
- self-host は無償で利用できる
- LayerDraw の主要機能を汎用LayerDraw代替の hosted / managed service として再提供することは禁止し、Fixed Model Applicationとしての利用は許可する
- protocol、schema、低レベル integration surface の一部は permissive license で提供する
- 公式 SaaS、運用基盤、credential、production configuration は public product source と分離する

製品中核は[LayerDraw License 1.0](../LICENSE)、interoperability surfaceはApache-2.0を採用する。適用path、Fixed Model Application、Commercial / OEM境界は[legal/README.md](legal/README.md)を規範とし、公開release前の株式会社DENCYUによる正式approvalを内部gateとする。外部弁護士reviewや特定の署名方式をOSI由来の必須要件として扱わない。

### 2.2 Repository 分割の原則

repository は compile boundary の代用品にしない。Engine、Runtime、protocol schema、generated binding、Web、Desktop、VSCode、SDK、MCP Apps は、相互の schema / protocol / conformance 変更を一つの pull request で完結させる必要があるため、同じ public product monorepo に置く。

repository を分けるのは、次のいずれかを満たす場合だけである。

- 公開可否が異なる
- credential または production environment の信頼境界が異なる
- product release と独立した content lifecycle を持つ
- release automation が生成物だけを書き込む distribution repository である

## 3. Repository Topology

### 3.1 正式構成

| Repository | Visibility | Source of truth | Owns | Does not own |
| --- | --- | --- | --- | --- |
| `layerdraw/layerdraw` | Public | Yes | product source、schemas、generated bindings、Go / TS packages、apps、extensions、integrations、self-host deployment、CI、release definition | SaaS credential、production state、公式Registry content本体 |
| `layerdraw/cloud` | Private | Yes | 公式SaaS control plane、GitOps environment、provider固有secret wiring、production policy、operations、incident runbook | LDL / Engine semantics、public package fork、release artifact rebuild |
| `layerdraw/registry-content` | Public | Yes | 公式 `.ldpack` / `.layerdraw` のsource、asset、metadata、content test、publisher record | Registry service実装、product binary |
| `layerdraw/homebrew-tap` | Public | No | release automation が生成する formula | 手編集するproduct source |
| security response environment | Private | No | embargo中のadvisory、patch検証、coordinated disclosure | 通常開発の恒久fork |

必要になった配布先専用repositoryは、生成物mirrorとしてのみ追加する。正本sourceを複製しない。

### 3.2 Public product monorepo と private cloud の境界

public monorepo は managed deployment でも Self-host でも使える同じmulti-organization `layerdraw-server`、generic Access / Entitlement / Usage port、storage adapter interface を生成する。private deployment repository は署名済みpublic release artifactをdigest固定で利用する。

private cloud が public monorepo の Go `internal/` package を importすることを禁止する。private capability が必要な場合は、次のいずれかで接続する。

- versioned public protocol
- documented host port
- signed executable / container artifact
- public client package

これにより、SaaSだけが未公開Engine forkを持つ状態と、Self-host artifactをproductionで再buildする状態を禁止する。

### 3.3 Registry content の境界

Registry service実装は `layerdraw/layerdraw` に置き、公式RegistryとSelf-host Registryで同じbinaryを使う。Registryへ公開する実体は `layerdraw/registry-content` に置く。

- Pack はすべて `.ldpack`
- Template はすべて `.layerdraw`
- source asset、license、publisher metadata、build recipeをcontent repositoryで管理する
- release済みartifactはdigestで識別し、同じversionの内容を差し替えない

## 4. Public Product Monorepo Layout

正準構成は次とする。これはownershipとdependency directionを表すものであり、frameworkごとにdomain logicを複製するための構成ではない。

```text
/
  .changeset/                 release intent
  .github/
    ISSUE_TEMPLATE/           typed issue forms
    workflows/                CI / release entrypoints
    CODEOWNERS                generated ownership routing
  docs/
    legal/                    license design / use cases / policies
    ...                       requirements / architecture / ADR
  rfcs/                       accepted / proposed public design changes
  schemas/                    language-neutral protocol / container source of truth
  gen/                        committed generated Go / TS bindings
  internal/                   Go capability packages and adapters
  cmd/                        Go composition roots
  packages/                   publishable and private TS workspace packages
  apps/
    web/                      React Web delivery shell
    desktop/                  Wails + React delivery shell
    mcp-app/                  MCP Apps client delivery shell
  extensions/
    vscode/                   VSCode delivery shell
  integrations/
    marketplace/              provider integration shells
  deploy/
    self-host/                Docker Compose / Helm / example configuration
  tests/
    conformance/              cross-runtime normative fixtures
    integration/              multi-component tests
    e2e/                      delivery-bundle workflows
    packaged/                 tests against built artifacts
  tools/                      repository-local build / codegen / policy tools
  go.mod                      single Go module
  package.json
  pnpm-workspace.yaml
  turbo.json
  Makefile                    language-neutral operator entrypoints
  LICENSE                     canonical LayerDraw License 1.0 text
  NOTICE                      product / third-party notice entrypoint
  OWNERS.yaml                 root ownership fallback
```

Go package と TS package の詳細な名前および依存DAGは [component-package-boundary-specification.md](component-package-boundary-specification.md) を規範とする。

### 4.1 禁止する構成

- nested product `go.mod`
- appごとのprotocol schema copy
- generated sourceを各build jobで別々に生成して比較しない構成
- private cloud repositoryへのEngine source copy
- distribution repositoryでのsource修正
- product packageと `.ldpack` / `.layerdraw` を同じ意味の「package」として扱う命名

## 5. Build System

### 5.1 Toolchain

| Concern | Tool / rule |
| --- | --- |
| Go dependency and build | repository rootの単一 `go.mod` |
| TS dependency | `pnpm` workspace。lockfileはrootに一つ |
| TS task graph / cache | Turborepo |
| Release intent / changelog | Changesets |
| Cross-language operator commands | root `Makefile` |
| Protocol generation | `schemas/` から `gen/` と `@layerdraw/protocol` を生成 |
| Reproducible tool versions | version fileまたはpackage manager metadataで固定 |

Turborepo は Go build の意味論を所有しない。Makefile は実装ロジックを大量に持たず、Go、pnpm、codegen、conformance、packaging の安定した入口を提供する。

### 5.2 標準 task contract

少なくとも次のroot taskを定義する。

```text
make bootstrap
make generate
make format
make lint
make typecheck
make test
make conformance
make integration
make build
make package
make verify-packaged
make ci
```

localとCIで別の検証コマンドを作らない。`make ci` は required checks の意味論的なsupersetであり、CIは同じsubtaskを呼ぶ。

### 5.3 Generated code

generated bindingはcommitする。pull requestでは次を機械検証する。

1. clean checkoutから規定toolchainで生成する
2. worktreeに差分が出ないことを確認する
3. generated fileにgenerator versionとschema digestを記録する
4. Go / TS / release manifestのschema digest一致を確認する

生成差分はschema変更と同じpull requestに含める。release jobで初めて生成しない。

### 5.4 Cache と再現性

- cache keyはtoolchain、lockfile、task input、environment allowlistを含む
- secret、credential、署名keyをremote cacheへ含めない
- release artifactはclean checkoutと固定toolchainから作る
- timestampなど再現性を壊す値はrelease manifestへ明示し、artifact本体への混入を制御する
- build後のartifactを再buildせず、promotionする

## 6. Governance and Contribution

### 6.1 Role

| Role | Responsibility |
| --- | --- |
| Contributor | issue / RFC / pull requestを作成する |
| Reviewer | code quality、test、component整合性をreviewする |
| Approver | component boundary、public API、release影響を承認する |
| Maintainer | repository policy、release、security、ownershipを管理する |
| Release manager | protected environmentで署名済みreleaseをpublishする |
| Security team | private report、embargo、CVE / advisory coordinationを扱う |

reviewerとapproverを同一概念にしない。componentごとの `OWNERS.yaml` を正本とし、`.github/CODEOWNERS`、label、review routingはそこから生成する。

### 6.2 Ownership

`OWNERS.yaml` は少なくとも次を表現する。

```yaml
component: engine
reviewers:
  - alice
approvers:
  - bob
labels:
  - area/engine
```

ownershipは親directoryから継承できるが、`schemas/`、`internal/engine/`、`.github/workflows/release*`、license fileは明示ownerを必須とする。

次の変更は2名以上のapprovalを必須とし、少なくとも1名は対象componentのapproverでなければならない。

- LDL language semantics
- protocol / container schema
- StableAddress、hash、canonicalization
- Engine / Runtime public boundary
- release workflow、artifact signing、provenance
- license、CLA、trademark policy

### 6.3 RFC / ADR

RFCが必要な変更:

- language、protocol、containerのbreaking change
- public packageまたはbinaryの追加 / 削除
- repository topologyまたはlicense boundaryの変更
- storage / realtime / registry portの意味変更
- release setまたは配布チャネルの意味変更

ADRは採用した実装選択と根拠を記録する。RFCの代わりにissueコメントだけでpublic contractを変更しない。

### 6.4 Issue / Pull Request

Issue formは少なくとも Bug、Feature、RFC、Storage Provider、Registry Content、Security Guidance に分ける。security vulnerabilityはpublic issueに誘導せず、GitHub private vulnerability reportingへ送る。

Organization Project `LayerDraw Development`をpublic development workflowの正本とする。repositoryがprivateの間はProjectもprivateとし、repositoryをpublicへ切り替えるrelease gateでProjectもpublicへ切り替える。

管理情報のownerを次のように一意にする。

| Concern | Source of truth |
| --- | --- |
| workflow state | Project `Status` (`Triage` / `Ready` / `In progress` / `In review` / `Blocked` / `Done`) |
| implementation size | Project `Size` (`XS` / `S` / `M` / `L` / `XL`) |
| priority | `priority: p0`から`priority: p3`までのlabel |
| issue / change kind | `type:*` label |
| component ownership | `area:*` label |
| community suitability | `contribution:*` label |
| terminal resolution | `resolution:*` label |
| shippable target | GitHub Milestone |
| decomposition | Parent Issue / Sub-issue |

同じconcernをProject fieldとlabelへ重複保存しない。特に`status:*` labelは作らず、workflow stateはProject `Status`だけで表現する。`XL`はそのまま着手可能という意味ではなく、着手前にSub-issueへ分解すべき項目を表す。

Projectは少なくとも`Triage`、`Ready`、`Active`、`Roadmap`、`Community`、`Done`のViewを持つ。LayerDraw repositoryのIssueとPull RequestはProjectへ自動追加し、新規項目は`Triage`、closeまたはmergeされた項目は`Done`とする。`Done`の項目は30日後に自動archiveする。自動化できない状態遷移をlabelで代替しない。

Milestoneは実際に出荷可能な成果を表す場合だけ作成する。`V1`、`V2`等の曖昧なphase名、根拠のないdue date、単なるcomponent groupingには使用しない。Sprint / Iterationは明確なdelivery cadenceを採用するまで作成しない。

blank issueは無効化し、外部Contributorを構造化Issue Formへ誘導する。open-ended questionと初期ideaはDiscussionsで扱う。Issue Formの`projects:`指定は起票者のProject write権限を要求するためpublic contributor向け経路には使わず、Project側のauto-addを使用する。

pull requestは次を必須とする。

- problemとbehavior change
- affected components / delivery bundles
- test evidence
- schema / protocol / migration impact
- release note要否
- license / dependency impact

CLAを採用し、個人と法人のcontributionを追跡できるようにする。CLAは著作権を不必要に譲渡させるためではなく、source-available commercial distribution、relicensing、patent grantの法的権限を明確にするために使う。

### 6.5 Branch policy

- `main` を常時release可能なtrunkとする
- direct pushを禁止する
- merge queueを使用する
- required checks、required review、conversation resolutionを強制する
- mergeはsquashを標準とし、Changesetをrelease履歴の正本にする
- maintenance branchは既存majorのsecurity / critical fixに限る
- 長期feature branchをrelease integration単位にしない

## 7. CI Architecture

### 7.1 信頼境界

外部contributorのcodeは `pull_request` eventで実行し、write token、publish credential、signing key、cloud credentialへアクセスさせない。untrusted checkoutを扱うworkflowで `pull_request_target` を使わない。

- GitHub Actionsはcommit SHAへpinする
- workflow permissionはjob単位で最小化する
- OpenID Connectはrelease / deploymentのprotected jobだけに許可する
- secretを使うjobはapproved environmentとtrusted revisionを要求する
- fork PR artifactをrelease jobへ流用しない

### 7.2 Required pipeline

```text
policy
  -> changes
  -> generate-drift
  -> quality-go / quality-ts / docs
  -> unit-go / unit-ts
  -> conformance-native / conformance-wasm / conformance-sidecar
  -> integration
  -> packaged-smoke (affected delivery bundles)
  -> required
```

`required` jobは全required resultを集約する。path filterでjobを省略しても、required check自体が欠落しない構成にする。

### 7.3 Change detection

path-based executionは速度最適化に使えるが、依存DAGに従う。

- `schemas/**` は全generated package、Engine、WASM、SDK、server、conformanceを無条件に再検証する
- `internal/engine/**` はnative / WASM / sidecar conformanceを再検証する
- shared TS packageは全consumer bundleを再buildする
- release、license、workflow変更はpolicyとsecurity reviewを必須にする
- change detector自身が失敗した場合は広いtest setへfail-openせず、検証対象を拡大する

### 7.4 Test layers

| Layer | Purpose |
| --- | --- |
| Unit | package内部behavior |
| Contract | port / protocol request-response |
| Conformance | native、WASM、sidecar、serverの意味結果一致 |
| Integration | storage、query、registry、realtime adapterとの境界 |
| Packaged | build済みbinary / npm tarball / VSIX / containerを利用した検証 |
| E2E | delivery bundleの主要user workflow |

source treeからのtestだけでrelease可能と判定しない。配布する実artifactをinstall / executeするpackaged testを必須にする。

## 8. Release Model

### 8.1 Fixed release set

LayerDraw公式artifactは一つのLayerDraw release `vX.Y.Z` として扱う。internal Go packageを個別versioningしない。protocol、language、container、renderer / exporter profileは独立version axisを持ち、release manifestが対応関係を固定する。

fixed release setに含むもの:

- Go native binaries
- Server / Registry container images
- Engine WASM
- generated protocol package
- public TS packages
- Desktop installers
- VSCode VSIX
- MCP Apps bundle
- self-host chart / manifest
- release manifest、SBOM、signature、provenance

Marketplace審査やDesktop store審査の完了時刻はrelease transactionに含めない。同じrelease artifactを後から各channelへpromoteする。

Registryに公開する `.ldpack` / `.layerdraw` contentはfixed product release setに含めない。Registry service binaryとRegistry protocolはproduct releaseに含むが、contentは独立version、artifact digest、互換language / container rangeを持つ。

### 8.2 Changesets

変更者はuser-visible package / app変更にChangesetを追加する。互いに同じEngine / protocol / generated artifactへ固定される公式packageはfixed groupとして同じversionを持つ。

Changesetは次を表す。

- affected public artifacts
- semver impact
- user-visible summary
- migration / deprecation note

Changesetをpackage publishing scriptやschema migrationの代わりにしない。

### 8.3 Release channel

| Channel | Version / identity | Publication rule |
| --- | --- | --- |
| Stable | `vX.Y.Z` | signed tagとprotected approvalを必須とし、全primary channelへ配布する |
| Release candidate | `vX.Y.Z-rc.N` | prereleaseとして分離し、stable tagやstable updaterへ流さない |
| Nightly | source revision + build date | immutable検証用artifactとし、stable SemVerを消費しない |

Stableへの昇格時はrelease candidateのartifactをそのままstable versionへ改名しない。Stable tagからclean buildし、同じ再現可能入力で新しいrelease manifestと署名を作る。Nightlyは互換性契約やmigration supportの基準にしない。

### 8.4 Release flow

```text
merged Changesets
  -> automated Release PR
  -> version / changelog / lockstep manifest update
  -> protected approval
  -> signed tag
  -> clean build once
  -> packaged artifact verification
  -> SBOM / provenance / signature
  -> protected publish approval
  -> publish exact verified bytes
  -> channel promotion
  -> release verification
```

buildとpublishを分ける。publish jobはsourceから再buildせず、verified artifact digestとrelease manifestを受け取る。

### 8.5 Publish order

1. release manifest候補と全artifact digestを確定する
2. leaf npm packageをpublishする
3. facade / SDK packageをpublishする
4. binaries、WASM、Desktop、VSIXをGitHub Releaseへattachする
5. container imageをplatform digestでpushする
6. multi-architecture manifestをdigestから組み立てる
7. Helm / Homebrew / marketplace metadataを同じdigestへ更新する
8. 全配布先をread-backし、release manifestと一致することを検証する

途中失敗時に同じversionへ異なるbytesを上書きしない。未完了channelを記録して再開し、既にpublish済みartifactを再生成しない。

### 8.6 Distribution matrix

| Artifact | Primary channel | Secondary channel | Integrity |
| --- | --- | --- | --- |
| TS SDK / clients / UI packages | npm `@layerdraw/*` | GitHub Release metadata | npm provenance、digest |
| Go binaries / WASM / release manifest | GitHub Releases | package manager metadata | checksum、signature、SLSA provenance |
| Server / Registry images | GHCR | approved mirror | immutable digest、Cosign signature、SBOM |
| Self-host deployment | OCI Helm registry / GitHub Release | Docker Compose example | referenced image digest |
| Desktop | GitHub Release + updater manifest | OS stores | signed installer、release digest |
| VSCode | Visual Studio Marketplace | Open VSX + GitHub Release VSIX | signed release metadata、VSIX digest |
| MCP Apps | supported MCP distribution channel | GitHub Release bundle | bundle digest、capability manifest |
| Marketplace integration | Google Workspace Marketplace、Microsoft AppSource等の対象provider catalog | integration Web entrypoint | 同じserver / web digest、provider審査済みlisting revision |
| Homebrew | `layerdraw/homebrew-tap` | none | formula checksum generated from release |

Marketplace listingのOAuth client、redirect URI、tenant configuration、審査credentialはprivate cloudが所有する。provider向けにproduct sourceを別buildせず、public release manifestに含まれるserver / web artifact digestをlisting revisionへ関連付ける。

### 8.7 Supply-chain requirements

- artifactごとにSPDXまたはCycloneDX SBOMを生成する
- GitHub OIDCを使ってkeyless provenance / signatureを発行する
- containerはtagではなくdigestをrelease manifestへ記録する
- native binary、WASM、npm tarball、VSIX、installerのdigestを記録する
- custom licensed npm tarballはartifact rootへ正準LayerDraw License全文を`LICENSE`として同梱し、publish前にcanonical fileとのdigest一致を検証する
- third-party dependency licenseとvulnerabilityをCIで検査する
- release workflow自体への変更は通常code changeより強いapprovalを要求する

### 8.8 Registry content release

公式Registry contentは `layerdraw/registry-content` の独立pipelineで公開する。

```text
content pull request
  -> pinned LayerDraw Engineでvalidate / build
  -> asset license / publisher / dependency policy
  -> packaged `.ldpack` / `.layerdraw` test
  -> artifact digest / compatibility metadata
  -> publisher signature
  -> immutable Registry publish
  -> Registry read-back verification
```

content buildに使ったEngine releaseをprovenanceへ記録するが、content versionをLayerDraw product SemVerへ固定しない。同じcontent versionのartifact差し替えを禁止し、撤回はsigned tombstoneまたはdisabled metadataで表す。

## 9. SaaS Continuous Delivery

### 9.1 Public release と private deployment の分離

public product CIはartifactをbuild、test、sign、publishするところまでを所有する。SaaS production deploymentはprivate `layerdraw/cloud` が所有する。

private cloudはpublic release manifestを入力として、次だけを変更するpromotion pull requestを作る。

```text
release_version: vX.Y.Z
server_image_digest: sha256:...
registry_image_digest: sha256:...
web_asset_digest: sha256:...
schema_digests: ...
```

private CDでproduct artifactを再buildしない。

外部fork pull requestをproduction credential付きpreviewへdeployしない。ephemeral previewでprivate integrationが必要な場合は、trusted branchへ取り込んだrevisionと隔離済みtest tenant / credentialだけを使う。

### 9.2 Environment promotion

```text
ephemeral preview
  -> staging
  -> canary
  -> production
```

同じdigestを順にpromotionする。environmentごとの差はconfiguration、credential reference、scale、traffic policyだけに限定する。

各promotion gateは少なくとも次を検証する。

- release manifest signature
- image / asset digest
- protocol handshake
- schema / migration compatibility
- storage backend smoke test
- realtime and commit smoke test
- rollback compatibility
- observability health

### 9.3 Database / state migration

- expand / contractを使い、旧versionと新versionがcanary期間中に共存できるようにする
- destructive migrationをapplication deployと同じ一方向stepで実行しない
- document definition / state backend migrationはEngine / Runtimeのmigration contractに従う
- migration jobもrelease digestとmigration identifierを記録する
- rollback不能な変更は通常deploymentと別の承認を要求する

### 9.4 Rollback

rollbackは直前の既知のartifact digestへGitOps changeを戻す。tagの付け替え、image rebuild、database snapshotだけに依存するrollbackは禁止する。

## 10. License Boundary

root `LICENSE`を製品中核の正準条文とする。適用path、Apache-2.0 surface、Fixed Model Application、Commercial / OEM境界、content license、商標、CLA、具体例は[legal/README.md](legal/README.md)を唯一の入口とし、本書へ同じ条件を再定義しない。

Repository / release設計が担うのは次の機械的境界だけである。

- 各package manifestとsource headerへ正しいSPDX identifierを付ける。
- Apache-2.0 packageからLayerDraw License対象implementationへの依存を禁止する。
- publish artifactへ該当license、notice、third-party notice、SBOMを同梱する。
- Registry contentのlicenseとasset provenanceをproduct code licenseから分離する。
- CLA acceptanceとlicense変更approvalをmerge / release gateで検証する。
- private cloud固有codeをpublic product licenseの適用pathへ混入させない。

## 11. Enforcement

この設計は文書だけにせず、次をCIで強制する。

- Go / TS dependency graph policy
- generated file driftとschema digest
- component ownershipとrequired approver
- Changesetの要否
- package / path license matrix
- action SHA pinningとworkflow permission
- artifact SBOM、signature、provenance
- release manifestと公開先digestの一致
- private cloud deploymentでのdigest pinning
- distribution repositoryへの人手source commit禁止

## 12. 完了条件

Repository / Governance / Delivery設計への適合は、次をすべて満たした時に成立する。

- public product sourceとprivate SaaS operationsの境界が明示されている
- Engine / Runtime / delivery shellsを一つのatomic monorepo changeで更新できる
- root単一Go module、pnpm workspace、generated schema正本が維持される
- ownership、RFC、CLA、security reportingが定義される
- fork PRがsecret-bearing jobへ到達しない
- source testだけでなく配布artifact testがある
- 一度buildした同じbytesを署名し全channelへpromoteする
- SaaS CDがpublic artifactを再buildせずdigestで利用する
- release manifestがpackage、binary、protocol、schema、profileの対応を固定する
- source-available、permissive integration surface、content license、trademarkが区別される

## 13. 調査上の参考実装

この節は規範ではない。上記判断の検証対象とした公開projectを示す。

- [Grafana repository](https://github.com/grafana/grafana): Go / TypeScript monorepo、frontend task graph、artifact releaseの参考
- [n8n repository](https://github.com/n8n-io/n8n): pnpm / Turborepo、source-available license、multi-channel releaseの参考
- [Dify repository](https://github.com/langgenius/dify): application / container monorepo、path-aware CI、source-available termsの参考
- [Biome repository](https://github.com/biomejs/biome): native binary、WASM、npm artifactを同一sourceから配布する構成の参考
- [Tauri repository](https://github.com/tauri-apps/tauri): native / JS packageとcross-platform releaseの参考
- [Prisma engines](https://github.com/prisma/prisma-engines) と [Prisma](https://github.com/prisma/prisma): engineを別repositoryにした場合のcross-repository revision coordinationの比較対象
- [Kubernetes OWNERS](https://www.kubernetes.dev/docs/guide/owners/): reviewer / approverを分ける階層ownershipの参考
- [Elastic License 2.0](https://www.elastic.co/licensing/elastic-license): hosted / managed service制限の設計比較対象。LayerDrawへそのまま適用しない
