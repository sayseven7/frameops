export type Client = {
  id: string;
  name: string;
};

export type Engagement = Client & {
  clientId: string;
};

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
