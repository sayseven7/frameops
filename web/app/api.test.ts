import assert from "node:assert/strict";
import test from "node:test";

import { approveReportRevision, captureEvidence, collectionItems, createEngagement, createFinding, createMethodology, createProjectPlan, deriveReportPDF, publishMethodology, readEngagementChecklist, readIngestions, readProjectPlan, readReportRevisions, recordRetest, requestJSON, transitionProjectPlan, triageFinding, updateProjectPlan, uploadReportRevision } from "./api.ts";
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

test("evidence capture sends one multipart file with the session and CSRF", async () => {
  let request: Request | undefined;
  const fetcher: typeof fetch = async (input, init) => {
    request = new Request(new URL(input.toString(), "https://frameops.example.test"), init);
    return Response.json({ id: "evidence-id", state: "stored", filename: "capture.txt", sha256: "digest", byteSize: 7 }, { status: 201 });
  };

  await captureEvidence("finding-id", new File(["capture"], "capture.txt", { type: "text/plain" }), "csrf-token", fetcher);

  assert.equal(request?.url, "https://frameops.example.test/v1/findings/finding-id/evidence");
  assert.equal(request?.method, "POST");
  assert.equal(request?.credentials, "include");
  assert.equal(request?.headers.get("X-CSRF-Token"), "csrf-token");
  assert.match(request?.headers.get("Content-Type") ?? "", /^multipart\/form-data; boundary=/);
  assert.notEqual(request?.headers.get("Content-Type"), "application/json");
  assert.equal((await request?.formData()).get("file") instanceof File, true);
});

