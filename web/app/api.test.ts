import assert from "node:assert/strict";
import test from "node:test";

import { requestJSON } from "./api.ts";
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
