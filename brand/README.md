# LayerDraw Brand Kit

このディレクトリは、LayerDrawのブランド表現とVisual Foundationの正本である。
LP、Web、Desktop、VS Code、SDK、MCP Apps、ドキュメント、動画、イベント素材を
作る際は、ここにある規則とassetを共通して使用する。

## 正本文書

| File | Scope |
| --- | --- |
| [`BRAND_GUIDELINES.md`](BRAND_GUIDELINES.md) | ロゴ、ブランドカラー、背景、余白、コピー、禁止事項 |
| [`VISUAL_FOUNDATION.md`](VISUAL_FOUNDATION.md) | UI、LP、Canvas、Graph、Typography、Layout、Accessibility |
| [`tokens.json`](tokens.json) | 色、文字、余白、寸法、角丸、影、motionの機械可読な正本 |

法的な名称・商標利用条件は[Trademark Policy](../docs/legal/trademarks.md)だけが所有する。
ブランドガイドラインは法的許諾を追加または変更しない。

## 正本asset

SVGをロゴとアイコンの正本とする。PNGとfaviconはSVGから生成し、手作業で修正しない。

| File | Use |
| --- | --- |
| `layerdraw-icon.svg` | アイコンの正本 |
| `layerdraw-logo-on-light.svg` | 明るい背景用ロゴの正本 |
| `layerdraw-logo-on-dark.svg` | 暗い背景用ロゴの正本 |

## 生成asset

| File | Intended use |
| --- | --- |
| `png/layerdraw-icon-16.png` | Browser icon source |
| `png/layerdraw-icon-32.png` | Browser icon source |
| `png/layerdraw-icon-48.png` | Browser icon source |
| `png/layerdraw-icon-128.png` | VS Code extension icon |
| `png/layerdraw-icon-180.png` | Apple touch icon |
| `png/layerdraw-icon-192.png` | PWA icon |
| `png/layerdraw-icon-256.png` | High-density extension icon |
| `png/layerdraw-icon-512.png` | PWA icon |
| `png/layerdraw-icon-1024.png` | High-resolution app icon source |
| `png/layerdraw-logo-on-light-1200.png` | 明るい背景用raster logo |
| `png/layerdraw-logo-on-dark-1200.png` | 暗い背景用raster logo |
| `favicon.ico` | 16、32、48 pixel favicon |
| `github-social-preview.png` | 1280 x 640 GitHub social preview |
| `og-image.png` | 1200 x 630 Open Graph image |

生成assetはrepository rootから再生成する。

```sh
./tools/generate-brand-assets.sh
```

## 実装への供給

`tokens.json`をbrand tokenの正本とする。将来のCSS custom properties、TypeScript、
Wails、VS Code theme bridge、renderer profile向け定数は、このfileから生成する。
生成先を直接編集したり、提供形態ごとに独自paletteを定義したりしてはならない。
