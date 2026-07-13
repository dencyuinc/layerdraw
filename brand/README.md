# LayerDraw Brand Assets

The SVG files in this directory are the canonical brand sources. Raster files
and the favicon are generated from them by `tools/generate-brand-assets.sh`.

## Canonical Sources

| File | Use |
| --- | --- |
| `layerdraw-icon.svg` | Icon source |
| `layerdraw-logo-on-light.svg` | Logo for light backgrounds |
| `layerdraw-logo-on-dark.svg` | Logo for dark backgrounds |

## Generated Assets

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
| `png/layerdraw-logo-on-light-1200.png` | Raster logo for light backgrounds |
| `png/layerdraw-logo-on-dark-1200.png` | Raster logo for dark backgrounds |
| `favicon.ico` | Multi-resolution 16, 32, and 48 pixel favicon |
| `github-social-preview.png` | 1280 x 640 GitHub social preview |

Regenerate all derived assets from the repository root:

```sh
./tools/generate-brand-assets.sh
```

Use of LayerDraw names and marks is governed by the
[Trademark Policy](../docs/legal/trademarks.md).
