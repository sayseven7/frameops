"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { type Client, type Engagement, requestJSON } from "./api";
import { apiErrorMessage, copy, type Locale } from "./copy";

type Collection<T> = { items: T[] };

export default function HomePage() {
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [csrf, setCSRF] = useState("");
  const [clients, setClients] = useState<Client[]>([]);
  const [engagements, setEngagements] = useState<Engagement[]>([]);
  const [clientID, setClientID] = useState("");
  const [clientName, setClientName] = useState("");
  const [engagementName, setEngagementName] = useState("");
  const [busy, setBusy] = useState<"login" | "client" | "engagement" | "">("");
  const [error, setError] = useState("");
  const errorRef = useRef<HTMLParagraphElement>(null);
  const text = copy[locale];

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  async function loadEngagements(id: string) {
    setEngagements((await requestJSON<Collection<Engagement>>(`/v1/clients/${encodeURIComponent(id)}/engagements`)).items);
  }

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("login");
    setError("");
    try {
      await requestJSON<void>("/v1/session/login", { method: "POST", body: JSON.stringify({ email, password }) });
      const token = await requestJSON<{ token: string }>("/v1/csrf");
      setCSRF(token.token);
      setClients((await requestJSON<Collection<Client>>("/v1/clients")).items);
      setPassword("");
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function selectClient(id: string) {
    setClientID(id);
    setError("");
    if (!id) {
      setEngagements([]);
      return;
    }
    try {
      await loadEngagements(id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    }
  }

  async function createClient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("client");
    setError("");
    try {
      const client = await requestJSON<Client>("/v1/clients", { method: "POST", body: JSON.stringify({ name: clientName }) }, csrf);
      setClients((current) => [...current, client]);
      setClientName("");
      await selectClient(client.id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function createEngagement(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!clientID) {
      setError(text.noClientSelected);
      return;
    }
    setBusy("engagement");
    setError("");
    try {
      const engagement = await requestJSON<Engagement>(`/v1/clients/${encodeURIComponent(clientID)}/engagements`, { method: "POST", body: JSON.stringify({ name: engagementName }) }, csrf);
      setEngagements((current) => [...current, engagement]);
      setEngagementName("");
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  const signedIn = Boolean(csrf);

  return (
    <main className="portfolio-shell">
      <header className="portfolio-header">
        <div><strong>{text.title}</strong><span>{text.subtitle}</span></div>
        <label>
          <span>{text.language}</span>
          <select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}>
            <option value="pt-BR">{text.portuguese}</option>
            <option value="en">{text.english}</option>
          </select>
        </label>
      </header>

      {error && <p className="error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}

      {!signedIn ? (
        <section className="panel" aria-labelledby="sign-in-title">
          <h1 id="sign-in-title">{text.signIn}</h1>
          <form onSubmit={signIn}>
            <label htmlFor="email">{text.email}</label>
            <input id="email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
            <label htmlFor="password">{text.password}</label>
            <input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
            <button type="submit" disabled={busy === "login"}>{busy === "login" ? text.signingIn : text.signInAction}</button>
          </form>
        </section>
      ) : (
        <section className="portfolio" aria-labelledby="portfolio-title">
          <h1 id="portfolio-title">{text.portfolio}</h1>
          <p aria-live="polite">{busy ? text.loading : text.sessionReady}</p>
          <div className="portfolio-grid">
            <section className="panel" aria-labelledby="clients-title">
              <h2 id="clients-title">{text.clients}</h2>
              <form onSubmit={createClient}>
                <label htmlFor="client-name">{text.clientName}</label>
                <input id="client-name" value={clientName} onChange={(event) => setClientName(event.target.value)} required />
                <button type="submit" disabled={busy === "client"}>{busy === "client" ? text.creatingClient : text.createClient}</button>
              </form>
              <label htmlFor="client-select">{text.selectClient}</label>
              <select id="client-select" value={clientID} onChange={(event) => void selectClient(event.target.value)}>
                <option value="">{text.selectClient}</option>
                {clients.map((client) => <option key={client.id} value={client.id}>{client.name}</option>)}
              </select>
              {!clients.length && <p>{text.noClients}</p>}
            </section>
            <section className="panel" aria-labelledby="engagements-title">
              <h2 id="engagements-title">{text.engagements}</h2>
              <form onSubmit={createEngagement}>
                <label htmlFor="engagement-name">{text.engagementName}</label>
                <input id="engagement-name" value={engagementName} onChange={(event) => setEngagementName(event.target.value)} required disabled={!clientID} />
                <button type="submit" disabled={!clientID || busy === "engagement"}>{busy === "engagement" ? text.creatingEngagement : text.createEngagement}</button>
              </form>
              <ul aria-live="polite">
                {engagements.map((engagement) => <li key={engagement.id}>{engagement.name}</li>)}
              </ul>
              {clientID && !engagements.length && <p>{text.noEngagements}</p>}
            </section>
          </div>
        </section>
      )}
    </main>
  );
}