test("methodology and engagement requests use their API routes and CSRF", async () => {
  const requests: Request[] = [];
  const fetcher: typeof fetch = async (input, init) => {
    requests.push(new Request(new URL(input.toString(), "https://frameops.example.test"), init));
    return Response.json({ id: "result-id" }, { status: 201 });
  };
  const methodology = { name: "Web", sourceName: "OWASP WSTG", sourceVersion: "4.2", attribution: "Structured after OWASP WSTG.", items: [{ title: "Authorization", objective: "Verify access", procedure: "Replay request" }] };

  await createMethodology(methodology, "csrf-token", fetcher);
  await publishMethodology("template-id", "csrf-token", fetcher);
  await createEngagement("client-id", { name: "Q1", methodologyVersionId: "version-id" }, "csrf-token", fetcher);
  await readEngagementChecklist("engagement-id", fetcher);

  assert.deepEqual(requests.map((request) => [request.url, request.method, request.headers.get("X-CSRF-Token"), request.credentials]), [
    ["https://frameops.example.test/v1/methodology-templates", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/methodology-templates/template-id/publish", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/clients/client-id/engagements", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/engagements/engagement-id/checklist", "GET", null, "include"],
  ]);
});

test("apiErrorMessage localizes invalid finding state and CVSS errors", () => {
  assert.equal(apiErrorMessage("invalid_cvss_vector", "pt-BR"), "Informe um vetor CVSS 3.1 válido.");
  assert.equal(apiErrorMessage("invalid_cvss_vector", "en"), "Enter a valid CVSS 3.1 vector.");
  assert.equal(apiErrorMessage("invalid_state", "pt-BR"), "O estado atual não permite esta operação.");
  assert.equal(apiErrorMessage("invalid_state", "en"), "The current state does not allow this operation.");
  assert.equal(apiErrorMessage("evidence_too_large", "pt-BR"), "O arquivo de evidência excede o limite de 32 MiB.");
  assert.equal(apiErrorMessage("evidence_too_large", "en"), "The evidence file exceeds the 32 MiB limit.");
});

test("apiErrorMessage names deterministic Nmap import refusals", () => {
  assert.equal(apiErrorMessage("invalid_nmap_report", "pt-BR"), "O artefato não é um relatório XML Nmap aceito.");
  assert.equal(apiErrorMessage("artifact_too_large", "en"), "The import artifact exceeds the 8 MiB limit.");
  assert.equal(apiErrorMessage("duplicate_artifact", "en"), "This artifact has already been imported into this project.");
});

test("collectionItems normalizes null items to an empty collection", () => {
	assert.deepEqual(collectionItems<{ id: string }>({ items: null }), []);
});

test("project planning adapters use the scoped API and CSRF", async () => {
	const requests: Request[] = [];
	const fetcher: typeof fetch = async (input, init) => {
		const request = new Request(new URL(input.toString(), "https://frameops.example.test"), init);
		requests.push(request);
		return Response.json({ engagementId: "engagement-id", status: "draft" });
	};
	const plan = { startsOn: "2026-08-10", endsOn: "2026-08-20", rulesOfEngagement: "No production disruption.", targets: ["app.example.test"], exclusions: [], team: [], milestones: [] };

	await readProjectPlan("engagement-id", fetcher);
	await createProjectPlan("engagement-id", plan, "csrf-token", fetcher);
	await updateProjectPlan("engagement-id", plan, "csrf-token", fetcher);
	await transitionProjectPlan("engagement-id", "active", "csrf-token", fetcher);

	assert.deepEqual(requests.map((request) => [request.url, request.method, request.headers.get("X-CSRF-Token")]), [
		["https://frameops.example.test/v1/engagements/engagement-id/plan", "GET", null],
		["https://frameops.example.test/v1/engagements/engagement-id/plan", "POST", "csrf-token"],
		["https://frameops.example.test/v1/engagements/engagement-id/plan", "PUT", "csrf-token"],
		["https://frameops.example.test/v1/engagements/engagement-id/plan/transition", "POST", "csrf-token"],
	]);
});

test("readIngestions loads the selected engagement's persisted import history", async () => {
  let request: Request | undefined;
  await readIngestions("engagement/id", async (input, init) => {
    request = new Request(new URL(input.toString(), "https://frameops.example.test"), init);
    return Response.json({ items: [] });
  });

  assert.equal(request?.url, "https://frameops.example.test/v1/engagements/engagement%2Fid/ingestions");
  assert.equal(request?.method, "GET");
  assert.equal(request?.credentials, "include");
});

test("report revision adapters list, upload, approve, and derive with the session and CSRF", async () => {
  const requests: Request[] = [];
  const fetcher: typeof fetch = async (input, init) => {
    const request = new Request(new URL(input.toString(), "https://frameops.example.test"), init);
    requests.push(request);
    if (request.method === "GET") return Response.json({ items: [{ id: "revision-id", filename: "report.docx", state: "stored", sha256: "docx-digest", byteSize: 7, approvedAt: null }] });
    if (request.url.endsWith("/pdf")) return Response.json({ id: "pdf-id", revisionId: "revision-id", state: "stored", sourceSha256: "docx-digest", sha256: "pdf-digest", byteSize: 9 }, { status: 201 });
    return Response.json({ id: "revision-id", filename: "report.docx", state: "stored", sha256: "docx-digest", byteSize: 7, approvedAt: "2026-08-04T00:00:00Z" }, { status: request.url.endsWith("/approve") ? 200 : 201 });
  };

  await readReportRevisions("engagement-id", fetcher);
  await uploadReportRevision("engagement-id", new File(["report"], "report.docx", { type: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" }), "csrf-token", fetcher);
  await approveReportRevision("revision-id", "csrf-token", fetcher);
  await deriveReportPDF("revision-id", "csrf-token", fetcher);

  assert.deepEqual(requests.map((request) => [request.url, request.method, request.headers.get("X-CSRF-Token"), request.credentials]), [
    ["https://frameops.example.test/v1/engagements/engagement-id/reports", "GET", null, "include"],
    ["https://frameops.example.test/v1/engagements/engagement-id/reports", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/report-revisions/revision-id/approve", "POST", "csrf-token", "include"],
    ["https://frameops.example.test/v1/report-revisions/revision-id/pdf", "POST", "csrf-token", "include"],
  ]);
  assert.match(requests[1].headers.get("Content-Type") ?? "", /^multipart\/form-data; boundary=/);
  assert.notEqual(requests[1].headers.get("Content-Type"), "application/json");
  assert.equal((await requests[1].formData()).get("file") instanceof File, true);
});
