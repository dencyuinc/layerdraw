# LayerDraw Brand Guidelines

## 1. 目的

LayerDrawは、図を手作業で描き直すためのdrawing toolではない。型付きgraphを正本として
定義し、一つの変更から複数のviewを再生成するための構造化platformである。

ブランド表現は次を伝える。

- structure first
- one source, many views
- human-readable and agent-operable
- precise without being intimidating
- quiet enough for long-running professional work

ブランドをgenericなAI product、紫色のSaaS dashboard、またはnodeを散らしただけの
network illustrationとして表現しない。

## 2. Brand statement

Product nameは常に`LayerDraw`と綴る。`Layer Draw`、`layerDraw`、`Layerdraw`へ変更しない。

英語の主要messageは次を使用できる。

> Don't draw diagrams. Define the structure.
>
> Change one fact. Update every view.

これはcampaign copyであり、product nameやrepository descriptionの代わりではない。
LPのH1では`LayerDraw`または提供する対象を明示し、上記messageはsupporting copyとして使う。

日本語では直訳を固定taglineにしない。画面と文脈に応じて、例えば
「構造を一度定義し、必要なビューを生成する」のように機能を具体的に説明する。

## 3. Logo system

### 3.1 正本

| Background | Asset |
| --- | --- |
| Light | `layerdraw-logo-on-light.svg` |
| Dark | `layerdraw-logo-on-dark.svg` |
| Square icon surface | `layerdraw-icon.svg` |

WebとdocumentではSVGを優先する。PNGはSVGを扱えないsurfaceだけで使う。

### 3.2 Clear space

full logoの周囲には、logo heightの25%以上のclear spaceを確保する。
icon単体ではicon widthの20%以上を確保する。clear space内に文字、罫線、画像、別logo、
click targetの境界を入れない。

### 3.3 Minimum size

| Asset | Digital | Print |
| --- | ---: | ---: |
| Full logo | 160 px wide | 42 mm wide |
| Icon | 24 px square | 8 mm square |

16 px以下のbrowser surfaceではfull logoやSVG iconを縮小せず、生成済みfaviconを使う。

### 3.4 Background

- Light logoは`background.page`、`background.surface`、白に近い無地背景で使う。
- Dark logoは`background.inverse`、`background.app`のDark theme、黒に近い無地背景で使う。
- 写真やdiagram上へ置く場合は、logo全体で十分なcontrastを持つ静かな領域を確保する。
- contrastを確保できない場合は、写真を暗く加工するのではなく無地のbrand bandを設ける。

### 3.5 禁止事項

- logoの比率、角丸、wordmark間隔を変更しない。
- iconとwordmarkを切り離して独自に再配置しない。
- logoを回転、斜体、outline化、立体化しない。
- shadow、glow、stroke、別gradientを追加しない。
- logo内部の色をsurfaceに合わせて再着色しない。
- on-light assetをDark backgroundで、on-dark assetをLight backgroundで使わない。
- logoをpatternや透かしとして反復しない。
- product screenshotよりlogoを大きくして、実体のないbrand pageにしない。

monochrome logoが必要なsurfaceでは既存assetを加工せず、正規variantを追加してから使う。

## 4. Brand color

### 4.1 Core colors

| Name | Value | Role |
| --- | --- | --- |
| LayerDraw Violet | `#5E17EB` | Light themeのprimary action、brand accent |
| LayerDraw Violet Light | `#A78BFA` | Dark themeのbrand accent |
| LayerDraw Ink | `#373A3D` | Light background上のwordmark |
| LayerDraw White | `#FFFFFF` | symbol、Dark background上の主要要素 |
| LayerDraw Soft White | `#FCFBFF` | Dark background上のwordmark |

logo icon内のgradientはmark固有の表現である。gradientをbutton、background、heading、
section dividerへ展開しない。

### 4.2 Color balance

LPとproduct UIでは、neutral surfaceを面積の大部分に使い、Violetは操作と識別へ限定する。
目安として、一画面でbrand colorが占める面積は10%未満に保つ。これは機械的な検査値ではなく、
purple-dominantな画面を避けるためのreview基準である。

Violetを次へ使用する。

- primary action
- current selection
- keyboard focus
- active navigation
- LayerDraw固有の短いaccent

