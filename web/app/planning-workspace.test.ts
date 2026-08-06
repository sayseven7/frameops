import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { copy, type Locale } from "./copy.ts";

for (const [locale, expected] of [["pt-BR", { title: "Planejamento do projeto", lead: "Até que o diretório de membros esteja disponível, o criador do projeto é definido como lead." }], ["en", { title: "Project planning", lead: "Until the member directory is available, the project creator is assigned as lead." }]] as const satisfies readonly [Locale, { title: string; lead: string }][]) {
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
