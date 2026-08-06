export const appRoutes = ["/login", "/dashboard", "/organizations", "/clients", "/projects", "/settings"] as const;

export type ProjectSection = "overview" | "scope" | "methodology" | "findings" | "evidence" | "imports" | "reports";

export function projectRoute(projectID: string, section: ProjectSection) {
  return `/projects/${encodeURIComponent(projectID)}/${section}`;
}

export function workspaceRoute(projectID: string, section: ProjectSection) {
  return projectID ? projectRoute(projectID, section) : "/projects";
}
