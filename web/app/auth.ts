export function loginDestination(next: string | null | undefined): string {
  return next?.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : "/dashboard";
}

export function destinationForSession(pathname: string, authenticated: boolean): string {
  if (authenticated && (pathname === "/" || pathname === "/login")) return "/dashboard";
  if (!authenticated && pathname === "/") return "/login";
  return authenticated ? pathname : `/login?next=${encodeURIComponent(pathname)}`;
}
