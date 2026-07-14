# Docs

layerdraw の企画・要件定義・設計メモを管理するディレクトリです。

このrepositoryは公開前提で再構築する正準source treeである。本directory内のpathは、今後の実装・CI・release・配布の基準になるTo-Be layoutを表す。

## Files

- [requirements.md](requirements.md): 要件定義
- [blueprint.md](blueprint.md): LayerDraw 全体設計ブループリント
- [architecture.md](architecture.md): Go Engine、TS presentation、framework shell、提供物を分離する規範Technical Architecture
- [component-package-boundary-specification.md](component-package-boundary-specification.md): Go / TS package、generated schema、binary、framework shell、delivery bundleの依存境界
- [system-boundary-contracts-specification.md](system-boundary-contracts-specification.md): Engine Protocol、Runtime / Host Port、Realtime、Query adapter、Registry、Render / Export、SDK、Version / Releaseの規範契約
- [engine-stdio-framing-v1.md](engine-stdio-framing-v1.md): Engine native stdio transportのLDSP framing 1.0 byte grammar、canonical chunk、golden fixtureの規範契約
- [repository-governance-and-delivery.md](repository-governance-and-delivery.md): To-Be repository topology、monorepo、governance、CI、release、配布、SaaS CD、license enforcementの規範
- [legal/README.md](legal/README.md): License本文、適用マトリクス、use case、商標、CLA、Contributor Privacy Notice、Apache surfaceの入口
- [compiler-architecture.md](compiler-architecture.md): LDL Compiler / Workbench / Search / Query / Analysis / View / ExportPlan の入出力と実装境界
- [search-query-and-analysis.md](search-query-and-analysis.md): FTS / Vector / Hybridによる起点発見、決定論的Structural Query、Ladybug graph analysis、UI / MCP workflowの規範
- [authoring-access-control.md](authoring-access-control.md): Schema定義とGraph instance編集を分離し、SDK / MCP / Serverの全書込経路で強制するAuthoring Policy契約
- [ai-integration.md](ai-integration.md): AI / MCP の局所取得、編集mode、token budget、host capability契約
- [collaboration-and-auth.md](collaboration-and-auth.md): 認証・権限・共同編集方針
- [layerdraw-server.md](layerdraw-server.md): LayerDraw Server とストレージ分離方針
- [ldl-language-specification.md](ldl-language-specification.md): `.ldl`の規範source syntax、StableAddress、module、型、Query、View、identity migration
- [ldl-language-detailed-specification.md](ldl-language-detailed-specification.md): Language 1の規範defaults、Normalized Model、Query/ViewData評価、format/hash、diagnostics、semantic operations
- [product-quality-gates.md](product-quality-gates.md): 製品品質として満たすべき実装ゲート
- [project-management.md](project-management.md): プロジェクト管理・ログイン導線方針
- [registry-and-templates.md](registry-and-templates.md): Pack Registry と Template Registry 方針
- [entity-type-system.md](entity-type-system.md): EntityType / Entity のclass・instance境界とproduct behavior
- [relation-type-system.md](relation-type-system.md): RelationType の意味・制約・View 連携方針
- [state-backends.md](state-backends.md): `.ldl` と state を分離した remote / local backend 設計
- [system-fields-and-provenance.md](system-fields-and-provenance.md): created/updated と provenance / freshness の扱い
- [views-and-projections.md](views-and-projections.md): View / Projection / Query による切り出し方針
- [view-conversion-contract.md](view-conversion-contract.md): 正本グラフからViewData、RenderData、ExportPlan、ExportArtifactへ変換する契約
