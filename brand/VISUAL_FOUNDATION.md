# LayerDraw Visual Foundation

## 1. Scope

この文書は、LayerDrawのLP、product UI、embedded SDK、Desktop、VS Code、MCP Apps、
Canvas、Graph、View previewに共通する視覚規則を定義する。

正本の優先順位は次の通りである。

1. `tokens.json`: 実装値
2. この文書: tokenの意味と使用規則
3. `BRAND_GUIDELINES.md`: brand表現とlogo規則
4. 各surfaceのdesign: 上記を具体的なworkflowへ適用したもの

ViewData、RenderData、ExportPlanの意味論や決定性は設計文書が所有する。この文書は
visual semanticsを定義するが、rendererやexporterが正本graphの意味を推測する根拠にはしない。

## 2. Design principles

### 2.1 Structure is visible

階層、所属、relation、選択範囲は、余白、alignment、境界、connectorで見せる。
意味のないcard、装飾線、色面を追加しない。

### 2.2 Quiet workbench

LayerDrawは長時間使う専門toolである。neutral surfaceを基本とし、brand colorは操作と
現在位置に使う。主要情報よりUI chromeを目立たせない。

### 2.3 Dense, not cramped

一度に多くの構造を比較できる密度を保ちながら、control、label、値の境界を明確にする。
固定寸法を持つtoolbar、panel、table rowを、内容によって伸縮させない。

### 2.4 Meaning survives color

状態やrelationを色だけで区別しない。text、icon、line style、shape、labelを併用し、
grayscale、high contrast、printでも意味を維持する。

### 2.5 Same fact, same visual result

同じViewData、theme、renderer profile、font、assetからは同じvisual resultを作る。
random color、実行順依存layout、hostごとの暗黙fallbackを使わない。

## 3. Token model

`tokens.json`は次の層を持つ。

| Layer | Purpose | Example |
| --- | --- | --- |
| Primitive | 生の色、寸法、font、duration | `color.violet.700` |
| Semantic | UI上の意味 | `theme.light.action.primary.background` |
| Visualization | Canvas、Graph、Categoryの意味 | `theme.light.visualization.relation.default` |
| Component | 必要な場合だけ設ける具体値 | `component.control.height.md` |

componentはprimitiveを直接参照せず、可能な限りsemantic tokenを使う。新しい画面のために
`purpleButton`や`grayPanel2`のような見た目由来のtokenを追加しない。

themeは`light`、`dark`、`highContrast`を持つ。surfaceがhost themeへ追従する場合も、
必要なsemantic token一式を解決してからcomponentへ渡す。

## 4. Color system

### 4.1 Neutral foundation

Light themeは白とcool neutral、Dark themeは青みを抑えたgraphiteを使う。
blue-gray一色やpurple-tinted grayで画面全体を構成しない。

| Role | Light | Dark |
| --- | --- | --- |
| App background | `#F4F6F8` | `#111318` |
| Canvas background | `#F8F9FB` | `#0D0F13` |
| Surface | `#FFFFFF` | `#191C22` |
| Subtle surface | `#EEF1F4` | `#21252D` |
| Strong border | `#B7BDC7` | `#4A515E` |
| Primary text | `#17191E` | `#F3F4F6` |
| Secondary text | `#5F6672` | `#ADB3BD` |

### 4.2 Brand action

Light themeのprimary actionは`#5E17EB`にwhite textを使う。Dark themeでは
`#A78BFA`に`#111318` textを使う。既定状態でWCAG AA相当のcontrastを確保する。

primary actionを一つのscopeに複数並べない。同格のactionが複数ある場合は、primaryを
一つ選び、残りをsecondaryまたはghostにする。

### 4.3 Status

| Status | Hue | Required companion |
| --- | --- | --- |
| Information | Blue | info iconまたはlabel |
| Success | Green | check iconまたは完了text |
| Warning | Amber | warning iconと具体的な影響 |
| Danger | Red | danger iconと不可逆性の説明 |

status colorをEntityTypeやLayer categoryへ流用しない。category colorがstatusに見える場合は、
neutral outline、category label、legendを併用する。

### 4.4 Interaction states

- hoverはbackgroundまたはborderを一段変える。layout、font weight、border widthを変えない。
- activeはhoverより強くし、押下中であることを示す。
- selectedはpersistent stateであり、hoverと同じ見た目にしない。
- focus ringは2 px、component外側へ2 px offsetを基本とする。
- disabledはopacityだけに依存せず、cursor、contrast、interactionを同時に無効化する。
- destructive actionは通常時からred textまたはborderを持ち、確認画面だけで突然redにしない。

## 5. Typography

### 5.1 Roles

| Role | Size / line-height | Weight |
| --- | --- | --- |
| Marketing display | 48 / 56 | 700 |
| Page title | 30 / 38 | 650 or 700 |
| Section title | 24 / 32 | 600 |
| Panel title | 16 / 24 | 600 |
| Body | 14 / 20 or 16 / 24 | 400 |
| Control | 14 / 20 | 500 |
| Caption | 12 / 16 | 400 or 500 |
| Code | 13 / 20 | 400 |

