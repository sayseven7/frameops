"use client";

import { FormEvent, useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";

import { createProjectPlan, readProjectPlan, transitionProjectPlan, updateProjectPlan, type ProjectPlan, type ProjectPlanInput, requestJSON } from "./api";
import { destinationForSession } from "./auth";
import { apiErrorMessage, copy } from "./copy";

type PlanningWorkspaceProps = { engagementID: string };

const emptyPlan: ProjectPlanInput = { startsOn: "", endsOn: "", rulesOfEngagement: "", targets: [], exclusions: [], team: [], milestones: [] };

export default function PlanningWorkspace({ engagementID }: PlanningWorkspaceProps) {
  const pathname = usePathname();
  const router = useRouter();
  const [csrf, setCSRF] = useState("");
  const [plan, setPlan] = useState<ProjectPlan>();
  const [input, setInput] = useState<ProjectPlanInput>(emptyPlan);
  const [targets, setTargets] = useState("");
  const [exclusions, setExclusions] = useState("");
  const [milestoneTitle, setMilestoneTitle] = useState("");
  const [milestoneDueOn, setMilestoneDueOn] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const text = copy["pt-BR"];

  useEffect(() => {
    let active = true;
    void requestJSON<{ token: string }>("/v1/csrf").then(async ({ token }) => {
      if (!active) return;
      setCSRF(token);
      try {
        const saved = await readProjectPlan(engagementID);
        if (!active) return;
        setPlan(saved);
        setInput({ startsOn: saved.startsOn.slice(0, 10), endsOn: saved.endsOn.slice(0, 10), rulesOfEngagement: saved.rulesOfEngagement, targets: saved.scope.targets, exclusions: saved.scope.exclusions, team: saved.team, milestones: saved.milestones.map(({ title, dueOn }) => ({ title, dueOn: dueOn.slice(0, 10) })) });
        setTargets(saved.scope.targets.join("\n"));
        setExclusions(saved.scope.exclusions.join("\n"));
      } catch (reason) {
        if (reason instanceof Error && reason.message !== "not_found") setError(apiErrorMessage(reason.message, "pt-BR"));
      }
    }).catch(() => router.replace(destinationForSession(pathname, false)));
    return () => { active = false; };
  }, [engagementID, pathname, router]);

  function normalizedLines(value: string) { return value.split("\n").map((item) => item.trim()).filter(Boolean); }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true); setError("");
    const next = { ...input, targets: normalizedLines(targets), exclusions: normalizedLines(exclusions) };
    try {
      const saved = plan ? await updateProjectPlan(engagementID, next, csrf) : await createProjectPlan(engagementID, next, csrf);
      setPlan(saved); setInput({ ...next, team: saved.team });
    } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", "pt-BR")); } finally { setBusy(false); }
  }

  function addMilestone() {
    if (!milestoneTitle || !milestoneDueOn) return;
    setInput((current) => ({ ...current, milestones: [...current.milestones, { title: milestoneTitle, dueOn: milestoneDueOn }] }));
    setMilestoneTitle(""); setMilestoneDueOn("");
  }

  async function advance(status: "active" | "closed") {
    setBusy(true); setError("");
    try { await transitionProjectPlan(engagementID, status, csrf); setPlan((current) => current ? { ...current, status } : current); } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", "pt-BR")); } finally { setBusy(false); }
  }

  return <main className="planning-shell"><header><a href={`/projects/${encodeURIComponent(engagementID)}/overview`}>← {text.overview}</a><h1>Planejamento do projeto</h1><p>Escopo autorizado, regras, equipe e cronograma. Cada alteração de escopo cria uma versão histórica.</p></header>{error && <p className="error" role="alert">{error}</p>}<form onSubmit={save} className="planning-form"><fieldset disabled={busy || plan?.status !== "draft"}><legend>Autorização e cronograma</legend><div className="form-grid"><label>Início<input type="date" value={input.startsOn} onChange={(event) => setInput((current) => ({ ...current, startsOn: event.target.value }))} required /></label><label>Fim<input type="date" value={input.endsOn} onChange={(event) => setInput((current) => ({ ...current, endsOn: event.target.value }))} required /></label></div><label>Regras de engajamento<textarea value={input.rulesOfEngagement} onChange={(event) => setInput((current) => ({ ...current, rulesOfEngagement: event.target.value }))} required /></label><div className="form-grid"><label>Alvos autorizados (um por linha)<textarea value={targets} onChange={(event) => setTargets(event.target.value)} required /></label><label>Exclusões (uma por linha)<textarea value={exclusions} onChange={(event) => setExclusions(event.target.value)} /></label></div><div className="planning-milestones"><strong>Marcos</strong><div className="form-grid"><label>Título<input value={milestoneTitle} onChange={(event) => setMilestoneTitle(event.target.value)} /></label><label>Data<input type="date" value={milestoneDueOn} onChange={(event) => setMilestoneDueOn(event.target.value)} /></label></div><button type="button" onClick={addMilestone}>Adicionar marco</button><ul>{input.milestones.map((milestone, index) => <li key={`${milestone.title}-${milestone.dueOn}`}><span>{milestone.title} · {milestone.dueOn}</span><button type="button" onClick={() => setInput((current) => ({ ...current, milestones: current.milestones.filter((_, itemIndex) => itemIndex !== index) }))}>Remover</button></li>)}</ul></div><button type="submit">{plan ? "Salvar nova versão do escopo" : "Criar planejamento"}</button></fieldset></form>{plan && <section className="planning-record"><h2>Registro atual</h2><dl><div><dt>Estado</dt><dd>{plan.status}</dd></div><div><dt>Versão do escopo</dt><dd>{plan.scope.versionNumber}</dd></div><div><dt>Metodologia</dt><dd>Checklist imutável atribuído ao projeto</dd></div><div><dt>Equipe</dt><dd>{plan.team.map((member) => member.role).join(", ")}</dd></div></dl>{plan.status === "draft" && <button type="button" disabled={busy} onClick={() => void advance("active")}>Ativar projeto</button>}{plan.status === "active" && <button type="button" disabled={busy} onClick={() => void advance("closed")}>Encerrar projeto</button>}</section>}</main>;
}
