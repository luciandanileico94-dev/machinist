import React, { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { api, SignedOutError } from "./api.js";
import { Source } from "./source-mark.jsx";
import { sourceLink } from "./jobs.js";
import { factorySettingsHref } from "./routes.js";

const labels = { orchestrator: "Оркестратор", publish: "Публикация", spec: "Spec", build: "Build", "review-lead": "Ревью · лид", "review-security": "Ревью · security", "review-ux": "Ревью · UX", "review-ui": "Ревью · UI", "review-code": "Ревью · code", approver: "Approver", merged: "Влито", "needs-human": "Нужен человек" };
const lanes = [["spec", "Spec"], ["build", "Build"], ["review", "Ревью"], ["approver", "Approver"], ["merged", "Влито"], ["needs-human", "Нужен человек"]];
const reviewers = ["лид", "security", "UX", "UI", "code"];
const efforts = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];
const providers = { claude: "Claude", codex: "Codex / GPT", deepseek: "DeepSeek" };

export function effortsFor(catalog, provider, model) {
  return catalog?.efforts?.[provider]?.[model] || catalog?.efforts?.[provider] || ["low", "medium", "high"];
}

export function parseFallback(value) {
  const match = /^(claude|codex|deepseek)\/(.+):([^:]+)$/.exec(value || "");
  return match ? { provider: match[1], model: match[2], effort: match[3] } : null;
}

