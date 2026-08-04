"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { createFinding, recordRetest, triageFinding, type Client, type Engagement, type Finding, type FindingInput, type Retest, type RetestInput, requestJSON } from "./api";
import { apiErrorMessage, copy, type Locale } from "./copy";

type Collection<T> = { items: T[] };

export default function HomePage() {
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [csrf, setCSRF] = useState("");
  const [clients, setClients] = useState<Client[]>([]);
  const [engagements, setEngagements] = useState<Engagement[]>([]);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [retests, setRetests] = useState<Retest[]>([]);
  const [clientID, setClientID] = useState("");
  const [engagementID, setEngagementID] = useState("");
  const [findingID, setFindingID] = useState("");
  const [clientName, setClientName] = useState("");
  const [engagementName, setEngagementName] = useState("");
  const [findingInput, setFindingInput] = useState<FindingInput>({ title: "", description: "", impact: "", remediation: "", reproduction: "", cvssVector: "" });
  const [retestInput, setRetestInput] = useState<Omit<RetestInput, "round">>({ resultState: "open", procedure: "", observedResult: "", justification: "" });
  const [busy, setBusy] = useState<"login" | "client" | "engagement" | "finding" | "triage" | "retest" | "">("");
  const [error, setError] = useState("");
  const errorRef = useRef<HTMLParagraphElement>(null);
  const text = copy[locale];

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  async function loadEngagements(id: string) {
    setEngagements((await requestJSON<Collection<Engagement>>(`/v1/clients/${encodeURIComponent(id)}/engagements`)).items);
  }

  async function loadFindings(id: string) {
    setFindings((await requestJSON<Collection<Finding>>(`/v1/engagements/${encodeURIComponent(id)}/findings`)).items);
  }

  async function loadRetests(id: string) {
    setRetests((await requestJSON<Collection<Retest>>(`/v1/findings/${encodeURIComponent(id)}/retests`)).items);
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
      setEngagementID("");
      setFindings([]);
      setFindingID("");
      setRetests([]);
      return;
    }
    try {
      await loadEngagements(id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    }
  }

  async function selectEngagement(id: string) {
    setEngagementID(id);
    setFindingID("");
    setRetests([]);
    setError("");
    if (!id) {
      setFindings([]);
      return;
    }
    try {
      await loadFindings(id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    }
  }

  async function selectFinding(id: string) {
    setFindingID(id);
    setError("");
    if (!id) {
      setRetests([]);
      return;
    }
    try {
      await loadRetests(id);
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
      await selectEngagement(engagement.id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function submitFinding(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!engagementID) return;
    setBusy("finding");
    setError("");
    try {
      const finding = await createFinding(engagementID, findingInput, csrf);
      setFindings((current) => [...current, finding]);
      setFindingInput({ title: "", description: "", impact: "", remediation: "", reproduction: "", cvssVector: "" });
      await selectFinding(finding.id);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function confirmFinding() {
    if (!findingID) return;
    setBusy("triage");
    setError("");
    try {
      const finding = await triageFinding(findingID, csrf);
      setFindings((current) => current.map((item) => item.id === finding.id ? finding : item));
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function submitRetest(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!findingID) return;
    setBusy("retest");
    setError("");
    try {
      const retest = await recordRetest(findingID, { ...retestInput, round: retests.length + 1 }, csrf);
      setRetests((current) => [...current, retest]);
      setFindings((current) => current.map((item) => item.id === findingID ? { ...item, remediationState: retest.resultState } : item));
      setRetestInput({ resultState: "open", procedure: "", observedResult: "", justification: "" });
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  const signedIn = Boolean(csrf);
  const selectedFinding = findings.find((finding) => finding.id === findingID);

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
              <label htmlFor="engagement-select">{text.selectEngagement}</label>
              <select id="engagement-select" value={engagementID} onChange={(event) => void selectEngagement(event.target.value)} disabled={!clientID}>
                <option value="">{text.selectEngagement}</option>
                {engagements.map((engagement) => <option key={engagement.id} value={engagement.id}>{engagement.name}</option>)}
              </select>
              {clientID && !engagements.length && <p>{text.noEngagements}</p>}
            </section>
            <section className="panel" aria-labelledby="findings-title">
              <h2 id="findings-title">{text.findings}</h2>
              <form onSubmit={submitFinding}>
                <label htmlFor="finding-title">{text.findingTitle}</label>
                <input id="finding-title" value={findingInput.title} onChange={(event) => setFindingInput((input) => ({ ...input, title: event.target.value }))} required disabled={!engagementID} />
                <label htmlFor="finding-description">{text.description}</label>
                <textarea id="finding-description" value={findingInput.description} onChange={(event) => setFindingInput((input) => ({ ...input, description: event.target.value }))} disabled={!engagementID} />
                <label htmlFor="finding-impact">{text.impact}</label>
                <textarea id="finding-impact" value={findingInput.impact} onChange={(event) => setFindingInput((input) => ({ ...input, impact: event.target.value }))} disabled={!engagementID} />
                <label htmlFor="finding-remediation">{text.remediation}</label>
                <textarea id="finding-remediation" value={findingInput.remediation} onChange={(event) => setFindingInput((input) => ({ ...input, remediation: event.target.value }))} disabled={!engagementID} />
                <label htmlFor="finding-reproduction">{text.reproduction}</label>
                <textarea id="finding-reproduction" value={findingInput.reproduction} onChange={(event) => setFindingInput((input) => ({ ...input, reproduction: event.target.value }))} disabled={!engagementID} />
                <label htmlFor="finding-cvss">{text.cvssVector}</label>
                <input id="finding-cvss" value={findingInput.cvssVector} onChange={(event) => setFindingInput((input) => ({ ...input, cvssVector: event.target.value }))} required disabled={!engagementID} />
                <button type="submit" disabled={!engagementID || busy === "finding"}>{busy === "finding" ? text.creatingFinding : text.createFinding}</button>
              </form>
              <label htmlFor="finding-select">{text.selectFinding}</label>
              <select id="finding-select" value={findingID} onChange={(event) => void selectFinding(event.target.value)} disabled={!engagementID}>
                <option value="">{text.selectFinding}</option>
                {findings.map((finding) => <option key={finding.id} value={finding.id}>{finding.title} — {text.score} {finding.cvssScore} — {finding.validationState}/{finding.remediationState ?? "new"}</option>)}
              </select>
              {engagementID && !findings.length && <p>{text.noFindings}</p>}
            </section>
            <section className="panel" aria-labelledby="retests-title">
              <h2 id="retests-title">{text.retests}</h2>
              {selectedFinding?.validationState === "new" && <button type="button" onClick={() => void confirmFinding()} disabled={busy === "triage"}>{busy === "triage" ? text.confirmingFinding : text.confirmFinding}</button>}
              <form onSubmit={submitRetest}>
                <label htmlFor="retest-result">{text.resultState}</label>
                <select id="retest-result" value={retestInput.resultState} onChange={(event) => setRetestInput((input) => ({ ...input, resultState: event.target.value as RetestInput["resultState"] }))} disabled={selectedFinding?.remediationState !== "open"}>
                  <option value="open">open</option><option value="fixed">fixed</option><option value="not_reproduced">not_reproduced</option>
                </select>
                <label htmlFor="retest-procedure">{text.procedure}</label>
                <textarea id="retest-procedure" value={retestInput.procedure} onChange={(event) => setRetestInput((input) => ({ ...input, procedure: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} />
                <label htmlFor="retest-observed">{text.observedResult}</label>
                <textarea id="retest-observed" value={retestInput.observedResult} onChange={(event) => setRetestInput((input) => ({ ...input, observedResult: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} />
                <label htmlFor="retest-justification">{text.justification}</label>
                <textarea id="retest-justification" value={retestInput.justification} onChange={(event) => setRetestInput((input) => ({ ...input, justification: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} />
                <button type="submit" disabled={selectedFinding?.remediationState !== "open" || busy === "retest"}>{busy === "retest" ? text.recordingRetest : text.recordRetest}</button>
              </form>
              <ul aria-live="polite">{retests.map((retest) => <li key={retest.id}>#{retest.round}: {retest.previousState} → {retest.resultState}</li>)}</ul>
              {findingID && !retests.length && <p>{text.noRetests}</p>}
            </section>
          </div>
        </section>
      )}
    </main>
  );
}
