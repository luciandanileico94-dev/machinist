import React, { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { api, SignedOutError } from "./api.js";
import { Source } from "./source-mark.jsx";
import { sourceLink } from "./jobs.js";

const stages = ["spec", "build", "review-lead", "review-security", "review-ux", "review-ui", "review-code", "approver"];
const labels = { spec: "Spec", build: "Build", "review-lead": "Ревью · лид", "review-security": "Ревью · security", "review-ux": "Ревью · UX", "review-ui": "Ревью · UI", "review-code": "Ревью · code", approver: "Approver", merged: "Влито", "needs-human": "Нужен человек" };
const lanes = [["spec", "Spec"], ["build", "Build"], ["review", "Ревью"], ["approver", "Approver"], ["merged", "Влито"], ["needs-human", "Нужен человек"]];
const reviewers = ["лид", "security", "UX", "UI", "code"];
const efforts = ["low", "medium", "high", "xhigh", "max"];

function laneOf(job) {
  const state = String(job.state || "").toLowerCase();
  const pr = String(job.pr_state || "").toLowerCase();
  const step = String(job.current_step || "").toLowerCase();
  if (pr === "merged") return "merged";
  if (["failed", "cancelled"].includes(state) || (state === "done" && pr !== "merged")) return "needs-human";
  if (step.includes("approv") || step.includes("publish") || step === "merge") return "approver";
  if (step.includes("review")) return "review";
  if (step.includes("spec")) return "spec";
  if (step.includes("build") || step.includes("orchestrat") || state === "running") return "build";
  return "spec";
}

function JobCard({ job, lane }) {
  return <li className="board-card factory-card" data-state={job.state === "running" ? "running" : lane === "needs-human" ? "needs_you" : lane === "merged" ? "done" : "queued"}>
    <strong className="factory-card-title">{job.title || job.key || job.id}</strong>
    <div className="board-card-meta"><code>{job.key || job.id}</code><span>{[job.current_step || job.state, job.preset].filter(Boolean).join(" · ")}</span></div>
    {lane === "review" && <div className="factory-reviews">{reviewers.map(name => <span className="pill" key={name}>{name} · Machinist</span>)}</div>}
    {sourceLink(job) && <a className="factory-link" href={sourceLink(job)} target="_blank" rel="noreferrer"><Source source={job.source} /> · Источник ↗</a>}
    {job.pr_number && job.vars?.repo && <a className="factory-link" href={`https://github.com/${job.vars.repo}/pull/${job.pr_number}`} target="_blank" rel="noreferrer">PR #{job.pr_number} · {job.pr_state || ""}</a>}
  </li>;
}

function Board({ jobs }) {
  return <div className="board factory-board">
    {lanes.map(([key, title]) => {
      const items = jobs.filter(job => laneOf(job) === key);
      return <section className="board-column" key={key} aria-label={title}>
        <h2 className="board-column-head"><span className="board-column-name"><span className="board-dot" data-column={key === "merged" ? "done" : key === "build" ? "running" : "queued"} aria-hidden="true" />{title}</span><span className="board-count">{items.length}</span></h2>
        {items.length ? <ul className="board-cards">{items.map(job => <JobCard key={job.id} job={job} lane={key} />)}</ul> : <p className="board-empty">Пока пусто</p>}
      </section>;
    })}
  </div>;
}

function Settings({ config, setConfig, onSave, saving, message }) {
  function updateStage(id, field, value) {
    setConfig(current => ({ ...current, stages: { ...current.stages, [id]: { ...current.stages[id], [field]: value } } }));
  }
  function updateProvider(id, provider) {
    updateStage(id, "provider", provider);
    updateStage(id, "model", provider === "claude" ? "claude-opus-5-5" : "gpt-6.2-sol");
  }
  return <>
    <section className="panel factory-section" aria-labelledby="weights-heading">
      <h2 id="weights-heading" className="chart-title">Вес подписок роутера</h2>
      <p className="page-note">Вес 0 исключает подписку из обычного распределения запросов.</p>
      <div className="factory-weights">{Object.entries(config.router.subscriptions).map(([id, subscription]) => <label className="factory-field" key={id}>
        <span>{id}</span><input className="field-control" aria-label={`Вес ${id}`} type="number" min="0" max="1000000" step="1" value={subscription.weight} onChange={event => setConfig(current => ({ ...current, router: { ...current.router, subscriptions: { ...current.router.subscriptions, [id]: { weight: Number(event.target.value) } } } }))} />
      </label>)}</div>
    </section>
    <section className="panel factory-section" aria-labelledby="stages-heading">
      <h2 id="stages-heading" className="chart-title">Модель и effort этапов</h2>
      <div className="factory-stages">{stages.filter(id => config.stages[id]).map(id => {
        const stage = config.stages[id];
        const available = stage.provider === "claude" ? ["claude-opus-5-5", "claude-sonnet-5-5"] : ["gpt-6.2-sol"];
        return <div className="factory-stage" key={id}>
          <div className="factory-stage-name">{labels[id]}<small>{id}</small></div>
          <label className="factory-field"><span>Провайдер</span><select className="field-control" aria-label={`${labels[id]} — провайдер`} value={stage.provider} onChange={event => updateProvider(id, event.target.value)}><option value="claude">Claude</option><option value="codex">Codex / GPT</option></select></label>
          <label className="factory-field"><span>Модель</span><select className="field-control" aria-label={`${labels[id]} — модель`} value={stage.model} onChange={event => updateStage(id, "model", event.target.value)}>{[...new Set([stage.model, ...available])].map(model => <option key={model}>{model}</option>)}</select></label>
          <label className="factory-field"><span>Effort</span><select className="field-control" aria-label={`${labels[id]} — effort`} value={stage.effort} onChange={event => updateStage(id, "effort", event.target.value)}>{efforts.map(effort => <option key={effort}>{effort}</option>)}</select></label>
        </div>;
      })}</div>
      <p className="page-note">Настройки сохраняются в etapy.yaml и применяются при следующем запуске этапа.</p>
    </section>
    <div className="page-actions"><Button type="button" disabled={saving} onClick={onSave}>{saving ? "Сохраняю…" : "Сохранить настройки"}</Button><span className="ticket-meta" role="status">{message}</span></div>
  </>;
}

export function FactoryPage({ onSignedOut }) {
  const [tab, setTab] = useState("board");
  const [jobs, setJobs] = useState([]);
  const [config, setConfig] = useState(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let stopped = false;
    let timer;
    async function load() {
      try {
        const response = await api("/machinist/jobs");
        if (!stopped) { setJobs(response.jobs || []); setError(""); }
      } catch (failure) {
        if (failure instanceof SignedOutError) onSignedOut();
        else if (!stopped) setError(failure.message);
      }
      if (!stopped) timer = window.setTimeout(load, 10_000);
    }
    load();
    api("/api/v1/fabrika/settings").then(value => { if (!stopped) setConfig(value); }).catch(failure => {
      if (failure instanceof SignedOutError) onSignedOut();
      else if (!stopped) setMessage(`Настройки недоступны: ${failure.message}`);
    });
    return () => { stopped = true; window.clearTimeout(timer); };
  }, [onSignedOut]);

  async function save() {
    setSaving(true); setMessage("");
    try {
      setConfig(await api("/api/v1/fabrika/settings", { method: "PUT", body: config }));
      setMessage("Сохранено в etapy.yaml и роутер.");
    } catch (failure) {
      if (failure instanceof SignedOutError) onSignedOut();
      else setMessage(`Ошибка: ${failure.message}`);
    } finally { setSaving(false); }
  }

  return <div className="page factory-page">
    <header className="page-head"><div><h1>Фабрика</h1><p className="page-sub">Доска этапов и настройки агентов.</p></div></header>
    <div className="factory-tabs" role="group" aria-label="Разделы фабрики">
      <Button type="button" variant={tab === "board" ? "default" : "outline"} aria-pressed={tab === "board"} onClick={() => setTab("board")}>Доска</Button>
      <Button type="button" variant={tab === "settings" ? "default" : "outline"} aria-pressed={tab === "settings"} onClick={() => setTab("settings")}>Настройки</Button>
      {tab === "board" && <span className="ticket-meta" role="status">Обновление каждые 10 секунд</span>}
    </div>
    {error && tab === "board" && <p className="form-error" role="alert">Machinist API: {error}</p>}
    {tab === "board" && <Board jobs={jobs} />}
    {tab === "settings" && (config ? <Settings config={config} setConfig={setConfig} onSave={save} saving={saving} message={message} /> : <p className="page-note" role="status">{message || "Загружаю настройки…"}</p>)}
  </div>;
}