Violetを次へ使用しない。

- page全体のbackground
- large card群のfill
- success、warning、dangerの代用
- graph categoryすべての既定色
- 長文本文

UIとvisualizationの完全な色指定は[`VISUAL_FOUNDATION.md`](VISUAL_FOUNDATION.md)と
[`tokens.json`](tokens.json)に従う。

## 5. Typography

### 5.1 Font families

| Role | Preferred | Fallback |
| --- | --- | --- |
| Product and marketing sans | Inter | Noto Sans JP, system-ui, sans-serif |
| Code, LDL, identifiers | JetBrains Mono | SFMono-Regular, Consolas, monospace |

日本語本文は`Noto Sans JP`を優先してよい。font fileを配布物へbundleする場合は、
license、subset、weight、fallback metricをrelease artifact単位で固定する。

### 5.2 Rules

- 通常本文は400、controlとlabelは500、headingは600または700を使う。
- すべてのletter spacingは`0`を基本とする。
- viewport widthに比例してfont sizeを変えない。
- product UI内でhero scaleを使わない。
- StableAddress、LDL、log、table値はmonoを使えるが、説明文全体をmonoにしない。
- uppercaseは短いtechnical labelに限定し、日本語へ適用しない。

## 6. Visual signature

LayerDrawのsignatureは「Layer Rails」である。layer、relation、viewへのprojectionを、
contentに沿って整列する細いrail、境界、接続として見せる。

- railは実際の情報階層、section boundary、またはgraph relationを示す。
- randomなnode networkや背景装飾として使わない。
- railは水平または垂直を基本とし、layout gridへ揃える。
- Violetはactive railまたはprojection結果の一部だけに使う。
- screenshotやlive visualizationがある場合は、それ自体を主役にする。

Layer Railsはlogo iconの模倣ではない。logo markを分解して背景装飾へ転用しない。

## 7. Photography and product imagery

LayerDrawの主要visualは実際のproduct state、graph、view、table、export結果とする。

- inspection可能な明るさと解像度を維持する。
- UIを極端に傾けたmockup、強いblur、暗いoverlayで隠さない。
- genericなserver room、会議、AI robot、抽象network stock imageを主役にしない。
- animationはmaster graphの変更が複数viewへ反映される一つの流れに集中させる。
- empty repository段階で架空の完成UIをproduct screenshotとして表示しない。

## 8. Voice

LayerDrawの文章は、構造を扱う人へ直接、具体的に説明する。

- active voiceを使う。
- 一つの文で一つのことを伝える。
- feature名より、利用者が制御する対象を先に書く。
- AI、realtime、semanticなどの語を価値の説明なしに並べない。
- 「簡単」「革新的」「次世代」のような根拠のない形容を避ける。
- errorは原因と回復操作を示し、謝罪や曖昧な失敗messageにしない。

Command labelは結果と一致させる。`Publish`を押した後の完了messageは`Published`とし、
途中で`Submit`や`Deploy`へ言い換えない。

## 9. Landing page baseline

LPはmarketing card collectionではなく、LayerDrawの構造と実物を見せるproduct surfaceとして作る。

- first viewportで`LayerDraw`と実際のgraphまたはviewを認識できるようにする。
- H1はproduct nameまたはliteralなofferとし、抽象的なvalue propositionだけにしない。
- heroはsplit cardにせず、textと実際のproduct visualを同一compositionに置く。
- next sectionの一部をfirst viewportに残す。
- primary CTAは一つに絞り、Violetを使う。
- sectionをfloating cardの連続にしない。full-width bandまたはunframed layoutを使う。
- purple gradient background、orb、bokeh、decorative graphを使わない。
- mobileでもproduct名、CTA、product visualの順序を維持する。

## 10. Governance

logo source、core color、product name、visual signatureの変更はbrand changeとしてreviewする。
tokenの追加は意味と利用箇所を明示し、既存tokenで表現できないことを確認する。

提供形態固有の都合で正本tokenを上書きしない。host themeへ適応する場合も、LayerDrawの
focus、status、graph semantics、contrast contractを維持する。

商標の利用可否は[Trademark Policy](../docs/legal/trademarks.md)に従う。
