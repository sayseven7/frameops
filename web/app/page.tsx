"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { approveReportRevision, captureEvidence, collectionItems, createEngagement as createEngagementRequest, createFinding, createMethodology, deriveReportPDF, publishMethodology, readEvidence, readEngagementChecklist, readReportRevisions, recordRetest, triageFinding, type Client, type Engagement, type EngagementChecklist, type Evidence, type Finding, type FindingInput, type Methodology, type MethodologyInput, type ReportPDF, type ReportRevision, type Retest, type RetestInput, requestJSON, uploadReportRevision } from "./api";
import { apiErrorMessage, copy, type Locale } from "./copy";

type Collection<T> = { items: T[] | null };
type Section = "overview" | "clients" | "projects" | "methodologies" | "findings" | "evidence" | "reports";

export default function HomePage() {
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [csrf, setCSRF] = useState("");
  const [clients, setClients] = useState<Client[]>([]);
  const [engagements, setEngagements] = useState<Engagement[]>([]);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [retests, setRetests] = useState<Retest[]>([]);
  const [evidence, setEvidence] = useState<Evidence[]>([]);
  const [evidenceFile, setEvidenceFile] = useState<File>();
  const [reports, setReports] = useState<ReportRevision[]>([]);
  const [reportFile, setReportFile] = useState<File>();
  const [reportPDFs, setReportPDFs] = useState<ReportPDF[]>([]);
  const [methodologies, setMethodologies] = useState<Methodology[]>([]);
  const [checklist, setChecklist] = useState<EngagementChecklist>();
  const [clientID, setClientID] = useState("");
  const [engagementID, setEngagementID] = useState("");
  const [findingID, setFindingID] = useState("");
  const [clientName, setClientName] = useState("");
  const [engagementName, setEngagementName] = useState("");
  const [methodologyID, setMethodologyID] = useState("");
  const [methodologyInput, setMethodologyInput] = useState<MethodologyInput>({ name: "", sourceName: "", sourceVersion: "", attribution: "", items: [{ title: "", objective: "", procedure: "" }] });
  const [findingInput, setFindingInput] = useState<FindingInput>({ title: "", description: "", impact: "", remediation: "", reproduction: "", cvssVector: "" });
  const [retestInput, setRetestInput] = useState<Omit<RetestInput, "round">>({ resultState: "open", procedure: "", observedResult: "", justification: "" });
  const [busy, setBusy] = useState<"login" | "client" | "methodology" | "publish" | "engagement" | "finding" | "triage" | "retest" | "evidence" | "report" | "approve" | "pdf" | "">("");
  const [error, setError] = useState("");
  const [section, setSection] = useState<Section>("overview");
  const errorRef = useRef<HTMLParagraphElement>(null);
  const contentRef = useRef<HTMLElement>(null);
  const findingIDRef = useRef("");
  const text = copy[locale];

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  useEffect(() => {
    if (csrf) contentRef.current?.focus();
  }, [csrf, section]);

  async function loadEngagements(id: string) {
    setEngagements(collectionItems(await requestJSON<Collection<Engagement>>(`/v1/clients/${encodeURIComponent(id)}/engagements`)));
  }

  async function loadFindings(id: string) {
    setFindings(collectionItems(await requestJSON<Collection<Finding>>(`/v1/engagements/${encodeURIComponent(id)}/findings`)));
  }

  async function loadRetests(id: string) {
    setRetests(collectionItems(await requestJSON<Collection<Retest>>(`/v1/findings/${encodeURIComponent(id)}/retests`)));
  }

  async function loadEvidence(id: string) {
    const items = collectionItems(await readEvidence(id));
    if (findingIDRef.current === id) setEvidence(items);
  }

  async function loadReports(id: string) {
    setReports(collectionItems(await readReportRevisions(id)));
  }

  async function loadMethodologies() {
    setMethodologies(collectionItems(await requestJSON<Collection<Methodology>>("/v1/methodology-templates")));
  }

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("login");
    setError("");
    try {
      await requestJSON<void>("/v1/session/login", { method: "POST", body: JSON.stringify({ email, password }) });
      const token = await requestJSON<{ token: string }>("/v1/csrf");
      setCSRF(token.token);
      setClients(collectionItems(await requestJSON<Collection<Client>>("/v1/clients")));
      await loadMethodologies();
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
    setEngagements([]);
    setEngagementID("");
    setChecklist(undefined);
    setFindings([]);
    setFindingID("");
    findingIDRef.current = "";
    setRetests([]);
    setEvidence([]);
    setEvidenceFile(undefined);
    setReports([]);
    setReportFile(undefined);
    setReportPDFs([]);
    if (!id) {
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
    setChecklist(undefined);
    setFindings([]);
    setFindingID("");
    findingIDRef.current = "";
    setRetests([]);
    setEvidence([]);
    setEvidenceFile(undefined);
    setReports([]);
    setReportFile(undefined);
    setReportPDFs([]);
    setError("");
    if (!id) {
      return;
    }
    try {
      const [, , engagementChecklist] = await Promise.all([loadFindings(id), loadReports(id), readEngagementChecklist(id)]);
      setChecklist(engagementChecklist);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    }
  }

  async function selectFinding(id: string) {
    setFindingID(id);
    findingIDRef.current = id;
    setEvidenceFile(undefined);
    setError("");
    if (!id) {
      setRetests([]);
      setEvidence([]);
      setEvidenceFile(undefined);
      return;
    }
    try {
      await Promise.all([loadRetests(id), loadEvidence(id)]);
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

  async function submitMethodology(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("methodology");
    setError("");
    try {
      const methodology = await createMethodology(methodologyInput, csrf);
      setMethodologies((current) => [...current, methodology]);
      setMethodologyInput({ name: "", sourceName: "", sourceVersion: "", attribution: "", items: [{ title: "", objective: "", procedure: "" }] });
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function publish(templateID: string) {
    setBusy("publish");
    setError("");
    try {
      const methodology = await publishMethodology(templateID, csrf);
      setMethodologies((current) => current.map((item) => item.id === methodology.id ? methodology : item));
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
    if (!methodologyID) {
      setError(text.noMethodologySelected);
      return;
    }
    setBusy("engagement");
    setError("");
    try {
      const engagement = await createEngagementRequest(clientID, { name: engagementName, methodologyVersionId: methodologyID }, csrf);
      setEngagements((current) => [...current, engagement]);
      setEngagementName("");
      await selectEngagement(engagement.id);
      setChecklist(await readEngagementChecklist(engagement.id));
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

  async function submitEvidence(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    if (!findingID || !evidenceFile) return;
    setBusy("evidence");
    setError("");
    try {
      await captureEvidence(findingID, evidenceFile, csrf);
      await loadEvidence(findingID);
      setEvidenceFile(undefined);
      form.reset();
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function submitReport(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    if (!engagementID || !reportFile) return;
    setBusy("report");
    setError("");
    try {
      const report = await uploadReportRevision(engagementID, reportFile, csrf);
      setReports((current) => [...current, report]);
      setReportFile(undefined);
      form.reset();
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function approveReport(revisionID: string) {
    setBusy("approve");
    setError("");
    try {
      const report = await approveReportRevision(revisionID, csrf);
      setReports((current) => current.map((item) => item.id === report.id ? report : item));
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  async function derivePDF(revisionID: string) {
    setBusy("pdf");
    setError("");
    try {
      const pdf = await deriveReportPDF(revisionID, csrf);
      setReportPDFs((current) => [...current, pdf]);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    } finally {
      setBusy("");
    }
  }

  const signedIn = Boolean(csrf);
  const selectedFinding = findings.find((finding) => finding.id === findingID);
  const selectedClient = clients.find((client) => client.id === clientID);
  const selectedEngagement = engagements.find((engagement) => engagement.id === engagementID);
  const navigation: { id: Section; label: string }[] = [
    { id: "overview", label: text.overview }, { id: "clients", label: text.clients }, { id: "projects", label: text.projects },
    { id: "methodologies", label: text.methodologies }, { id: "findings", label: text.findings }, { id: "evidence", label: text.evidence },
    { id: "reports", label: text.reports },
  ];

  return (
    <main className={signedIn ? "app-shell" : "login-shell"}>
      {!signedIn ? <section className="login-panel" aria-labelledby="sign-in-title">
        <header className="login-header"><div><strong>{text.title}</strong><span>{text.subtitle}</span></div>
        <label>
          <span>{text.language}</span>
          <select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}>
            <option value="pt-BR">{text.portuguese}</option>
            <option value="en">{text.english}</option>
          </select>
        </label>
        </header>
        {error && <p className="error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}
        <h1 id="sign-in-title">{text.signIn}</h1>
        <form onSubmit={signIn}>
          <label htmlFor="email">{text.email}</label>
          <input id="email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required />
          <label htmlFor="password">{text.password}</label>
          <input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
          <button type="submit" disabled={busy === "login"}>{busy === "login" ? text.signingIn : text.signInAction}</button>
        </form>
      </section> : <>
        <aside className="app-rail" aria-label={text.title}>
          <div className="brand"><strong>{text.title}</strong><span>{text.subtitle}</span></div>
          <nav>{navigation.map((item) => <button key={item.id} type="button" className={section === item.id ? "active" : ""} aria-current={section === item.id ? "page" : undefined} onClick={() => setSection(item.id)}>{item.label}</button>)}</nav>
          <label><span>{text.language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{text.portuguese}</option><option value="en">{text.english}</option></select></label>
        </aside>
        <section className="app-content" ref={contentRef} tabIndex={-1} aria-label={navigation.find((item) => item.id === section)?.label}>
          <header className="context-bar"><div><span>{text.engagementContext}</span><strong>{selectedClient?.name ?? text.selectClient} / {selectedEngagement?.name ?? text.selectEngagement}</strong></div><p aria-live="polite">{busy ? text.loadingWorkspace : text.sessionReady}</p></header>
          {error && <p className="error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}
          {section === "overview" && <section className="workbench" aria-labelledby="overview-title"><div><h1 id="overview-title">{text.workspace}</h1><p>{text.workbenchDescription}</p></div><div className="context-selectors"><label htmlFor="workspace-client">{text.clientContext}</label><select id="workspace-client" value={clientID} onChange={(event) => void selectClient(event.target.value)}><option value="">{text.selectClient}</option>{clients.map((client) => <option key={client.id} value={client.id}>{client.name}</option>)}</select><label htmlFor="workspace-engagement">{text.projectContext}</label><select id="workspace-engagement" value={engagementID} onChange={(event) => void selectEngagement(event.target.value)} disabled={!clientID}><option value="">{text.selectEngagement}</option>{engagements.map((engagement) => <option key={engagement.id} value={engagement.id}>{engagement.name}</option>)}</select></div><div className="workbench-summary"><h2>{text.workbench}</h2><p>{engagementID ? text.projectSummary : text.noEngagementSelected}</p></div></section>}
          {section === "clients" && <section className="surface" aria-labelledby="clients-title">
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
            </section>}
          {section === "methodologies" && <section className="surface" aria-labelledby="methodologies-title">
              <h2 id="methodologies-title">{text.methodologies}</h2>
              <form onSubmit={submitMethodology}>
                <label htmlFor="methodology-name">{text.methodologyName}</label>
                <input id="methodology-name" value={methodologyInput.name} onChange={(event) => setMethodologyInput((input) => ({ ...input, name: event.target.value }))} required />
                <label htmlFor="methodology-source">{text.sourceName}</label>
                <input id="methodology-source" value={methodologyInput.sourceName} onChange={(event) => setMethodologyInput((input) => ({ ...input, sourceName: event.target.value }))} required />
                <label htmlFor="methodology-source-version">{text.sourceVersion}</label>
                <input id="methodology-source-version" value={methodologyInput.sourceVersion} onChange={(event) => setMethodologyInput((input) => ({ ...input, sourceVersion: event.target.value }))} required />
                <label htmlFor="methodology-attribution">{text.attribution}</label>
                <textarea id="methodology-attribution" value={methodologyInput.attribution} onChange={(event) => setMethodologyInput((input) => ({ ...input, attribution: event.target.value }))} required />
                <label htmlFor="methodology-item-title">{text.checklistItemTitle}</label>
                <input id="methodology-item-title" value={methodologyInput.items[0].title} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], title: event.target.value }] }))} required />
                <label htmlFor="methodology-item-objective">{text.checklistObjective}</label>
                <textarea id="methodology-item-objective" value={methodologyInput.items[0].objective} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], objective: event.target.value }] }))} required />
                <label htmlFor="methodology-item-procedure">{text.checklistProcedure}</label>
                <textarea id="methodology-item-procedure" value={methodologyInput.items[0].procedure} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], procedure: event.target.value }] }))} required />
                <button type="submit" disabled={busy === "methodology"}>{busy === "methodology" ? text.creatingMethodology : text.createMethodology}</button>
              </form>
              <ul>{methodologies.map((methodology) => <li key={methodology.id}>{methodology.name} v{methodology.versionNumber} — {methodology.state} {methodology.state === "draft" && <button type="button" onClick={() => void publish(methodology.templateId)} disabled={busy === "publish"}>{busy === "publish" ? text.publishingMethodology : text.publishMethodology}</button>}</li>)}</ul>
            </section>}
          {section === "projects" && <section className="surface" aria-labelledby="engagements-title">
              <h2 id="engagements-title">{text.engagements}</h2>
              <form onSubmit={createEngagement}>
                <label htmlFor="engagement-name">{text.engagementName}</label>
                <input id="engagement-name" value={engagementName} onChange={(event) => setEngagementName(event.target.value)} required disabled={!clientID} />
                <label htmlFor="methodology-select">{text.selectMethodology}</label>
                <select id="methodology-select" value={methodologyID} onChange={(event) => setMethodologyID(event.target.value)} required disabled={!clientID}>
                  <option value="">{text.selectMethodology}</option>
                  {methodologies.filter((methodology) => methodology.state === "published").map((methodology) => <option key={methodology.id} value={methodology.id}>{methodology.name} v{methodology.versionNumber}</option>)}
                </select>
                <button type="submit" disabled={!clientID || !methodologyID || busy === "engagement"}>{busy === "engagement" ? text.creatingEngagement : text.createEngagement}</button>
              </form>
              <label htmlFor="engagement-select">{text.selectEngagement}</label>
              <select id="engagement-select" value={engagementID} onChange={(event) => void selectEngagement(event.target.value)} disabled={!clientID}>
                <option value="">{text.selectEngagement}</option>
                {engagements.map((engagement) => <option key={engagement.id} value={engagement.id}>{engagement.name}</option>)}
              </select>
              {clientID && !engagements.length && <p>{text.noEngagements}</p>}
              <div className="secondary-surface"><h3>{text.projectChecklist}</h3>{checklist ? <><p>{checklist.name} v{checklist.versionNumber} — {checklist.sourceName} {checklist.sourceVersion}</p><ul>{checklist.items.map((item) => <li key={item.position}>{item.title}: {item.objective} — {item.procedure}</li>)}</ul></> : <p>{text.noChecklist}</p>}</div>
            </section>}
          {section === "reports" && <section className="surface" aria-labelledby="reports-title">
              <h2 id="reports-title">{text.reports}</h2>
              <form onSubmit={submitReport}>
                <label htmlFor="report-file">{text.reportFile}</label>
                <input id="report-file" type="file" accept=".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document" onChange={(event) => setReportFile(event.target.files?.[0])} disabled={!engagementID || busy === "report"} required />
                <button type="submit" disabled={!engagementID || !reportFile || busy === "report"}>{busy === "report" ? text.uploadingReport : text.uploadReport}</button>
              </form>
              <ul aria-live="polite">{reports.map((report) => <li key={report.id}>{report.filename} — {report.state} — {report.sha256} — {report.byteSize} {report.state === "stored" && !report.approvedAt && <button type="button" onClick={() => void approveReport(report.id)} disabled={busy === "approve"}>{busy === "approve" ? text.approvingReport : text.approveReport}</button>} {report.approvedAt && <button type="button" onClick={() => void derivePDF(report.id)} disabled={busy === "pdf"}>{busy === "pdf" ? text.derivingPDF : text.derivePDF}</button>} {reportPDFs.filter((pdf) => pdf.revisionId === report.id).map((pdf) => <p key={pdf.id}>{text.derivedPDF} — {pdf.state} — {pdf.sourceSha256} — {pdf.sha256} — {pdf.byteSize}</p>)}</li>)}</ul>
              {engagementID && !reports.length && <p>{text.noReports}</p>}
            </section>}
          {section === "findings" && <section className="surface" aria-labelledby="findings-title">
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
            </section>}
          {section === "evidence" && <section className="surface" aria-labelledby="evidence-title">
              <h2 id="evidence-title">{text.evidence}</h2>
              <label htmlFor="evidence-finding-select">{text.selectFinding}</label>
              <select id="evidence-finding-select" value={findingID} onChange={(event) => void selectFinding(event.target.value)} disabled={!engagementID}>
                <option value="">{text.selectFinding}</option>
                {findings.map((finding) => <option key={finding.id} value={finding.id}>{finding.title} — {text.score} {finding.cvssScore}</option>)}
              </select>
              <form onSubmit={submitEvidence}>
                <label htmlFor="evidence-file">{text.evidenceFile}</label>
                <input id="evidence-file" type="file" onChange={(event) => setEvidenceFile(event.target.files?.[0])} disabled={!findingID || busy === "evidence"} required />
                <button type="submit" disabled={!findingID || !evidenceFile || busy === "evidence"}>{busy === "evidence" ? text.capturingEvidence : text.captureEvidence}</button>
              </form>
              <ul aria-live="polite">{evidence.map((item) => <li key={item.id}>{item.filename} — {item.state} — {item.sha256} — {item.byteSize}</li>)}</ul>
              {findingID && !evidence.length && <p>{text.noEvidence}</p>}
              <div className="secondary-surface"><h3>{text.retests}</h3>
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
              </div>
            </section>}
        </section>
      </>}
    </main>
  );
}