export function formatFallback(item) {
  return `${item.provider}/${item.model}:${item.effort}`;
}

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
    setConfig(current => {
      const stage = { ...current.stages[id], [field]: value };
      if (field === "model" && !effortsFor(current.catalog, stage.provider, value).includes(stage.effort)) stage.effort = effortsFor(current.catalog, stage.provider, value)[0];
      return { ...current, stages: { ...current.stages, [id]: stage } };
    });
  }
  function updateProvider(id, provider) {
    setConfig(current => {
      const model = current.catalog[provider][0];
      const available = effortsFor(current.catalog, provider, model);
      const stage = { ...current.stages[id], provider, model };
      if (!available.includes(stage.effort)) stage.effort = available[0];
      return { ...current, stages: { ...current.stages, [id]: stage } };
    });
  }
  function updateFallback(id, index, field, value) {
    const items = (config.stages[id].fallback || []).map(parseFallback).filter(Boolean);
    const item = items[index];
    if (!item) return;
    if (field === "provider") Object.assign(item, { provider: value, model: config.catalog?.[value]?.[0] || "", effort: effortsFor(config.catalog, value, config.catalog?.[value]?.[0])[0] || "medium" });
    else if (field === "model") Object.assign(item, { model: value, effort: effortsFor(config.catalog, item.provider, value)[0] || "medium" });
    else item.effort = value;
    updateStage(id, "fallback", items.map(formatFallback));
  }
  function moveFallback(id, index, step) {
    const items = (config.stages[id].fallback || []).slice();
    [items[index], items[index + step]] = [items[index + step], items[index]];
    updateStage(id, "fallback", items);
  }
  return <>
    <section className="panel factory-section" aria-labelledby="weights-heading">
      <h2 id="weights-heading" className="chart-title">Статус подписок</h2>
      <div className="factory-subscriptions">{(config.subscriptionStatus || []).map(item => <article className="factory-subscription" key={item.id}>
        <header><strong>{item.name}</strong><span className={`pill ${item.status === "good" ? "pill-success" : item.status === "limit" ? "pill-warning" : "pill-danger"}`}>{item.statusLabel}</span></header>
        {["5h", "7d"].map(window => <div className="factory-limit" key={window}>
          <div className="factory-limit-head"><span>{window === "5h" ? "Окно 5 ч" : "Неделя"}</span><b>{item[`${window}Utilization`] == null ? "Нет данных" : `${Math.round(item[`${window}Utilization`] * 100)}%`}</b></div>
          <div className="factory-progress" role="progressbar" aria-label={`${item.name}: ${window === "5h" ? "окно 5 ч" : "неделя"}`} aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(Math.min(1, item[`${window}Utilization`] || 0) * 100)} data-tone={item[`${window}Utilization`] >= (item[`${window}Threshold`] || 0.9) ? "danger" : item[`${window}Utilization`] >= (item[`${window}Threshold`] || 0.9) * 0.8 ? "warning" : "good"}><span style={{ width: `${Math.min(100, Math.round((item[`${window}Utilization`] || 0) * 100))}%` }} /></div>
          <small>{item[`${window}Reset`] ? `Сброс ${new Intl.DateTimeFormat("ru-RU", { timeZone: "Europe/Chisinau", dateStyle: window === "5h" ? undefined : "medium", timeStyle: "short" }).format(new Date(item[`${window}Reset`] * 1000))}` : "Время сброса неизвестно"}</small>
        </div>)}
        <p className="ticket-meta">Запросы фабрики: {item.requests ?? 0}</p>
        {item.updatedAt && <p className="ticket-meta">Данные Codex на {new Intl.DateTimeFormat("ru-RU", { timeZone: "Europe/Chisinau", hour: "2-digit", minute: "2-digit" }).format(new Date(item.updatedAt))}</p>}
      </article>)}</div>
    </section>
    <section className="panel factory-section" aria-labelledby="subscription-rules-heading">
      <h2 id="subscription-rules-heading" className="chart-title">Правила выбора подписки</h2>
      <label className="factory-rule-stop"><span>Стоп основной, неделя</span><input aria-label="Стоп основной, неделя" className="field-control" type="number" min="0" max="100" step="1" value={Math.round(config.rules.primaryStop * 100)} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, primaryStop: Number(event.target.value) / 100 } }))} />%</label>
      <div className="factory-rules">{Object.entries(config.rules.subscriptions).map(([id, rule]) => <article className="factory-rule" key={id}>
        <label className="factory-rule-enabled"><input type="checkbox" checked={rule.enabled} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, enabled: event.target.checked } } } }))} /><span>{rule.label}</span></label>
        <label><span>Порядок</span><input className="field-control" type="number" min="1" max="4" value={rule.order} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, order: Number(event.target.value) } } } }))} /></label>
        <label><span>Брать до % недели</span><input className="field-control" type="number" min="0" max="100" value={Math.round(rule.weeklyMax * 100)} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, weeklyMax: Number(event.target.value) / 100 } } } }))} /></label>
        <label><span>За N ч добирать</span><input className="field-control" type="number" min="0" max="168" value={rule.topUpHours} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, topUpHours: Number(event.target.value) } } } }))} /></label>
        <label><span>До % недели</span><input className="field-control" type="number" min="0" max="100" value={Math.round(rule.topUpTo * 100)} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, topUpTo: Number(event.target.value) / 100 } } } }))} /></label>
        <label><span>5 ч: не выше %</span><input className="field-control" type="number" min="0" max="100" value={Math.round(rule.max5h * 100)} onChange={event => setConfig(current => ({ ...current, rules: { ...current.rules, subscriptions: { ...current.rules.subscriptions, [id]: { ...rule, max5h: Number(event.target.value) / 100 } } } }))} /></label>
        {rule.temporary && <p className="ticket-meta">{rule.note}</p>}
      </article>)}</div>
    </section>
    <section className="panel factory-section" aria-labelledby="stages-heading">
      <h2 id="stages-heading" className="chart-title">Модель и effort этапов</h2>
      <div className="factory-stages">{Object.entries(config.stages).map(([id, stage]) => {
        const label = labels[id] || id;
        const available = config.catalog?.[stage.provider] || [];
        return <div className="factory-stage" key={id}>
          <div className="factory-stage-name">{label}<small>{id}</small></div>
          <label className="factory-field"><span>Провайдер</span><select className="field-control" aria-label={`${label} — провайдер`} value={stage.provider} onChange={event => updateProvider(id, event.target.value)}>{Object.entries(providers).filter(([key]) => key === stage.provider || config.catalog?.[key]?.length).map(([key, title]) => <option key={key} value={key}>{title}</option>)}</select></label>
          <label className="factory-field"><span>Модель</span><select className="field-control" aria-label={`${label} — модель`} value={stage.model} onChange={event => updateStage(id, "model", event.target.value)}>{[...new Set([stage.model, ...available])].map(model => <option key={model}>{model}</option>)}</select></label>
          <label className="factory-field"><span>Effort</span><select className="field-control" aria-label={`${label} — effort`} value={stage.effort} onChange={event => updateStage(id, "effort", event.target.value)}>{effortsFor(config.catalog, stage.provider, stage.model).map(effort => <option key={effort}>{effort}</option>)}</select></label>
          <div className="factory-fallbacks"><h3>Резервные модели</h3>
            {(stage.fallback || []).map((value, index) => {
              const fallback = parseFallback(value);
              if (!fallback) return null;
              const models = config.catalog?.[fallback.provider] || [];
              return <div className="factory-fallback" key={`${id}-${index}`}>
                <label><span>Провайдер</span><select className="field-control" aria-label={`${label}: резерв ${index + 1}, провайдер`} value={fallback.provider} onChange={event => updateFallback(id, index, "provider", event.target.value)}>{Object.entries(providers).filter(([key]) => config.catalog?.[key]?.length).map(([key, title]) => <option key={key} value={key}>{title}</option>)}</select></label>
                <label><span>Модель</span><select className="field-control" aria-label={`${label}: резерв ${index + 1}, модель`} value={fallback.model} onChange={event => updateFallback(id, index, "model", event.target.value)}>{[...new Set([fallback.model, ...models])].map(model => <option key={model}>{model}</option>)}</select></label>
                <label><span>Effort</span><select className="field-control" aria-label={`${label}: резерв ${index + 1}, effort`} value={fallback.effort} onChange={event => updateFallback(id, index, "effort", event.target.value)}>{effortsFor(config.catalog, fallback.provider, fallback.model).map(effort => <option key={effort}>{effort}</option>)}</select></label>
                <div className="factory-fallback-actions"><Button type="button" variant="outline" aria-label={`Поднять резерв ${index + 1}: ${label}`} disabled={index === 0} onClick={() => moveFallback(id, index, -1)}>↑</Button><Button type="button" variant="outline" aria-label={`Опустить резерв ${index + 1}: ${label}`} disabled={index === stage.fallback.length - 1} onClick={() => moveFallback(id, index, 1)}>↓</Button><Button type="button" variant="outline" aria-label={`Удалить резерв ${index + 1}: ${label}`} onClick={() => updateStage(id, "fallback", stage.fallback.filter((_, at) => at !== index))}>Убрать</Button></div>
              </div>;
            })}
            <Button type="button" variant="outline" onClick={() => {
              const provider = Object.keys(providers).find(key => config.catalog?.[key]?.length);
              const model = config.catalog?.[provider]?.[0];
              updateStage(id, "fallback", [...(stage.fallback || []), formatFallback({ provider, model, effort: effortsFor(config.catalog, provider, model)[0] || "medium" })]);
            }}>Добавить резерв</Button>
          </div>
        </div>;
      })}</div>
      <p className="page-note">Модели и цепочки сохраняются в etapy.yaml; правила выбора сразу применяются к новым сессиям и весам роутера.</p>
    </section>
    <div className="page-actions"><Button type="button" disabled={saving} onClick={onSave}>{saving ? "Сохраняю…" : "Сохранить настройки"}</Button><span className="ticket-meta" role="status">{message}</span></div>
  </>;
}

