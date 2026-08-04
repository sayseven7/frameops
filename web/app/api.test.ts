import assert from "node:assert/strict";
import test from "node:test";

import { createFinding, recordRetest, requestJSON, triageFinding } from "./api.ts";
import { apiErrorMessage } from "./copy.ts";

test("requestJSON keeps the session and sends CSRF for a mutating request", async () => {
  let request: Request | undefined;
  const result = await requestJSON<{ id: string }>(
    "/v1/clients",
    { method: "POST", body: JSON.stringify({ name: "Client" }) },
    "csrf-token",
    async (_input, init) => {
      request = new Request("https://frameops.example.test/v1/clients", init);
      return Response.json({ id: "client-id" }, { status: 201 });
    },
  );

  assert.deepEqual(result, { id: "client-id" });
  assert.equal(request?.credentials, "include");
  assert.equal(request?.headers.get("X-CSRF-Token"), "csrf-token");
  assert.equal(request?.headers.get("Content-Type"), "application/json");
});

test("apiErrorMessage localizes known API codes and hides unknown details", () => {
  assert.equal(apiErrorMessage("unauthorized", "pt-BR"), "Sua sessão expirou. Entre novamente.");
  assert.equal(apiErrorMessage("unauthorized", "en"), "Your session expired. Sign in again.");
  assert.equal(apiErrorMessage("database timeout: host=internal", "pt-BR"), "Não foi possível concluir a operação. Tente novamente.");
  assert.equal(apiErrorMessage("database timeout: host=internal", "en"), "We could not complete the operation. Try again.");
});

test("finding mutations keep the session and send CSRF to their existing API routes", async () => {
  const requests: Request[] = [];
  const fetcher: typeof fetch = async (input, init) => {
    requests.push(new Request(new URL(input.toString(), "https://frameops.example.test"), init));
    return Response.json({ id: "finding-id" }, { status: 201 });
  };

  await createFinding("engagement-id", { title: "SQL injection", cvssVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" }, "csrf-token", fetcher);
  await triageFinding("finding-id", "csrf-token", fetcher);
  await recordRetest("finding-id", { round: 1, resultState: "fixed", procedure: "Replayed request", observedResult: "Payload rejected", justification: "Verified patched build" }, "csrf-token", fetcher);

  assert.deepEqual(requests.map((request) => [request.url, request.method, request.headers.get("X-CSRF-Token"), request.credentials]), [
    ["https://frameops.example.test/v1/engagements/engagement-id/findings", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/findings/finding-id/triage", "PUT", "csrf-token", "include"],
    ["https://frameops.example.test/v1/findings/finding-id/retests", "POST", "csrf-token", "include"],
  ]);
});

test("apiErrorMessage localizes invalid finding state and CVSS errors", () => {
  assert.equal(apiErrorMessage("invalid_cvss_vector", "pt-BR"), "Informe um vetor CVSS 3.1 válido.");
  assert.equal(apiErrorMessage("invalid_cvss_vector", "en"), "Enter a valid CVSS 3.1 vector.");
  assert.equal(apiErrorMessage("invalid_state", "pt-BR"), "O estado atual não permite esta operação.");
  assert.equal(apiErrorMessage("invalid_state", "en"), "The current state does not allow this operation.");
});
