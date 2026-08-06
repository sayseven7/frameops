import assert from "node:assert/strict";
import test from "node:test";

import type { Finding, Ingestion, ReportPDF, ReportRevision } from "./api.ts";
import { currentIngestions, formatBytes, operationalQueue } from "./operations.ts";

const finding = (validationState: string, remediationState: string | null): Finding => ({ id: crypto.randomUUID(), engagementId: "e", title: "t", description: "", impact: "", remediation: "", reproduction: "", cvssVector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", cvssScore: 0, validationState, remediationState });
const report = (id: string, approvedAt: string | null): ReportRevision => ({ id, filename: `${id}.docx`, state: "stored", sha256: "x", byteSize: 1, approvedAt });

test("derives only actionable work from loaded operational state", () => {
  const reports = [report("approval", null), report("delivery", "2026-01-01"), report("done", "2026-01-01")];
  const pdfs: ReportPDF[] = [{ id: "pdf", revisionId: "done", state: "stored", sourceSha256: "x", sha256: "y", byteSize: 2 }];
  assert.deepEqual(operationalQueue([finding("new", null), finding("confirmed", "open"), finding("confirmed", "fixed")], reports, pdfs), { triage: 1, retest: 1, approval: 1, delivery: 1 });
  assert.equal(formatBytes(1536), "1.5 KiB");
});

test("keeps imports only when their engagement remains selected", () => {
  const items = [{ id: "ingestion" }] as Ingestion[];
  assert.deepEqual(currentIngestions("engagement-a", "engagement-a", items), items);
  assert.equal(currentIngestions("engagement-b", "engagement-a", items), undefined);
});
