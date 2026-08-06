import assert from "node:assert/strict";
import test from "node:test";

import { destinationForSession, loginDestination } from "./auth.ts";

test("unauthenticated private routes retain only an encoded local destination", () => {
  assert.equal(destinationForSession("/dashboard", false), "/login?next=%2Fdashboard");
  assert.equal(destinationForSession("/projects/engagement%2Fa/findings", false), "/login?next=%2Fprojects%2Fengagement%252Fa%2Ffindings");
});

test("session destinations reject external and protocol-relative next values", () => {
  assert.equal(loginDestination("/projects/engagement/overview"), "/projects/engagement/overview");
  assert.equal(loginDestination("https://attacker.example"), "/dashboard");
  assert.equal(loginDestination("//attacker.example"), "/dashboard");
  assert.equal(loginDestination("/\\attacker.example"), "/dashboard");
});

test("root and login resolve to the dashboard for an authenticated session", () => {
  assert.equal(destinationForSession("/", true), "/dashboard");
  assert.equal(destinationForSession("/login", true), "/dashboard");
});
