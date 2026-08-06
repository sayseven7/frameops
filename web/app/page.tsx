"use client";

import { FormEvent, useEffect, useRef, useState } from "react";

import { approveReportRevision, captureEvidence, collectionItems, createEngagement as createEngagementRequest, createFinding, createMethodology, deriveReportPDF, generateReportRevision, publishMethodology, readEvidence, readEngagementChecklist, readIngestions, readReportRevisions, recordRetest, triageFinding, type Client, type Engagement, type EngagementChecklist, type Evidence, type Finding, type FindingInput, type Ingestion, type Methodology, type MethodologyInput, type ReportPDF, type ReportRevision, type Retest, type RetestInput, requestJSON, uploadReportRevision } from "./api";
import { apiErrorMessage, copy, stateLabel, type Locale } from "./copy";
import { currentIngestions, formatBytes, operationalQueue } from "./operations";
import RootRedirect from "./root-redirect";
import { workspaceRoute } from "./routes";

type Collection<T> = { items: T[] | null };
export type Section = "overview" | "clients" | "projects" | "methodologies" | "findings" | "evidence" | "imports" | "reports";

export type WorkspaceProps = {
  initialSection?: Section;
  initialProjectID?: string;
  initialCSRF?: string;
};

export function Workspace({ initialSection = "overview", initialProjectID = "", initialCSRF = "" }: WorkspaceProps) {
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [theme, setTheme] = useState<"dark" | "light" | "system">("dark");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [csrf, setCSRF] = useState(initialCSRF);
  const [clients, setClients] = useState<Client[]>([]);
  const [engagements, setEngagements] = useState<Engagement[]>([]);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [retests, setRetests] = useState<Retest[]>([]);
  const [evidence, setEvidence] = useState<Evidence[]>([]);
  const [evidenceFile, setEvidenceFile] = useState<File>();
  const [reports, setReports] = useState<ReportRevision[]>([]);
  const [ingestions, setIngestions] = useState<Ingestion[]>([]);
  const [reportFile, setReportFile] = useState<File>();
  const [reportPDFs, setReportPDFs] = useState<ReportPDF[]>([]);
  const [methodologies, setMethodologies] = useState<Methodology[]>([]);
  const [checklist, setChecklist] = useState<EngagementChecklist>();
  const [clientID, setClientID] = useState("");
  const [engagementID, setEngagementID] = useState(initialProjectID);
  const [findingID, setFindingID] = useState("");
  const [clientName, setClientName] = useState("");
  const [engagementName, setEngagementName] = useState("");
  const [methodologyID, setMethodologyID] = useState("");
  const [methodologyInput, setMethodologyInput] = useState<MethodologyInput>({ name: "", sourceName: "", sourceVersion: "", attribution: "", items: [{ title: "", objective: "", procedure: "" }] });
  const [findingInput, setFindingInput] = useState<FindingInput>({ title: "", description: "", impact: "", remediation: "", reproduction: "", cvssVector: "" });
  const [retestInput, setRetestInput] = useState<Omit<RetestInput, "round">>({ resultState: "open", procedure: "", observedResult: "", justification: "" });
  const [busy, setBusy] = useState<"login" | "client" | "methodology" | "publish" | "engagement" | "finding" | "triage" | "retest" | "evidence" | "report" | "generate" | "approve" | "pdf" | "">("");
  const [error, setError] = useState("");
  const section = initialSection;
  const errorRef = useRef<HTMLParagraphElement>(null);
  const contentRef = useRef<HTMLElement>(null);
  const findingIDRef = useRef("");
  const engagementIDRef = useRef(initialProjectID);
  const text = copy[locale];

  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);

  useEffect(() => {
    if (csrf) contentRef.current?.focus({ preventScroll: true });
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

  async function loadIngestions(id: string) {
    const items = currentIngestions(engagementIDRef.current, id, collectionItems(await readIngestions(id)));
    if (items) setIngestions(items);
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
    engagementIDRef.current = id;
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
    setIngestions([]);
    if (!id) {
      return;
    }
    try {
      const [, , , engagementChecklist] = await Promise.all([loadFindings(id), loadReports(id), loadIngestions(id), readEngagementChecklist(id)]);
      setChecklist(engagementChecklist);
    } catch (reason) {
      setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale));
    }
  }

  useEffect(() => {
    if (initialProjectID) void Promise.resolve().then(() => selectEngagement(initialProjectID));
    // The route is the source of truth; remounting on its key clears old project state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialProjectID]);

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

  async function generateReport() {
    if (!engagementID) return;
    setBusy("generate");
    setError("");
    try {
      const report = await generateReportRevision(engagementID, csrf);
      setReports((current) => [...current, report]);
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
  const queue = operationalQueue(findings, reports, reportPDFs);
  const queueTotal = Object.values(queue).reduce((total, count) => total + count, 0);
  const navigation: { id: Section; label: string; href: string }[] = [
    { id: "overview", label: text.overview, href: "/dashboard" }, { id: "clients", label: text.clients, href: "/clients" }, { id: "projects", label: text.projects, href: "/projects" },
    { id: "methodologies", label: text.methodologies, href: workspaceRoute(engagementID, "methodology") }, { id: "findings", label: text.findings, href: workspaceRoute(engagementID, "findings") }, { id: "evidence", label: text.evidence, href: workspaceRoute(engagementID, "evidence") }, { id: "imports", label: text.imports, href: workspaceRoute(engagementID, "imports") },
    { id: "reports", label: text.reports, href: workspaceRoute(engagementID, "reports") },
  ];
  const stateClass = (state: string | null | undefined) => `status status-${(state ?? "new").replaceAll("_", "-")}`;

  return (
    <main className={signedIn ? "app-shell" : "login-shell"} data-theme={theme}>
      {!signedIn ? <section className="login-panel" aria-labelledby="sign-in-title">
        <header className="login-header"><div><strong>{text.title}</strong><span>{text.subtitle}</span></div>
        <label><span>{text.language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{text.portuguese}</option><option value="en">{text.english}</option></select></label></header>
        {error && <p className="error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}
        <h1 id="sign-in-title">{text.signIn}</h1>
        <form onSubmit={signIn}><label htmlFor="email">{text.email}</label><input id="email" type="email" autoComplete="email" value={email} onChange={(event) => setEmail(event.target.value)} required /><label htmlFor="password">{text.password}</label><input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /><button type="submit" disabled={busy === "login"}>{busy === "login" ? text.signingIn : text.signInAction}</button></form>
      </section> : <>
        <aside className="app-rail" aria-label={text.title}>
          <div className="brand"><span className="brand-mark" aria-hidden="true">F7</span><div><strong>{text.title}</strong><span>{text.subtitle}</span></div></div>
          <nav>{navigation.map((item) => <a key={item.id} href={item.href} className={section === item.id ? "active" : ""} aria-current={section === item.id ? "page" : undefined}><span>{item.label}</span>{item.id === "overview" && queueTotal > 0 && <small aria-label={`${queueTotal} ${text.pendingActions}`}>{queueTotal}</small>}</a>)}</nav>
          <div className="rail-context"><span>{text.engagementContext}</span><strong>{selectedEngagement?.name ?? text.noActiveProject}</strong><small>{selectedClient?.name ?? text.selectClient}</small></div>
          <label className="locale-control"><span>{text.language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{text.portuguese}</option><option value="en">{text.english}</option></select></label>
          <label className="theme-control"><span>{text.theme}</span><select value={theme} onChange={(event) => setTheme(event.target.value as typeof theme)}><option value="dark">{text.themeDark}</option><option value="light">{text.themeLight}</option><option value="system">{text.themeSystem}</option></select></label>
        </aside>
        <section className="app-content" ref={contentRef} tabIndex={-1} aria-label={navigation.find((item) => item.id === section)?.label}>
          <header className="context-bar">
            <div className="page-heading"><span>{text.operationsDesk}</span><strong>{navigation.find((item) => item.id === section)?.label}</strong></div>
            <div className="context-controls"><label htmlFor="global-client"><span>{text.clientContext}</span><select id="global-client" value={clientID} onChange={(event) => void selectClient(event.target.value)}><option value="">{text.selectClient}</option>{clients.map((client) => <option key={client.id} value={client.id}>{client.name}</option>)}</select></label><span className="context-separator" aria-hidden="true">/</span><label htmlFor="global-engagement"><span>{text.projectContext}</span><select id="global-engagement" value={engagementID} onChange={(event) => void selectEngagement(event.target.value)} disabled={!clientID}><option value="">{text.selectEngagement}</option>{engagements.map((engagement) => <option key={engagement.id} value={engagement.id}>{engagement.name}</option>)}</select></label></div>
            <p className={busy ? "system-state busy" : "system-state"} aria-live="polite"><span aria-hidden="true" />{busy ? text.loadingWorkspace : text.sessionReady}</p>
          </header>
          {error && <p className="error global-error" role="alert" tabIndex={-1} ref={errorRef}>{error}</p>}

          {section === "overview" && <section className="workspace" aria-labelledby="overview-title">
            <header className="surface-header"><div><h1 id="overview-title">{selectedEngagement?.name ?? text.workspace}</h1><p>{selectedEngagement ? `${selectedClient?.name} / ${text.projectSummary}` : text.workbenchDescription}</p></div>{engagementID && <a className="secondary-action" href={workspaceRoute(engagementID, "findings")}>{text.openFindings}</a>}</header>
            <div className="operations-ledger" aria-label={text.operationalSummary}><div><span>{text.findings}</span><strong>{findings.length}</strong></div><div><span>{text.selectedFindingEvidence}</span><strong>{selectedFinding ? evidence.length : "—"}</strong></div><div><span>{text.selectedFindingRetests}</span><strong>{selectedFinding ? retests.length : "—"}</strong></div><div><span>{text.reports}</span><strong>{reports.length}</strong></div><div><span>{text.checklistItems}</span><strong>{checklist?.items.length ?? 0}</strong></div></div>
            <div className="overview-grid">
              <section className="surface-map" aria-labelledby="surface-title"><header><div><h2 id="surface-title">{text.attackSurface}</h2><p>{text.attackSurfaceDescription}</p></div><span className={engagementID ? "scope-state active" : "scope-state"}>{engagementID ? text.scopeLoaded : text.noActiveProject}</span></header>
                {engagementID ? <div className="map-body"><div className="scope-core"><span>{text.authorizedScope}</span><strong>{selectedEngagement?.name}</strong><small>{selectedClient?.name}</small></div><div className="finding-nodes">{findings.length ? findings.map((finding) => <a key={finding.id} href={workspaceRoute(engagementID, "findings")} className={finding.id === findingID ? "finding-node selected" : "finding-node"}><span>{text.score} {finding.cvssScore}</span><strong>{finding.title}</strong><small>{stateLabel(finding.validationState, locale)} · {stateLabel(finding.remediationState ?? "new", locale)}</small></a>) : <div className="map-empty"><strong>{text.noFindings}</strong><a className="text-action" href={workspaceRoute(engagementID, "findings")}>{text.recordFirstFinding}</a></div>}</div></div> : <div className="empty-state"><strong>{text.selectOperationalContext}</strong><p>{text.noEngagementSelected}</p></div>}
                <footer><span><i className="legend scope" />{text.authorizedScope}</span><span><i className="legend record" />{text.structuredRecord}</span><span>{text.currentStateOnly}</span></footer>
              </section>
              <aside className="action-queue" aria-labelledby="queue-title"><header><h2 id="queue-title">{text.actionQueue}</h2><span>{queueTotal}</span></header>{engagementID ? <ol><li><a href={workspaceRoute(engagementID, "findings")}><span><strong>{text.triageQueue}</strong><small>{text.triageQueueHelp}</small></span><b>{queue.triage}</b></a></li><li><a href={workspaceRoute(engagementID, "evidence")}><span><strong>{text.retestQueue}</strong><small>{text.retestQueueHelp}</small></span><b>{queue.retest}</b></a></li><li><a href={workspaceRoute(engagementID, "reports")}><span><strong>{text.approvalQueue}</strong><small>{text.approvalQueueHelp}</small></span><b>{queue.approval}</b></a></li><li><a href={workspaceRoute(engagementID, "reports")}><span><strong>{text.deliveryQueue}</strong><small>{text.deliveryQueueHelp}</small></span><b>{queue.delivery}</b></a></li></ol> : <div className="empty-state compact"><p>{text.queueNeedsProject}</p></div>}</aside>
            </div>
            <section className="checklist-strip"><header><div><h2>{text.projectChecklist}</h2>{checklist && <p>{checklist.name} v{checklist.versionNumber} · {checklist.sourceName} {checklist.sourceVersion}</p>}</div><span>{checklist?.items.length ?? 0} {text.items}</span></header>{checklist ? <ol>{checklist.items.map((item) => <li key={item.position}><span>{String(item.position ?? 0).padStart(2, "0")}</span><div><strong>{item.title}</strong><p>{item.objective}</p></div><details><summary>{text.procedure}</summary><p>{item.procedure}</p></details></li>)}</ol> : <p className="empty-line">{text.noChecklist}</p>}</section>
          </section>}

          {section === "clients" && <section className="workspace" aria-labelledby="clients-title"><header className="surface-header"><div><h1 id="clients-title">{text.clients}</h1><p>{text.clientsDescription}</p></div><details name="workspace-task" className="task-disclosure"><summary>{text.createClient}</summary><form onSubmit={createClient}><label htmlFor="client-name">{text.clientName}</label><input id="client-name" value={clientName} onChange={(event) => setClientName(event.target.value)} required /><button type="submit" disabled={busy === "client"}>{busy === "client" ? text.creatingClient : text.createClient}</button></form></details></header><div className="data-surface"><div className="table-head clients-grid"><span>{text.clientName}</span><span>{text.projects}</span><span>{text.context}</span></div>{clients.map((client) => <button type="button" className={client.id === clientID ? "data-row clients-grid selected" : "data-row clients-grid"} key={client.id} onClick={() => void selectClient(client.id)}><strong>{client.name}</strong><span>{client.id === clientID ? engagements.length : "—"}</span><span className={client.id === clientID ? "status status-active" : "status"}>{client.id === clientID ? text.active : text.select}</span></button>)}{!clients.length && <div className="empty-state"><strong>{text.noClients}</strong><p>{text.createClientHelp}</p></div>}</div></section>}

          {section === "methodologies" && <section className="workspace" aria-labelledby="methodologies-title"><header className="surface-header"><div><h1 id="methodologies-title">{text.methodologies}</h1><p>{text.methodologiesDescription}</p></div><details name="workspace-task" className="task-disclosure wide"><summary>{text.createMethodology}</summary><form onSubmit={submitMethodology}><div className="form-grid"><label htmlFor="methodology-name">{text.methodologyName}<input id="methodology-name" value={methodologyInput.name} onChange={(event) => setMethodologyInput((input) => ({ ...input, name: event.target.value }))} required /></label><label htmlFor="methodology-source">{text.sourceName}<input id="methodology-source" value={methodologyInput.sourceName} onChange={(event) => setMethodologyInput((input) => ({ ...input, sourceName: event.target.value }))} required /></label><label htmlFor="methodology-source-version">{text.sourceVersion}<input id="methodology-source-version" value={methodologyInput.sourceVersion} onChange={(event) => setMethodologyInput((input) => ({ ...input, sourceVersion: event.target.value }))} required /></label></div><label htmlFor="methodology-attribution">{text.attribution}<textarea id="methodology-attribution" value={methodologyInput.attribution} onChange={(event) => setMethodologyInput((input) => ({ ...input, attribution: event.target.value }))} required /></label><div className="form-grid"><label htmlFor="methodology-item-title">{text.checklistItemTitle}<input id="methodology-item-title" value={methodologyInput.items[0].title} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], title: event.target.value }] }))} required /></label><label htmlFor="methodology-item-objective">{text.checklistObjective}<textarea id="methodology-item-objective" value={methodologyInput.items[0].objective} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], objective: event.target.value }] }))} required /></label><label htmlFor="methodology-item-procedure">{text.checklistProcedure}<textarea id="methodology-item-procedure" value={methodologyInput.items[0].procedure} onChange={(event) => setMethodologyInput((input) => ({ ...input, items: [{ ...input.items[0], procedure: event.target.value }] }))} required /></label></div><button type="submit" disabled={busy === "methodology"}>{busy === "methodology" ? text.creatingMethodology : text.createMethodology}</button></form></details></header><div className="data-surface" role="table" aria-label={text.methodologies}><div className="table-head methodology-grid" role="row"><span role="columnheader">{text.methodologyName}</span><span role="columnheader">{text.sourceVersion}</span><span role="columnheader">{text.items}</span><span role="columnheader">{text.status}</span><span role="columnheader">{text.action}</span></div>{methodologies.map((methodology) => <div className="data-row methodology-grid" role="row" key={methodology.id}><strong role="cell">{methodology.name}</strong><span role="cell">{methodology.sourceName} · v{methodology.versionNumber}</span><span role="cell">{methodology.items.length}</span><span role="cell" className={stateClass(methodology.state)}>{stateLabel(methodology.state, locale)}</span><span role="cell">{methodology.state === "draft" ? <button className="inline-action" type="button" onClick={() => void publish(methodology.templateId)} disabled={busy === "publish"}>{busy === "publish" ? text.publishingMethodology : text.publishMethodology}</button> : "—"}</span></div>)}{!methodologies.length && <div className="empty-state"><strong>{text.noMethodologies}</strong></div>}</div></section>}

          {section === "projects" && <section className="workspace" aria-labelledby="engagements-title"><header className="surface-header"><div><h1 id="engagements-title">{text.projects}</h1><p>{selectedClient ? `${text.clientContext}: ${selectedClient.name}` : text.projectsDescription}</p></div><details name="workspace-task" className="task-disclosure"><summary>{text.createEngagement}</summary><form onSubmit={createEngagement}><label htmlFor="engagement-name">{text.engagementName}</label><input id="engagement-name" value={engagementName} onChange={(event) => setEngagementName(event.target.value)} required disabled={!clientID} /><label htmlFor="methodology-select">{text.selectMethodology}</label><select id="methodology-select" value={methodologyID} onChange={(event) => setMethodologyID(event.target.value)} required disabled={!clientID}><option value="">{text.selectMethodology}</option>{methodologies.filter((methodology) => methodology.state === "published").map((methodology) => <option key={methodology.id} value={methodology.id}>{methodology.name} v{methodology.versionNumber}</option>)}</select><button type="submit" disabled={!clientID || !methodologyID || busy === "engagement"}>{busy === "engagement" ? text.creatingEngagement : text.createEngagement}</button></form></details></header>{!clientID ? <div className="empty-state standalone"><strong>{text.selectClient}</strong><p>{text.projectNeedsClient}</p></div> : <div className="split-workspace"><div className="data-surface"><div className="table-head project-grid"><span>{text.engagementName}</span><span>{text.context}</span></div>{engagements.map((engagement) => <button type="button" className={engagement.id === engagementID ? "data-row project-grid selected" : "data-row project-grid"} key={engagement.id} onClick={() => void selectEngagement(engagement.id)}><strong>{engagement.name}</strong><span className={engagement.id === engagementID ? "status status-active" : "status"}>{engagement.id === engagementID ? text.active : text.open}</span></button>)}{!engagements.length && <div className="empty-state"><strong>{text.noEngagements}</strong></div>}</div><aside className="detail-pane"><header><span>{text.projectChecklist}</span><strong>{checklist?.name ?? text.noChecklist}</strong></header>{checklist ? <ol>{checklist.items.map((item) => <li key={item.position}><span>{item.position}</span><div><strong>{item.title}</strong><p>{item.objective}</p></div></li>)}</ol> : <div className="empty-state compact"><p>{engagementID ? text.noChecklist : text.selectProjectDetail}</p></div>}</aside></div>}</section>}

          {section === "findings" && <section className="workspace" aria-labelledby="findings-title"><header className="surface-header"><div><h1 id="findings-title">{text.findings}</h1><p>{selectedEngagement ? selectedEngagement.name : text.findingsDescription}</p></div><details name="workspace-task" className="task-disclosure wide"><summary>{text.createFinding}</summary><form onSubmit={submitFinding}><div className="form-grid"><label htmlFor="finding-title">{text.findingTitle}<input id="finding-title" value={findingInput.title} onChange={(event) => setFindingInput((input) => ({ ...input, title: event.target.value }))} required disabled={!engagementID} /></label><label htmlFor="finding-cvss">{text.cvssVector}<input id="finding-cvss" value={findingInput.cvssVector} onChange={(event) => setFindingInput((input) => ({ ...input, cvssVector: event.target.value }))} required disabled={!engagementID} /></label></div><label htmlFor="finding-description">{text.description}<textarea id="finding-description" value={findingInput.description} onChange={(event) => setFindingInput((input) => ({ ...input, description: event.target.value }))} disabled={!engagementID} /></label><div className="form-grid three"><label htmlFor="finding-impact">{text.impact}<textarea id="finding-impact" value={findingInput.impact} onChange={(event) => setFindingInput((input) => ({ ...input, impact: event.target.value }))} disabled={!engagementID} /></label><label htmlFor="finding-remediation">{text.remediation}<textarea id="finding-remediation" value={findingInput.remediation} onChange={(event) => setFindingInput((input) => ({ ...input, remediation: event.target.value }))} disabled={!engagementID} /></label><label htmlFor="finding-reproduction">{text.reproduction}<textarea id="finding-reproduction" value={findingInput.reproduction} onChange={(event) => setFindingInput((input) => ({ ...input, reproduction: event.target.value }))} disabled={!engagementID} /></label></div><button type="submit" disabled={!engagementID || busy === "finding"}>{busy === "finding" ? text.creatingFinding : text.createFinding}</button></form></details></header>{!engagementID ? <div className="empty-state standalone"><strong>{text.selectEngagement}</strong><p>{text.findingsNeedProject}</p></div> : <div className="split-workspace findings-layout"><div className="data-surface"><div className="table-head finding-grid"><span>{text.findingTitle}</span><span>{text.score}</span><span>{text.status}</span></div>{findings.map((finding) => <button type="button" className={finding.id === findingID ? "data-row finding-grid selected" : "data-row finding-grid"} key={finding.id} onClick={() => void selectFinding(finding.id)}><strong>{finding.title}</strong><b>{finding.cvssScore}</b><span className={stateClass(finding.remediationState ?? finding.validationState)}>{stateLabel(finding.validationState, locale)} / {stateLabel(finding.remediationState ?? "new", locale)}</span></button>)}{!findings.length && <div className="empty-state"><strong>{text.noFindings}</strong><p>{text.recordFirstFinding}</p></div>}</div><aside className="detail-pane finding-detail">{selectedFinding ? <><header><span>{text.findingDetail}</span><strong>{selectedFinding.title}</strong><div><span className="score-badge">CVSS {selectedFinding.cvssScore}</span><span className={stateClass(selectedFinding.validationState)}>{stateLabel(selectedFinding.validationState, locale)}</span><span className={stateClass(selectedFinding.remediationState)}>{stateLabel(selectedFinding.remediationState ?? "new", locale)}</span></div></header><dl><div><dt>{text.description}</dt><dd>{selectedFinding.description || "—"}</dd></div><div><dt>{text.impact}</dt><dd>{selectedFinding.impact || "—"}</dd></div><div><dt>{text.remediation}</dt><dd>{selectedFinding.remediation || "—"}</dd></div><div><dt>{text.reproduction}</dt><dd>{selectedFinding.reproduction || "—"}</dd></div><div><dt>{text.cvssVector}</dt><dd className="data-value">{selectedFinding.cvssVector}</dd></div></dl><div className="detail-actions">{selectedFinding.validationState === "new" && <button type="button" onClick={() => void confirmFinding()} disabled={busy === "triage"}>{busy === "triage" ? text.confirmingFinding : text.confirmFinding}</button>}<a className="secondary-action" href={workspaceRoute(engagementID, "evidence")}>{text.openEvidence}</a></div></> : <div className="empty-state compact"><strong>{text.selectFinding}</strong><p>{text.selectFindingDetail}</p></div>}</aside></div>}</section>}

          {section === "evidence" && <section className="workspace" aria-labelledby="evidence-title"><header className="surface-header"><div><h1 id="evidence-title">{text.evidence}</h1><p>{selectedFinding ? selectedFinding.title : text.evidenceDescription}</p></div><div className="header-actions"><label htmlFor="evidence-finding-select"><span>{text.findingContext}</span><select id="evidence-finding-select" value={findingID} onChange={(event) => void selectFinding(event.target.value)} disabled={!engagementID}><option value="">{text.selectFinding}</option>{findings.map((finding) => <option key={finding.id} value={finding.id}>{finding.title} · {finding.cvssScore}</option>)}</select></label><details name="workspace-task" className="task-disclosure"><summary>{text.captureEvidence}</summary><form onSubmit={submitEvidence}><label htmlFor="evidence-file">{text.evidenceFile}</label><input id="evidence-file" type="file" onChange={(event) => setEvidenceFile(event.target.files?.[0])} disabled={!findingID || busy === "evidence"} required /><button type="submit" disabled={!findingID || !evidenceFile || busy === "evidence"}>{busy === "evidence" ? text.capturingEvidence : text.captureEvidence}</button></form></details></div></header>{!findingID ? <div className="empty-state standalone"><strong>{text.selectFinding}</strong><p>{text.evidenceNeedsFinding}</p></div> : <div className="evidence-layout"><section className="data-surface evidence-chain" role="table" aria-label={text.custodyChain}><header><div><h2>{text.custodyChain}</h2><p>{text.custodyChainDescription}</p></div><span>{evidence.length} {text.artifacts}</span></header><div className="table-head evidence-grid" role="row"><span role="columnheader">{text.artifact}</span><span role="columnheader">{text.size}</span><span role="columnheader">SHA-256</span><span role="columnheader">{text.status}</span></div>{evidence.map((item) => <div className="data-row evidence-grid" role="row" key={item.id}><strong role="cell">{item.filename}</strong><span role="cell">{formatBytes(item.byteSize)}</span><code role="cell" title={item.sha256}>{item.sha256}</code><span role="cell" className={stateClass(item.state)}>{stateLabel(item.state, locale)}</span></div>)}{!evidence.length && <div className="empty-state"><strong>{text.noEvidence}</strong><p>{text.captureEvidenceHelp}</p></div>}</section><aside className="detail-pane retest-pane"><header><span>{text.retests}</span><strong>{stateLabel(selectedFinding?.remediationState ?? "new", locale)}</strong></header>{selectedFinding?.validationState === "new" && <button type="button" onClick={() => void confirmFinding()} disabled={busy === "triage"}>{busy === "triage" ? text.confirmingFinding : text.confirmFinding}</button>}<details name="workspace-task" className="inline-disclosure"><summary>{text.recordRetest}</summary><form onSubmit={submitRetest}><label htmlFor="retest-result">{text.resultState}<select id="retest-result" value={retestInput.resultState} onChange={(event) => setRetestInput((input) => ({ ...input, resultState: event.target.value as RetestInput["resultState"] }))} disabled={selectedFinding?.remediationState !== "open"}><option value="open">{stateLabel("open", locale)}</option><option value="fixed">{stateLabel("fixed", locale)}</option><option value="not_reproduced">{stateLabel("not_reproduced", locale)}</option></select></label><label htmlFor="retest-procedure">{text.procedure}<textarea id="retest-procedure" value={retestInput.procedure} onChange={(event) => setRetestInput((input) => ({ ...input, procedure: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} /></label><label htmlFor="retest-observed">{text.observedResult}<textarea id="retest-observed" value={retestInput.observedResult} onChange={(event) => setRetestInput((input) => ({ ...input, observedResult: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} /></label><label htmlFor="retest-justification">{text.justification}<textarea id="retest-justification" value={retestInput.justification} onChange={(event) => setRetestInput((input) => ({ ...input, justification: event.target.value }))} required disabled={selectedFinding?.remediationState !== "open"} /></label><button type="submit" disabled={selectedFinding?.remediationState !== "open" || busy === "retest"}>{busy === "retest" ? text.recordingRetest : text.recordRetest}</button></form></details><ol className="timeline">{retests.map((retest) => <li key={retest.id}><span>#{retest.round}</span><div><strong>{stateLabel(retest.previousState, locale)} → {stateLabel(retest.resultState, locale)}</strong><p>{retest.observedResult}</p></div></li>)}</ol>{!retests.length && <p className="empty-line">{text.noRetests}</p>}</aside></div>}</section>}

          {section === "imports" && <section className="workspace" aria-labelledby="imports-title"><header className="surface-header"><div><h1 id="imports-title">{text.imports}</h1><p>{text.importsDescription}</p></div></header>{!engagementID ? <div className="empty-state standalone"><strong>{text.selectEngagement}</strong><p>{text.importsNeedProject}</p></div> : <><section className="data-surface import-surface" role="table" aria-label={text.imports}><div className="table-head import-grid" role="row"><span role="columnheader">{text.artifact}</span><span role="columnheader">{text.importSource}</span><span role="columnheader">{text.importReceivedAt}</span><span role="columnheader">{text.importResult}</span><span role="columnheader">{text.importState}</span></div>{ingestions.map((ingestion) => <div className="data-row import-grid" role="row" key={ingestion.id}><div role="cell"><strong>{ingestion.filename}</strong><small>{formatBytes(ingestion.byteSize)} · <code title={ingestion.sha256}>{ingestion.sha256}</code></small></div><span role="cell">{ingestion.tool} · {ingestion.formatVersion}</span><time role="cell" dateTime={ingestion.receivedAt}>{new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" }).format(new Date(ingestion.receivedAt))}</time><span role="cell">{ingestion.summary.created} {text.importCreated} · {ingestion.summary.reused} {text.importReused} · {ingestion.summary.ignored} {text.importIgnored} · {ingestion.summary.rejected} {text.importRejected}</span><span role="cell" className="status status-active">{text.importRecorded}</span></div>)}{!ingestions.length && <div className="empty-state"><strong>{text.noImports}</strong><p>{text.importsCommand}</p></div>}</section><p className="cli-command"><code>fops ingest nmap ./scan.xml --engagement {engagementID}</code></p></>}</section>}

          {section === "reports" && <section className="workspace" aria-labelledby="reports-title"><header className="surface-header"><div><h1 id="reports-title">{text.reports}</h1><p>{selectedEngagement ? selectedEngagement.name : text.reportsDescription}</p></div><div className="header-actions"><button type="button" className="secondary-action" onClick={() => void generateReport()} disabled={!engagementID || busy === "generate"}>{busy === "generate" ? text.loading : text.generateReport}</button><details name="workspace-task" className="task-disclosure"><summary>{text.uploadReport}</summary><form onSubmit={submitReport}><label htmlFor="report-file">{text.reportFile}</label><input id="report-file" type="file" accept=".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document" onChange={(event) => setReportFile(event.target.files?.[0])} disabled={!engagementID || busy === "report"} required /><button type="submit" disabled={!engagementID || !reportFile || busy === "report"}>{busy === "report" ? text.uploadingReport : text.uploadReport}</button></form></details></div></header>{!engagementID ? <div className="empty-state standalone"><strong>{text.selectEngagement}</strong><p>{text.reportsNeedProject}</p></div> : <div className="data-surface report-surface" role="table" aria-label={text.reports}><div className="table-head report-grid" role="row"><span role="columnheader">{text.revision}</span><span role="columnheader">{text.size}</span><span role="columnheader">{text.integrity}</span><span role="columnheader">{text.status}</span><span role="columnheader">{text.action}</span></div>{reports.map((report) => <div className="data-row report-grid" role="row" key={report.id}><div role="cell"><strong>{report.filename}</strong>{reportPDFs.filter((pdf) => pdf.revisionId === report.id).map((pdf) => <small key={pdf.id}>{text.derivedPDF} · {formatBytes(pdf.byteSize)} · {stateLabel(pdf.state, locale)}</small>)}</div><span role="cell">{formatBytes(report.byteSize)}</span><code role="cell" title={report.sha256}>{report.sha256}</code><span role="cell" className={stateClass(report.approvedAt ? "approved" : report.state)}>{stateLabel(report.approvedAt ? "approved" : report.state, locale)}</span><span role="cell" className="row-actions">{report.state === "stored" && !report.approvedAt && <button className="inline-action" type="button" onClick={() => void approveReport(report.id)} disabled={busy === "approve"}>{busy === "approve" ? text.approvingReport : text.approveReport}</button>}{report.approvedAt && <button className="inline-action" type="button" onClick={() => void derivePDF(report.id)} disabled={busy === "pdf"}>{busy === "pdf" ? text.derivingPDF : text.derivePDF}</button>}</span></div>)}{!reports.length && <div className="empty-state"><strong>{text.noReports}</strong><p>{text.uploadReportHelp}</p></div>}</div>}</section>}
        </section>
      </>}
    </main>
  );
}

export default function HomePage() {
  return <RootRedirect />;
}
