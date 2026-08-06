import type { Finding, Ingestion, ReportPDF, ReportRevision } from "./api";

export function operationalQueue(findings: Finding[], reports: ReportRevision[], pdfs: ReportPDF[]) {
  return {
    triage: findings.filter((item) => item.validationState === "new").length,
    retest: findings.filter((item) => item.remediationState === "open").length,
    approval: reports.filter((item) => item.state === "stored" && !item.approvedAt).length,
    delivery: reports.filter((item) => item.approvedAt && !pdfs.some((pdf) => pdf.revisionId === item.id)).length,
  };
}

export function formatBytes(bytes: number) {
  return bytes < 1024 ? `${bytes} B` : bytes < 1024 ** 2 ? `${(bytes / 1024).toFixed(1)} KiB` : `${(bytes / 1024 ** 2).toFixed(1)} MiB`;
}

export function currentIngestions(activeEngagementID: string, requestedEngagementID: string, items: Ingestion[]) {
  return activeEngagementID === requestedEngagementID ? items : undefined;
}
