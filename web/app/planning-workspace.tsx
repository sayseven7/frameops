"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";

import { createProjectPlan, readProjectPlan, transitionProjectPlan, updateProjectPlan, type ProjectPlan, type ProjectPlanInput, requestJSON } from "./api";
import { destinationForSession } from "./auth";
import { apiErrorMessage, copy, type Locale } from "./copy";

type PlanningWorkspaceProps = { engagementID: string };

const emptyPlan: ProjectPlanInput = { startsOn: "", endsOn: "", rulesOfEngagement: "", targets: [], exclusions: [], team: [], milestones: [] };

export default function PlanningWorkspace({ engagementID }: PlanningWorkspaceProps) {
  const pathname = usePathname();
  const router = useRouter();
  const [locale, setLocale] = useState<Locale>("pt-BR");
  const localeRef = useRef(locale);
  const [csrf, setCSRF] = useState("");
  const [plan, setPlan] = useState<ProjectPlan>();
  const [input, setInput] = useState<ProjectPlanInput>(emptyPlan);
  const [targets, setTargets] = useState("");
  const [exclusions, setExclusions] = useState("");
  const [milestoneTitle, setMilestoneTitle] = useState("");
  const [milestoneDueOn, setMilestoneDueOn] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const text = copy[locale];

  useEffect(() => { localeRef.current = locale; }, [locale]);

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
        if (reason instanceof Error && reason.message !== "not_found") setError(apiErrorMessage(reason.message, localeRef.current));
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
    } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }

  function addMilestone() {
    if (!milestoneTitle || !milestoneDueOn) return;
    setInput((current) => ({ ...current, milestones: [...current.milestones, { title: milestoneTitle, dueOn: milestoneDueOn }] }));
    setMilestoneTitle(""); setMilestoneDueOn("");
  }

  async function advance(status: "active" | "closed") {
    setBusy(true); setError("");
    try { await transitionProjectPlan(engagementID, status, csrf); setPlan((current) => current ? { ...current, status } : current); } catch (reason) { setError(apiErrorMessage(reason instanceof Error ? reason.message : "", locale)); } finally { setBusy(false); }
  }

  return <main className="planning-shell"><header><a href={`/projects/${encodeURIComponent(engagementID)}/overview`}>← {text.overview}</a><label><span>{text.language}</span><select value={locale} onChange={(event) => setLocale(event.target.value as Locale)}><option value="pt-BR">{text.portuguese}</option><option value="en">{text.english}</option></select></label><h1>{text.projectPlanningTitle}</h1><p>{text.projectPlanningDescription}</p></header>{error && <p className="error" role="alert">{error}</p>}<form onSubmit={save} className="planning-form"><fieldset disabled={busy || plan?.status !== "draft"}><legend>{text.planningAuthorizationSchedule}</legend><div className="form-grid"><label>{text.startsOn}<input type="date" value={input.startsOn} onChange={(event) => setInput((current) => ({ ...current, startsOn: event.target.value }))} required /></label><label>{text.endsOn}<input type="date" value={input.endsOn} onChange={(event) => setInput((current) => ({ ...current, endsOn: event.target.value }))} required /></label></div><label>{text.rulesOfEngagement}<textarea value={input.rulesOfEngagement} onChange={(event) => setInput((current) => ({ ...current, rulesOfEngagement: event.target.value }))} required /></label><div className="form-grid"><label>{text.authorizedTargets}<textarea value={targets} onChange={(event) => setTargets(event.target.value)} required /></label><label>{text.exclusions}<textarea value={exclusions} onChange={(event) => setExclusions(event.target.value)} /></label></div><div className="planning-milestones"><strong>{text.milestones}</strong><div className="form-grid"><label>{text.milestoneTitle}<input value={milestoneTitle} onChange={(event) => setMilestoneTitle(event.target.value)} /></label><label>{text.date}<input type="date" value={milestoneDueOn} onChange={(event) => setMilestoneDueOn(event.target.value)} /></label></div><button type="button" onClick={addMilestone}>{text.addMilestone}</button><ul>{input.milestones.map((milestone, index) => <li key={`${milestone.title}-${milestone.dueOn}`}><span>{milestone.title} · {milestone.dueOn}</span><button type="button" onClick={() => setInput((current) => ({ ...current, milestones: current.milestones.filter((_, itemIndex) => itemIndex !== index) }))}>{text.remove}</button></li>)}</ul></div><button type="submit">{plan ? text.saveScopeVersion : text.createPlan}</button></fieldset></form>{plan && <section className="planning-record"><h2>{text.currentRecord}</h2><dl><div><dt>{text.status}</dt><dd>{plan.status}</dd></div><div><dt>{text.scopeVersion}</dt><dd>{plan.scope.versionNumber}</dd></div><div><dt>{text.methodologies}</dt><dd>{text.immutableChecklistAssigned}</dd></div><div><dt>{text.team}</dt><dd>{text.projectLeadFallback}</dd></div></dl>{plan.status === "draft" && <button type="button" disabled={busy} onClick={() => void advance("active")}>{text.activateProject}</button>}{plan.status === "active" && <button type="button" disabled={busy} onClick={() => void advance("closed")}>{text.closeProject}</button>}</section>}</main>;
}
