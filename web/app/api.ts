export type Client = {
  id: string;
  name: string;
};

export type Engagement = Client & {
  clientId: string;
};

export type MethodologyItem = {
  position?: number;
  title: string;
  objective: string;
  preconditions?: string;
  procedure: string;
  expectedEvidence?: string;
  reference?: string;
  notes?: string;
};

export type Methodology = {
  id: string;
  templateId: string;
  versionNumber: number;
  state: "draft" | "published";
  name: string;
  sourceName: string;
  sourceVersion: string;
  attribution: string;
  items: MethodologyItem[];
};

export type MethodologyInput = Pick<Methodology, "name" | "sourceName" | "sourceVersion" | "attribution" | "items">;

export type EngagementInput = Pick<Engagement, "name"> & { methodologyVersionId: string };

export type EngagementChecklist = Pick<Methodology, "name" | "sourceName" | "sourceVersion" | "attribution" | "items"> & {
  id: string;
  engagementId: string;
  templateVersionId: string;
  versionNumber: number;
};

export type ProjectPlan = {
  engagementId: string;
  ownerUserId: string;
  status: "draft" | "active" | "closed";
  startsOn: string;
  endsOn: string;
  rulesOfEngagement: string;
  scope: { versionNumber: number; targets: string[]; exclusions: string[]; createdAt: string };
  team: { userId: string; role: "lead" | "tester" | "reviewer" }[];
  milestones: { id?: string; title: string; dueOn: string; completedAt?: string | null }[];
};

export type ProjectPlanInput = Pick<ProjectPlan, "startsOn" | "endsOn" | "rulesOfEngagement" | "team" | "milestones"> & {
  targets: string[];
  exclusions: string[];
};

export type Finding = {
  id: string;
  engagementId: string;
  title: string;
  description: string;
  impact: string;
  remediation: string;
  reproduction: string;
  cvssVector: string;
  cvssScore: number;
  validationState: string;
  remediationState: string | null;
};

export type FindingInput = Pick<Finding, "title" | "description" | "impact" | "remediation" | "reproduction" | "cvssVector">;

export type Retest = {
  id: string;
  findingId: string;
  round: number;
  previousState: string;
  resultState: "open" | "fixed" | "not_reproduced";
  procedure: string;
  observedResult: string;
  justification: string;
};

export type RetestInput = Pick<Retest, "round" | "resultState" | "procedure" | "observedResult" | "justification">;

export type Evidence = {
  id: string;
  state: string;
  filename: string;
  sha256: string;
  byteSize: number;
};

export type Ingestion = {
  id: string;
  engagementId: string;
  tool: string;
  formatVersion: string;
  filename: string;
  sha256: string;
  byteSize: number;
  receivedAt: string;
  importedBy: string;
  summary: {
    read: number;
    created: number;
    reused: number;
    ignored: number;
    rejected: number;
  };
};

export type ReportRevision = {
  id: string;
  filename: string;
  state: string;
  sha256: string;
  byteSize: number;
  approvedAt: string | null;
};

export type ReportPDF = {
  id: string;
  revisionId: string;
  state: string;
  sourceSha256: string;
  sha256: string;
  byteSize: number;
};

export type Organization = { id: string; name: string };
export type OrganizationMember = { id: string; displayName: string; email: string; role: "admin" | "member"; isActive: boolean };
export type OrganizationMemberInput = Pick<OrganizationMember, "displayName" | "email" | "role"> & { password: string };
export type AuditEvent = { id: string; action: string; targetType: string; targetId: string; outcome: string; createdAt: string; context: Record<string, never> };

type Fetcher = typeof fetch;

export function collectionItems<T>(collection: { items: T[] | null }): T[] {
  return collection.items ?? [];
}

export async function requestJSON<T>(
  path: string,
  init: RequestInit = {},
  csrf?: string,
  fetcher: Fetcher = fetch,
): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData)) headers.set("Content-Type", "application/json");
  if (csrf) headers.set("X-CSRF-Token", csrf);

  const response = await fetcher(path, { ...init, credentials: "include", headers });
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string };
    throw new Error(body.error ?? response.statusText);
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>;
}

