"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { requestJSON } from "./api";
import { destinationForSession } from "./auth";
import OrganizationAdmin from "./organization-admin";

export default function ProtectedOrganizationAdmin() {
  const pathname = usePathname();
  const router = useRouter();
  const [csrf, setCSRF] = useState("");
  useEffect(() => {
    let active = true;
    void requestJSON<{ token: string }>("/v1/csrf").then(({ token }) => { if (active) setCSRF(token); }).catch(() => router.replace(destinationForSession(pathname, false)));
    return () => { active = false; };
  }, [pathname, router]);
  return csrf ? <OrganizationAdmin csrf={csrf} /> : <main className="login-shell" aria-busy="true" />;
}