mobileではMarketing displayを38 / 48まで下げる。viewport width連動のfont sizeを使わない。
product workbenchではPage title以上のscaleを原則使用しない。

### 5.2 Content rules

- 一行の本文は日本語40字前後、英語70 character前後を上限の目安とする。
- button labelを省略表示しない。収まらない場合はbutton幅またはlayoutを変える。
- identifierとhuman labelを同時に見せる場合は、human labelを主、identifierをsecondaryにする。
- 数値列はtabular figuresを使い、decimal alignmentが必要な表では右揃えにする。
- LDL source、StableAddress、diagnostic codeはmonoを使う。

## 6. Spacing and layout

4 pxを基本単位とし、主要spacingは8 px刻みで構成する。

| Context | Default |
| --- | ---: |
| Inline icon gap | 4-8 px |
| Control group gap | 8 px |
| Compact row padding | 8 px vertical / 12 px horizontal |
| Panel padding | 16 px |
| Page gutter | 24 px desktop / 16 px mobile |
| Section separation | 48-80 px marketing |
| Marketing content max width | 1200 px |
| Reading content max width | 760 px |

### 6.1 Product shell

- application header: 48 px
- standard toolbar: 40 px
- compact control: 32 px
- standard control: 36 px
- touch target: minimum 44 x 44 px when touch is primary
- navigation sidebar: 240-288 px
- inspector: 320-400 px

panel widthはdrag中を除いてcontentで変化させない。Canvasをdecorative card内に置かず、
workspaceの主面として扱う。

### 6.2 Marketing layout

LPでは12-column gridを基準にするが、sectionをcard gridへ自動変換しない。
product screenshot、live View、比較表など、内容に適したfull-width compositionを使う。

heroの基本構成は次とする。

```text
+------------------------------------------------------------------+
| logo / navigation                                      action    |
|                                                                  |
| LayerDraw                                                        |
| Don't draw diagrams. Define the structure.                       |
| supporting copy                         primary / secondary       |
|                                                                  |
|               actual graph, view, or live product                 |
|                                                                  |
+----------------------- next section hint ------------------------+
```

左右にtext cardとimage cardを並べるsplit heroを標準形にしない。

## 7. Shape, border, and elevation

- standard radiusは6 pxとする。
- compact controlは4 px、large media frameは8 pxまでとする。
- pillはstatus、tag、segmented controlなど意味がある場合だけ使う。
- page sectionをfloating cardにしない。
- card内へ別cardを入れない。
- borderは1 pxを基本とし、selectionやfocusだけ2 pxを使える。
- shadowはoverlay、menu、dialog、dragged objectへ限定する。
- panelの区切りはshadowよりborderを優先する。

## 8. Icons and controls

product UIのsystem iconはLucideを第一候補とし、同一surfaceで別icon familyを混ぜない。
brand iconとsystem iconは役割を分ける。

- icon buttonにはaccessible nameとtooltipを持たせる。
- save、undo、redo、zoom、closeなど既知の操作はiconを優先する。
- unfamiliar command、destructive action、primary workflowはiconとtextを併用する。
- binary settingはswitchまたはcheckboxを使う。
- modeはsegmented control、view切替はtabs、option setはmenuを使う。
- color選択はswatchを伴う。
- numeric valueはinput、stepper、sliderを意味に応じて使い分ける。

iconのstroke widthやsizeをhoverで変更しない。通常は16、18、20、24 pxのいずれかを使う。

## 9. Canvas and graph

### 9.1 Canvas

Canvasはapp shellとわずかに異なるbackgroundを持ち、境界をshadowではなくsurface差で示す。

- minor gridは低contrast、major gridはminorより一段強くする。
- zoom out時にgrid密度を自動調整し、画面をmoire patternにしない。
- guide、snap target、selection boxは別tokenを使う。
- empty Canvasに説明cardを置かず、必要な最初のactionをCanvas上の簡潔なempty stateで示す。

### 9.2 Entity

- Entityのfill、stroke、textはthemeとcategoryから解決する。
- labelは常に読めるcontrastを持つ。
- selectionはcategory色の置換ではなく、外側haloまたは二重outlineで示す。
- state変化でEntity寸法を変えない。
- image assetがあるEntityもlabelとtypeを失わない。

### 9.3 Relation

- default Relationはneutral strokeを使う。
- direction、cardinality、kindはarrow、endpoint、line pattern、labelを組み合わせる。
- colorだけでRelationTypeを区別しない。
- selected Relationは太さを増やすだけでなく、haloまたはendpoint highlightを使う。
- relation crossingを装飾で隠さず、layout、bridge、routingで読み分けられるようにする。

### 9.4 Layer

Layerは常に大きな色面として塗らない。薄いfill、境界、label railを組み合わせ、
EntityとRelationより前面へ出ないようにする。

重ね合わせViewでは、Layer間relationがcontainmentを意味する場合だけ枠のnestingへ変換する。
単なるcross-layer relationを視覚的な親子関係へ変換しない。

### 9.5 Categorical palette