export function createFinding(engagementID: string, input: FindingInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Finding>(`/v1/engagements/${encodeURIComponent(engagementID)}/findings`, { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function triageFinding(findingID: string, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Finding>(`/v1/findings/${encodeURIComponent(findingID)}/triage`, { method: "PUT", body: JSON.stringify({ validationState: "confirmed", remediationState: "open" }) }, csrf, fetcher);
}

export function recordRetest(findingID: string, input: RetestInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Retest>(`/v1/findings/${encodeURIComponent(findingID)}/retests`, { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function captureEvidence(findingID: string, file: File, csrf: string, fetcher?: Fetcher) {
  const form = new FormData();
  form.append("file", file);
  return requestJSON<Evidence>(`/v1/findings/${encodeURIComponent(findingID)}/evidence`, { method: "POST", body: form }, csrf, fetcher);
}

export function readEvidence(findingID: string, fetcher?: Fetcher) {
  return requestJSON<{ items: Evidence[] | null }>(`/v1/findings/${encodeURIComponent(findingID)}/evidence`, {}, undefined, fetcher);
}

export function createMethodology(input: MethodologyInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Methodology>("/v1/methodology-templates", { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function publishMethodology(templateID: string, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Methodology>(`/v1/methodology-templates/${encodeURIComponent(templateID)}/publish`, { method: "POST" }, csrf, fetcher);
}

export function createEngagement(clientID: string, input: EngagementInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Engagement>(`/v1/clients/${encodeURIComponent(clientID)}/engagements`, { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function readEngagementChecklist(engagementID: string, fetcher?: Fetcher) {
  return requestJSON<EngagementChecklist>(`/v1/engagements/${encodeURIComponent(engagementID)}/checklist`, {}, undefined, fetcher);
}

export function readProjectPlan(engagementID: string, fetcher?: Fetcher) {
  return requestJSON<ProjectPlan>(`/v1/engagements/${encodeURIComponent(engagementID)}/plan`, {}, undefined, fetcher);
}

export function createProjectPlan(engagementID: string, input: ProjectPlanInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<ProjectPlan>(`/v1/engagements/${encodeURIComponent(engagementID)}/plan`, { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function updateProjectPlan(engagementID: string, input: ProjectPlanInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<ProjectPlan>(`/v1/engagements/${encodeURIComponent(engagementID)}/plan`, { method: "PUT", body: JSON.stringify(input) }, csrf, fetcher);
}

export function transitionProjectPlan(engagementID: string, status: "active" | "closed", csrf: string, fetcher?: Fetcher) {
  return requestJSON<ProjectPlan>(`/v1/engagements/${encodeURIComponent(engagementID)}/plan/transition`, { method: "POST", body: JSON.stringify({ status }) }, csrf, fetcher);
}

export function readReportRevisions(engagementID: string, fetcher?: Fetcher) {
  return requestJSON<{ items: ReportRevision[] | null }>(`/v1/engagements/${encodeURIComponent(engagementID)}/reports`, {}, undefined, fetcher);
}

export function readIngestions(engagementID: string, fetcher?: Fetcher) {
  return requestJSON<{ items: Ingestion[] | null }>(`/v1/engagements/${encodeURIComponent(engagementID)}/ingestions`, {}, undefined, fetcher);
}

export function uploadReportRevision(engagementID: string, file: File, csrf: string, fetcher?: Fetcher) {
  const form = new FormData();
  form.append("file", file);
  return requestJSON<ReportRevision>(`/v1/engagements/${encodeURIComponent(engagementID)}/reports`, { method: "POST", body: form }, csrf, fetcher);
}

export function approveReportRevision(revisionID: string, csrf: string, fetcher?: Fetcher) {
  return requestJSON<ReportRevision>(`/v1/report-revisions/${encodeURIComponent(revisionID)}/approve`, { method: "POST" }, csrf, fetcher);
}

export function deriveReportPDF(revisionID: string, csrf: string, fetcher?: Fetcher) {
  return requestJSON<ReportPDF>(`/v1/report-revisions/${encodeURIComponent(revisionID)}/pdf`, { method: "POST" }, csrf, fetcher);
}


export function readOrganization(fetcher?: Fetcher) {
  return requestJSON<Organization>("/v1/organization", {}, undefined, fetcher);
}

export function updateOrganization(name: string, csrf: string, fetcher?: Fetcher) {
  return requestJSON<Organization>("/v1/organization", { method: "PUT", body: JSON.stringify({ name }) }, csrf, fetcher);
}

export function listOrganizationMembers(fetcher?: Fetcher) {
  return requestJSON<{ items: OrganizationMember[] | null }>("/v1/organization/members", {}, undefined, fetcher);
}

export function createOrganizationMember(input: OrganizationMemberInput, csrf: string, fetcher?: Fetcher) {
  return requestJSON<OrganizationMember>("/v1/organization/members", { method: "POST", body: JSON.stringify(input) }, csrf, fetcher);
}

export function updateOrganizationMember(memberID: string, input: Partial<Pick<OrganizationMember, "role" | "isActive">>, csrf: string, fetcher?: Fetcher) {
  return requestJSON<OrganizationMember>(`/v1/organization/members/${encodeURIComponent(memberID)}`, { method: "PATCH", body: JSON.stringify(input) }, csrf, fetcher);
}

export function listOrganizationAuditEvents({ action, cursor, limit }: { action?: string; cursor?: string; limit?: number } = {}, fetcher?: Fetcher) {
  const params = new URLSearchParams();
  if (action) params.set("action", action);
  if (cursor) params.set("cursor", cursor);
  if (limit) params.set("limit", String(limit));
  return requestJSON<{ items: AuditEvent[] | null; nextCursor: string }>(`/v1/organization/audit-events${params.size ? `?${params}` : ""}`, {}, undefined, fetcher);
}
