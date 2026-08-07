import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { copy, stateLabel, type Locale } from "./copy.ts";

for (const [locale, expected] of [["pt-BR", { title: "Planejamento do projeto", lead: "Até que o diretório de membros esteja disponível, o criador do projeto é o lead provisório." }], ["en", { title: "Project planning", lead: "Until the member directory is available, the project creator is the provisional lead." }]] as const satisfies readonly [Locale, { title: string; lead: string }][]) {
  test(`uses the shared ${locale} locale for planning and the lead fallback`, () => {
    const text = copy[locale];
    assert.equal(text.projectPlanningTitle, expected.title);
    assert.equal(text.projectLeadFallback, expected.lead);
  });
}

test("changing locale does not reload and replace the planning form", () => {
  const workspace = readFileSync(new URL("./planning-workspace.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(workspace, /\}, \[engagementID, locale, pathname, router\]\);/);
});

test("keeps the document language synchronized with the selected locale in every client locale consumer", () => {
  for (const source of ["./login/login-form.tsx", "./page.tsx", "./organization-admin.tsx", "./planning-workspace.tsx"]) {
    const component = readFileSync(new URL(source, import.meta.url), "utf8");
    assert.match(component, /document\.documentElement\.lang = locale/);
  }
});

test("maps API states to localized labels without exposing raw enums", () => {
  const states = ["new", "pending", "needs_review", "confirmed", "false_positive", "open", "risk_accepted", "draft", "stored", "published", "approved", "fixed", "not_reproduced", "active", "closed"];
  for (const state of states) assert.notEqual(stateLabel(state, "pt-BR"), copy["pt-BR"].unknownState);
  for (const state of states) assert.notEqual(stateLabel(state, "en"), copy.en.unknownState);
  assert.equal(stateLabel("unexpected_api_value", "en"), "Unknown state");
});

test("announces protected loading and exposes operational table relationships", () => {
  const loading = readFileSync(new URL("./protected-workspace.tsx", import.meta.url), "utf8");
  const workspace = readFileSync(new URL("./page.tsx", import.meta.url), "utf8");
  assert.match(loading, /role="status"/);
  assert.match(workspace, /focus\(\{ preventScroll: true \}\)/);
  for (const role of ["table", "row", "columnheader", "cell"]) assert.match(workspace, new RegExp(`role="${role}"`));
});

test("renders methodology summaries that omit checklist items without crashing the workspace", () => {
  const workspace = readFileSync(new URL("./page.tsx", import.meta.url), "utf8");
  assert.match(workspace, /methodology\.items\?\.length \?\? "—"/);
});

test("supports explicit and system themes without a global motion kill switch", () => {
  const styles = readFileSync(new URL("./globals.css", import.meta.url), "utf8");
  assert.match(styles, /\[data-theme="light"\]/);
  assert.match(styles, /\[data-theme="system"\]/);
  assert.match(styles, /\.app-shell, \.login-shell \{ background:var\(--ink\); color:var\(--text\); \}/);
  assert.match(styles, /\.app-content \{[^}]*border-top:2px solid transparent;/);
  assert.match(styles, /\.app-content:focus-visible \{ outline:0; border-top-color:var\(--focus\); \}/);
  assert.doesNotMatch(styles, /\.app-content:focus-visible \{[^}]*box-shadow:/);
  assert.doesNotMatch(styles, /transition-duration:\.01ms|animation-duration:\.01ms/);
});
