export type Client = {
  id: string;
  name: string;
};

export type Engagement = Client & {
  clientId: string;
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

type Fetcher = typeof fetch;

export async function requestJSON<T>(
  path: string,
  init: RequestInit = {},
  csrf?: string,
  fetcher: Fetcher = fetch,
): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set("Content-Type", "application/json");
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
