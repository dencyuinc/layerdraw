// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import TestRenderer, { act } from "react-test-renderer";
import {
  BASE_LOCALE,
  I18nProvider,
  SUPPORTED_LOCALES,
  baseShellCatalogs,
  createTranslator,
  findCatalogGaps,
  interpolate,
  isLocale,
  mergeCatalogs,
  resolveLocale,
  useI18n,
} from "../dist/i18n.js";

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

test("resolveLocale maps OS tags and unknowns to a supported locale", () => {
  assert.equal(resolveLocale("ja"), "ja");
  assert.equal(resolveLocale("ja-JP"), "ja");
  assert.equal(resolveLocale("en-US"), "en");
  assert.equal(resolveLocale("fr"), BASE_LOCALE);
  assert.equal(resolveLocale(undefined), BASE_LOCALE);
  assert.equal(resolveLocale(""), BASE_LOCALE);
  assert.equal(isLocale("ja"), true);
  assert.equal(isLocale("de"), false);
});

test("interpolate substitutes named arguments and leaves unknown placeholders", () => {
  assert.equal(interpolate("Opened {when}", { when: "2 days ago" }), "Opened 2 days ago");
  assert.equal(interpolate("Count {n}", { n: 3 }), "Count 3");
  assert.equal(interpolate("Keep {missing}", { other: "x" }), "Keep {missing}");
  assert.equal(interpolate("no args"), "no args");
});

test("translator falls back to base locale then key, and formats via Intl", () => {
  const catalogs = mergeCatalogs(baseShellCatalogs, { ja: { "hub.title": "プロジェクト" } });
  const ja = createTranslator("ja", catalogs);
  assert.equal(ja.t("hub.action.new"), "新規プロジェクト");
  assert.equal(ja.t("nonexistent.key"), "nonexistent.key");
  // Base-locale fallback: a key only present in en resolves for ja too.
  const partial = { en: { only: "English" }, ja: {} };
  assert.equal(createTranslator("ja", partial).t("only"), "English");
  assert.equal(ja.formatNumber(1234.5).length > 0, true);
  assert.match(ja.formatDate("2026-07-22T00:00:00Z", { year: "numeric" }), /2026/);
});

test("error() presents a translated reason plus the stable code, never the raw code alone", () => {
  const t = createTranslator("en", baseShellCatalogs);
  const message = t.error("desktop.project_missing");
  assert.match(message, /could not be found/);
  assert.match(message, /\(desktop\.project_missing\)$/);
  // Unknown codes still get a generic reason plus the code.
  const unknown = t.error("desktop.unheard_of");
  assert.match(unknown, /could not be opened/);
  assert.match(unknown, /\(desktop\.unheard_of\)$/);
});

test("formatRelativeTime chooses a sensible unit in the active locale", () => {
  const t = createTranslator("en", baseShellCatalogs);
  const now = new Date("2026-07-22T12:00:00Z");
  assert.match(t.formatRelativeTime(new Date("2026-07-22T11:58:00Z"), now), /2 minutes ago/);
  assert.match(t.formatRelativeTime(new Date("2026-07-20T12:00:00Z"), now), /2 days ago/);
  const ja = createTranslator("ja", baseShellCatalogs);
  assert.equal(ja.formatRelativeTime(new Date("2026-07-22T11:58:00Z"), now).length > 0, true);
});

test("ja catalog is complete for every base key", () => {
  const gaps = findCatalogGaps(baseShellCatalogs);
  for (const locale of SUPPORTED_LOCALES) assert.deepEqual(gaps[locale], []);
});

test("I18nProvider supplies a translator and switching locale re-renders without component changes", async () => {
  function Label() {
    const t = useI18n();
    return React.createElement("span", null, t.t("hub.action.new"));
  }
  let renderer;
  await act(async () => {
    renderer = TestRenderer.create(
      React.createElement(I18nProvider, { locale: "en", catalogs: baseShellCatalogs }, React.createElement(Label)),
    );
  });
  assert.equal(renderer.root.findByType("span").children.join(""), "New project");
  await act(async () => {
    renderer.update(
      React.createElement(I18nProvider, { locale: "ja", catalogs: baseShellCatalogs }, React.createElement(Label)),
    );
  });
  assert.equal(renderer.root.findByType("span").children.join(""), "新規プロジェクト");
  await act(async () => renderer.unmount());
});

test("useI18n throws outside a provider", async () => {
  function Orphan() {
    useI18n();
    return null;
  }
  let error;
  try {
    await act(async () => {
      TestRenderer.create(React.createElement(Orphan));
    });
  } catch (caught) {
    error = caught;
  }
  assert.match(String(error), /I18nProvider/);
});
