---
name: layerdraw-ui
description: LayerDraw UI/UX design governance. Use BEFORE writing or reviewing ANY user-facing UI code or mockups for LayerDraw surfaces (Desktop shell, Browser Editor, panels, dialogs, menus, canvas chrome). Encodes the canonical design system (brand/tokens.json + brand/VISUAL_FOUNDATION.md), i18n requirements, component placement rules, and the UX review checklist. Triggers on UI implementation, screen design, CSS/styling, component authoring, mockups, menus, error display, or theming work.
---

# LayerDraw UI Skill

LayerDraw already has a canonical, normative design system. Your job is to apply
it, never to invent an alternative. Precedence: the user's explicit direction >
this skill > your own taste.

## Canonical sources (read before designing)

1. `brand/tokens.json` — the ONLY source of color, type, spacing, radius,
   shadow, and motion values. Run `python3 .claude/skills/layerdraw-ui/scripts/tokens.py`
   for a flattened cheat sheet (`--theme light|dark|highContrast`, `--grep <term>`).
2. `brand/VISUAL_FOUNDATION.md` — token semantics and usage rules.
3. `docs/product-quality-gates.md` — UI/UX quality gates (three-pane workspace,
   pointer handling, canvas requirements).
4. `brand/BRAND_GUIDELINES.md` — logo and wordmark rules.

## Design principles (from VISUAL_FOUNDATION, enforced)

- **Quiet workbench**: neutral surfaces; brand violet is reserved for the
  primary action, selection, focus, and current location. If violet appears in
  more than a few places on a screen, you are misusing it.
- **Structure is visible**: hierarchy via whitespace, alignment, borders,
  connectors. No decorative cards, rules, or color fields that carry no meaning.
- **Dense, not cramped**: workbench density (rows ~44px, 13-14px body). Fixed
  toolbar/panel/row dimensions; content never stretches chrome.
- **Meaning survives color**: never encode state by color alone — pair with
  text, icon, line style, or shape.
- **Same fact, same visual result**: no random colors, no execution-order
  layout, no host-specific fallbacks.

## Hard rules (violations are rejected in review)

1. **Semantic tokens only.** Components reference semantic tokens
   (`theme.<mode>.action.primary.background`), not raw hex and not primitive
   tokens. Never add look-derived tokens (`purpleButton`, `grayPanel2`).
2. **No hardcoded user-facing strings.** Every string resolves through the i18n
   catalog (base `en`, complete `ja`). Adding a string means adding both
   locales. Locale switching must not require component changes.
3. **No internal identifiers in primary UI.** `doc_…` ids, revision hashes,
   session ids, state-machine names belong in diagnostics affordances only.
4. **Errors show reason + code.** Pattern: human-readable reason, stable code
   in parentheses, optional "Show details". Never a reason-less toast; always
   log the underlying cause. Diagnostics arrive as stable code + structured
   args; translation happens client-side (localized message is outside the
   semantic payload — see ldl-language-detailed-specification.md).
5. **Component placement.** Reusable presentation → `packages/react` (shared
   with Browser Editor/SDK). Desktop-only chrome (window controls, native
   dialog triggers, menu wiring) → `apps/desktop`. Never fork a shared
   component into the app layer.
6. **No semantics in TypeScript.** UI composes Engine semantic operations via
   `packages/composer` and renders Engine-produced ViewData via
   `packages/render`. Never generate or edit LDL source text in TS, never
   reimplement query/view/access logic (architecture.md 11.2).
7. **Native menus are part of the product.** macOS: system menu bar; Windows/
   Linux: in-window menu bar (Wails native menu). Menu labels are i18n-catalog
   strings rendered on the Go side — pass resolved strings across the bridge.
   Standard placement: app menu (About+version, Settings…, Language ▸, Quit),
   File (New/Open/Open Recent ▸/Close), Edit (standard), View (2D⇄2.5D, zoom,
   panel toggles), Window, Help.
8. **Both themes via tokens.** Style through semantic tokens so light/dark/
   highContrast all resolve. Light is the launch-priority theme; never ship a
   component that breaks when the dark token set is substituted.

## Established direction (decided with the owner, 2026-07)

- Reference feel: Figma × Linear — light-first, canvas-centered, dense refined
  chrome. Left rail (Projects/Library nav), white surface lists on canvas
  background, violet only on primary CTA and active nav.
- Hub: native menu bar, page header with actions, error banner (reason+code),
  Recent as dense rows (name + mono path + relative time), template cards only
  when Library sources exist.
- Approved mock: see the `hub-mock` artifact history (v5). Reproduce its
  vocabulary for new screens rather than inventing a new one.

## Workflow

1. For a new screen: static mock first (real token values, real feature
   honesty — grey out or omit what is not wired), owner review, then implement.
2. Implementation lands behind the same review: screenshot the running app
   (`wails dev`) and compare against the approved mock before PR.
3. Every PR touching UI runs the checklist below; note deviations explicitly.

## UX review checklist

- [ ] Loading, empty, and failure states exist for every async surface; empty
      states say what to do next, failures show reason + code.
- [ ] Keyboard: focus visible (`action.focusRing`), tab order sane, Escape
      closes overlays, shortcuts match the menu declarations.
- [ ] `prefers-reduced-motion` respected; motion uses `motion.*` tokens.
- [ ] Hit targets ≥ 24px; hover states on every interactive element; cursor
      semantics correct.
- [ ] Text truncation with ellipsis + title attr for paths/names; tabular-nums
      for aligned digits; `ja` strings verified for overflow.
- [ ] Contrast meets WCAG AA on both themes (tokens are pre-validated — this
      check is for token misuse).
- [ ] No horizontal body scroll; wide content scrolls in its own container.
- [ ] Screenshot compared against approved mock or previous state.