export function FactoryPage({ tab = "board", onSignedOut }) {
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
      setMessage("Сохранено в etapy.yaml и правилах роутера.");
    } catch (failure) {
      if (failure instanceof SignedOutError) onSignedOut();
      else setMessage(`Ошибка: ${failure.message}`);
    } finally { setSaving(false); }
  }

  return <div className="page factory-page">
    <header className="page-head"><div><h1>Фабрика</h1><p className="page-sub">Доска этапов и настройки агентов.</p></div></header>
    <div className="factory-tabs" role="group" aria-label="Разделы фабрики">
      <Button type="button" variant={tab === "board" ? "default" : "outline"} aria-pressed={tab === "board"} onClick={() => { window.location.hash = "#/factory"; }}>Доска</Button>
      <Button type="button" variant={tab === "settings" ? "default" : "outline"} aria-pressed={tab === "settings"} onClick={() => { window.location.hash = factorySettingsHref; }}>Настройки</Button>
      {tab === "board" && <span className="ticket-meta" role="status">Обновление каждые 10 секунд</span>}
    </div>
    {error && tab === "board" && <p className="form-error" role="alert">Machinist API: {error}</p>}
    {tab === "board" && <Board jobs={jobs} />}
    {tab === "settings" && (config ? <Settings config={config} setConfig={setConfig} onSave={save} saving={saving} message={message} /> : <p className="page-note" role="status">{message || "Загружаю настройки…"}</p>)}
  </div>;
}
