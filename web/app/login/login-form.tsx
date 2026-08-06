"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";

import { requestJSON } from "../api";
import { loginDestination } from "../auth";
import { apiErrorMessage, copy, type Locale } from "../copy";

export default function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const text = copy[locale];
  const destination = loginDestination(searchParams.get("next"));

  useEffect(() => {
    void requestJSON<{ token: string }>("/v1/csrf").then(() => router.replace(destination)).catch(() => undefined);
  }, [destination, router]);

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await requestJSON<void>("/v1/session/login", { method: "POST", body: JSON.stringify({ email, password }) });
      await requestJSON<{ token: string }>("/v1/csrf");
      router.replace(destination);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy(false);
    }
  }

  return <main className="login-shell"><section className="login-panel" aria-labelledby="sign-in-title">
    <header className="login-header"><div><strong>{text.title}</strong><span>{text.subtitle}</span></div>
    <label><span>{text.language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{text.portuguese}</option><option value="en">{text.english}</option></select></label></header>
    {error && <p className="error" role="alert">{error}</p>}
    <h1 id="sign-in-title">{text.signIn}</h1>
    <form onSubmit={signIn}><label htmlFor="email">{text.email}</label><input id="email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required /><label htmlFor="password">{text.password}</label><input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /><button type="submit" disabled={busy}>{busy ? text.signingIn : text.signInAction}</button></form>
  </section></main>;
}
