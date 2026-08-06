"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { destinationForSession } from "./auth";
import { requestJSON } from "./api";

export default function RootRedirect() {
  const router = useRouter();

  useEffect(() => {
    void requestJSON<{ token: string }>("/v1/csrf")
      .then(() => router.replace(destinationForSession("/", true)))
      .catch(() => router.replace(destinationForSession("/", false)));
  }, [router]);

  return <main className="login-shell" aria-busy="true" />;
}
