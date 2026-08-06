import assert from "node:assert/strict";
import test from "node:test";

import { appRoutes, projectRoute, workspaceRoute } from "./routes.ts";

test("appRoutes provides each supported top-level route exactly once", () => {
  assert.deepEqual(appRoutes, ["/login", "/dashboard", "/organizations", "/clients", "/projects", "/settings"]);
});

test("projectRoute builds an encoded deep link for every project workspace section", () => {
  assert.equal(projectRoute("engagement/a", "overview"), "/projects/engagement%2Fa/overview");
  assert.equal(projectRoute("engagement", "scope"), "/projects/engagement/scope");
  assert.equal(projectRoute("engagement", "methodology"), "/projects/engagement/methodology");
  assert.equal(projectRoute("engagement", "findings"), "/projects/engagement/findings");
  assert.equal(projectRoute("engagement", "evidence"), "/projects/engagement/evidence");
  assert.equal(projectRoute("engagement", "reports"), "/projects/engagement/reports");
});

test("workspaceRoute falls back to the projects index until an engagement is selected", () => {
  assert.equal(workspaceRoute("", "findings"), "/projects");
  assert.equal(workspaceRoute("engagement", "findings"), "/projects/engagement/findings");
});
