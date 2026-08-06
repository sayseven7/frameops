"use client";

import { FormEvent, useEffect, useState } from "react";

import { apiErrorMessage, copy, type Locale } from "./copy";
import { collectionItems, createOrganizationMember, listOrganizationAuditEvents, listOrganizationMembers, readOrganization, type AuditEvent, type Organization, type OrganizationMember, updateOrganization, updateOrganizationMember } from "./api";

const organizationCopy = {
  "pt-BR": { title: "Organização", description: "Configurações, membros e trilha de auditoria do contexto atual.", settings: "Configurações", name: "Nome da organização", save: "Salvar", members: "Membros", membersHelp: "Somente administradores podem alterar papéis, ativação e credenciais iniciais.", member: "Membro", email: "E-mail", password: "Senha inicial", role: "Papel", memberRole: "Membro", adminRole: "Administrador", add: "Adicionar membro", status: "Status", action: "Ação", active: "Ativo", inactive: "Desativado", makeMember: "Tornar membro", makeAdmin: "Tornar admin", deactivate: "Desativar", activate: "Ativar", audit: "Auditoria", auditHelp: "Eventos administrativos recentes; não há conteúdo de senha ou token.", event: "Evento", target: "Alvo", outcome: "Resultado", date: "Data", loadMore: "Carregar mais" },
  en: { title: "Organization", description: "Settings, members, and the current context audit trail.", settings: "Settings", name: "Organization name", save: "Save", members: "Members", membersHelp: "Only administrators can change roles, activation, and initial credentials.", member: "Member", email: "Email", password: "Initial password", role: "Role", memberRole: "Member", adminRole: "Administrator", add: "Add member", status: "Status", action: "Action", active: "Active", inactive: "Inactive", makeMember: "Make member", makeAdmin: "Make admin", deactivate: "Deactivate", activate: "Activate", audit: "Audit", auditHelp: "Recent administrative events; no password or token content is included.", event: "Event", target: "Target", outcome: "Outcome", date: "Date", loadMore: "Load more" },
} satisfies Record<Locale, Record<string, string>>;

export function appendAuditPage(events: AuditEvent[], page: { items: AuditEvent[] | null; nextCursor: string }) {
  return { events: [...events, ...collectionItems(page)], nextCursor: page.nextCursor };
}

export default function OrganizationAdmin({ csrf }: { csrf: string }) {
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const [organization, setOrganization] = useState<Organization>();
  const [members, setMembers] = useState<OrganizationMember[]>([]);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const text = organizationCopy[locale];

  async function load() {
    const [current, memberList, audit] = await Promise.all([readOrganization(), listOrganizationMembers(), listOrganizationAuditEvents({ limit: 25 })]);
    setOrganization(current); setMembers(collectionItems(memberList)); setEvents(collectionItems(audit)); setNextCursor(audit.nextCursor);
  }
  useEffect(() => {
    let active = true;
    void Promise.resolve().then(load).catch((reason) => { if (active) setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); });
    return () => { active = false; };
    // Load once; actions refresh the same bounded first page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function rename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError("");
    try { const form = new FormData(event.currentTarget); setOrganization(await updateOrganization(String(form.get("name")), csrf)); await load(); }
    catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }
  async function createMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      const form = new FormData(event.currentTarget);
      await createOrganizationMember({ displayName: String(form.get("displayName")), email: String(form.get("email")), password: String(form.get("password")), role: String(form.get("role")) as "admin" | "member" }, csrf);
      event.currentTarget.reset(); await load();
    } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }
  async function changeMember(member: OrganizationMember, update: { role?: "admin" | "member"; isActive?: boolean }) {
    setBusy(true); setError("");
    try { await updateOrganizationMember(member.id, update, csrf); await load(); }
    catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }
  async function loadMore() {
    setBusy(true); setError("");
    try {
      const page = await listOrganizationAuditEvents({ limit: 25, cursor: nextCursor });
      const next = appendAuditPage(events, page);
      setEvents(next.events); setNextCursor(next.nextCursor);
    } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }

  return <main className="planning-shell" aria-busy={busy}>
    <header><a href="/dashboard">← FrameOPS</a><label><span>{copy[locale].language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{copy[locale].portuguese}</option><option value="en">{copy[locale].english}</option></select></label><h1>{text.title}</h1><p>{text.description}</p></header>
    {error && <p className="error" role="alert">{error}</p>}
    {!organization ? <p>{copy[locale].loading}</p> : <>
      <form className="planning-form" onSubmit={rename}><fieldset><legend>{text.settings}</legend><label htmlFor="organization-name">{text.name}<input id="organization-name" name="name" defaultValue={organization.name} required maxLength={200} /></label><button disabled={busy}>{text.save}</button></fieldset></form>
      <section className="planning-record" aria-labelledby="members-title"><header><h2 id="members-title">{text.members}</h2><p>{text.membersHelp}</p></header>
        <form onSubmit={createMember}><div className="form-grid"><label>{text.member}<input name="displayName" required /></label><label>{text.email}<input name="email" type="email" autoComplete="off" required /></label><label>{text.password}<input name="password" type="password" autoComplete="new-password" minLength={12} required /></label><label>{text.role}<select name="role" defaultValue="member"><option value="member">{text.memberRole}</option><option value="admin">{text.adminRole}</option></select></label></div><button disabled={busy}>{text.add}</button></form>
        <div className="data-surface"><div className="table-head report-grid"><span>{text.member}</span><span>{text.role}</span><span>{text.status}</span><span> </span><span>{text.action}</span></div>{members.map((member) => <div className="data-row report-grid" key={member.id}><div><strong>{member.displayName}</strong><small>{member.email}</small></div><span>{member.role === "admin" ? text.adminRole : text.memberRole}</span><span className={`status ${member.isActive ? "status-active" : "status-stored"}`}>{member.isActive ? text.active : text.inactive}</span><span /><span className="row-actions"><button className="inline-action" type="button" disabled={busy} onClick={() => void changeMember(member, { role: member.role === "admin" ? "member" : "admin" })}>{member.role === "admin" ? text.makeMember : text.makeAdmin}</button><button className="inline-action" type="button" disabled={busy} onClick={() => void changeMember(member, { isActive: !member.isActive })}>{member.isActive ? text.deactivate : text.activate}</button></span></div>)}</div>
      </section>
      <section className="planning-record" aria-labelledby="audit-title"><header><h2 id="audit-title">{text.audit}</h2><p>{text.auditHelp}</p></header><div className="data-surface"><div className="table-head report-grid"><span>{text.event}</span><span>{text.target}</span><span>{text.outcome}</span><span>{text.date}</span><span /></div>{events.map((event) => <div className="data-row report-grid" key={event.id}><strong>{event.action}</strong><span>{event.targetType}</span><span>{event.outcome}</span><time dateTime={event.createdAt}>{new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" }).format(new Date(event.createdAt))}</time><span /></div>)}</div>{nextCursor && <button type="button" disabled={busy} onClick={() => void loadMore()}>{text.loadMore}</button>}</section>
    </>}
  </main>;
}
