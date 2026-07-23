# Native menu localization bridge (design memo)

Status: design only. Phase 1 establishes the message catalog in `@layerdraw/react`
(`./i18n`) and routes the in-window shell strings through it. The Wails native
menu (`internal/desktopwails/run.go`, `nativeMenu`) still hard-codes English
labels (`"New Project"`, `"Open Project"`, the stock Edit menu). This memo fixes
the path for resolving those labels from the same catalog without letting the Go
layer own translation. Implementation is Phase 2.

## Constraint

Per `SKILL.md` rule 7 and `VISUAL_FOUNDATION.md` §14, menu labels are i18n-catalog
strings and the localized text is resolved on the TypeScript/catalog side, then
passed across the bridge already resolved. Go never embeds translated text and
never carries a second copy of the catalog (the localized `message` is outside
the semantic payload, mirroring the diagnostic-code rule). The catalog stays the
single source of truth in `@layerdraw/react/i18n`.

## Resolution flow

1. The frontend already resolves the active locale (`resolveLocale(navigator.language)`
   in `apps/desktop/src/mount.ts`) and can later read an explicit settings override.
2. On startup and whenever the locale changes, the frontend builds a
   `NativeMenuStrings` record by resolving a fixed set of menu keys through the
   active `Translator` (`t.t("menu.file.new")`, etc.) and hands it to Go through a
   new binding, e.g. `FrontendBridge.SetMenuStrings(strings NativeMenuStrings)`.
3. Go stores the resolved strings and (re)builds the `menu.Menu` from them,
   calling `runtime.MenuSetApplicationMenu` to apply the update live. Command
   routing (`invokeNativeCommand`) is unchanged — only labels are swapped.
4. Menu command identity remains the stable `desktopcontract.Command*` enum, never
   the label, so a locale change never affects invocation or the typed command route.

## Catalog keys (add to the shared catalog in Phase 2)

`menu.app.about`, `menu.app.settings`, `menu.app.language`, `menu.app.quit`,
`menu.file`, `menu.file.new`, `menu.file.open`, `menu.file.openRecent`,
`menu.file.close`, `menu.edit`, `menu.view`, `menu.view.mode2d`,
`menu.view.mode25d`, `menu.view.zoom`, `menu.view.panels`, `menu.window`,
`menu.help`. Both `en` (canonical) and `ja` land in the same commit; the existing
`findCatalogGaps` completeness test covers them.

## Bridge shape

`NativeMenuStrings` is a flat `Record<string, string>` of resolved labels — no
codes, no arguments, no catalog. It carries only presentation text, so it fits the
"no arbitrary details / no semantics in the transport" rule of
`desktop-wails-contracts.md`. Because it is a new generated binding, the Phase 2
change must also extend the binding parity/compatibility fixtures
(`schemas/fixtures/desktop/owner-binding-parity-v1.json`,
`wails-binding-compatibility-v1.json`) and the packaged accessibility probe, which
is why it is deferred rather than folded into the presentation-only Phase 1.

## Accessibility parity

Embedded visible controls and the native menu must present the same labels and
invoke the same command route (`desktop-wails-contracts.md`). Both therefore read
from the same catalog: in-window controls via `useI18n().t(...)`, the native menu
via the resolved `NativeMenuStrings` snapshot. A single locale switch updates both.