category paletteは8組のfill、stroke、textを持つ。割り当てはStableAddress等の安定した入力から
決定し、同じView内で実行ごとに変化させない。user指定色がある場合もcontrast validationを通す。

8色を超えるcategoryは色だけで区別せず、label、group、pattern、filterを使う。

### 9.6 Presence

共同編集者のpresence colorはgraph categoryと別paletteを使う。cursor、selection outline、name labelを
同じ色で結び、色だけでactorを識別させない。actor名またはavatarを必ず併記する。

## 10. Tables, matrices, and forms

LayerDrawのViewはdiagramだけではない。tableとmatrixを第一級surfaceとして扱う。

- headerをstickyにできる構造にする。
- row heightはcompact 32 px、standard 40 pxを基本とする。
- zebra stripeを既定にせず、row borderとhoverで追跡可能にする。
- type、required、provenance、updated timeは独立columnとしてsort/filter可能にする。
- missing、empty、null、not applicableを同じ表示にしない。
- relation cellはlabelだけでなく参照先へnavigateできるようにする。
- matrixはrow/column headerを固定し、交点の意味をlegendへ明示する。
- formは関連fieldをsectionで並べ、cardの入れ子にしない。
- advanced optionは初期値と影響をsummaryで見せ、単に「詳細設定」とだけ書かない。

## 11. Motion

motionは構造変化を追跡するために使う。

- hover feedback: 80 ms
- control transition: 140 ms
- panel / overlay: 220 ms
- graph relayout: 220-360 ms。sourceとdestinationを追跡できるeasingを使う
- decorative loop animationは使わない
- `prefers-reduced-motion`では非本質的なmotionを0 msにする

複数View更新のdemoでは、一つのfact変更からaffected Viewが更新される順序を見せる。
常時脈動するnodeや無関係な線の流動をAI表現として使わない。

## 12. Accessibility

- normal textは4.5:1以上、large textと主要graphic boundaryは3:1以上を基準とする。
- keyboard focusを常に視認可能にする。
- pointerだけで利用する操作を作らない。
- hoverでのみ現れる必須情報を作らない。
- controlはname、role、value、stateを支援技術へ公開する。
- errorは対象fieldとsummaryの両方から辿れるようにする。
- color vision simulation、200% zoom、forced colors、reduced motionで検証する。
- Canvas操作にはlist、outline、table等の代替navigationを用意する。

High Contrast themeではbrand fidelityよりsystem contrastを優先する。OSのforced color modeでは
system colorへのmappingを許可し、logoだけ正規assetを維持する。

## 13. Responsive behavior

- fixed desktop layoutを縮小表示しない。
- mobileではpanelをdrawerまたはsequential screenへ変換する。
- toolbar commandはpriorityに基づいてoverflow menuへ移す。
- textとcontrolを重ねない。
- Canvasの最小操作領域を確保し、side panelがviewportを占有し続けない。
- LPではbrand、literal offer、CTA、product visualの順序を保つ。
- SDK embedではhost containerのsizeを観測し、global viewportだけでbreakpointを決めない。

## 14. Surface adaptation

| Surface | Adaptation | Must remain invariant |
| --- | --- | --- |
| Web | Full LayerDraw shell | semantic colors、focus、graph rules |
| Desktop | Native window chromeとの接続 | workbench density、tokens、renderer result |
| VS Code | VS Code theme bridge | LDL diagnostics、graph semantics、contrast |
| Client SDK | host typographyとsurfaceへ限定的に適応 | capability、focus、status、View rendering |
| MCP Apps | narrow containerとstreaming stateへ適応 | progressive result、selection、error semantics |
| Export | medium固有のfont/raster profile | source meaning、category mapping、legend |

host colorを受け入れるSDKでも、semantic tokenを個別CSS selectorへ直結させない。host adapterが
LayerDraw token contractを満たすtheme objectを構成し、欠損tokenは明示的にfallbackする。

## 15. Landing page acceptance

LP implementationは少なくとも次を満たす。

- Light、Dark、mobile、wide desktopでlogo variantが正しい。
- first viewportでLayerDrawとproductの実体が識別できる。
- primary CTAが一つに定まっている。
- next sectionがfirst viewportから完全に隠れない。
- purple-dominantなsurface、gradient background、decorative orbがない。
- 実際のproductまたは正直なprototype以外を完成画面として見せていない。
- heading、body、button、captionがtoken scaleへ一致する。
- keyboard navigation、focus、contrast、reduced motionを検証している。
- desktopとmobileのscreenshotでoverflow、overlap、切れがない。
- social card、favicon、Open Graph imageが正規brand assetを使う。

## 16. Change control

次はVisual Foundationの互換性に影響する変更としてreviewする。

- semantic tokenの削除または意味変更
- core color、type family、base spacing、radiusの変更
- category colorの順序変更
- focus、status、selectionの表現変更
- rendererとinteractive UIで異なるvisual semanticsを導入する変更

token名を変更する場合は、consumerを同じpull requestで更新する。release済みSDKで公開した
theme contractを破壊する場合は、versioned migrationを用意する。
