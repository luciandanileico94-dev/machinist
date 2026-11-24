import React, { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { api, SignedOutError } from "./api.js";

const labels = { orchestrator: "Оркестратор", publish: "Публикация", spec: "Spec", build: "Build", "review-lead": "Ревью · лид", "review-security": "Ревью · security", "review-ux": "Ревью · UX", "review-ui": "Ревью · UI", "review-code": "Ревью · code", approver: "Approver", merged: "Влито", "needs-human": "Нужен человек" };
const efforts = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];
const providers = { claude: "Claude", codex: "Codex / GPT", deepseek: "DeepSeek" };

function Settings({ config, setConfig, onSave, saving, message }) {
  function updateStage(id, field, value) {
    setConfig(current => ({ ...current, stages: { ...current.stages, [id]: { ...current.stages[id], [field]: value } } }));
  }
  function updateProvider(id, provider) {
    setConfig(current => ({ ...current, stages: { ...current.stages, [id]: { ...current.stages[id], provider, model: current.catalog[provider][0] } } }));
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
      <div className="factory-stages">{Object.entries(config.stages).map(([id, stage]) => {
        const label = labels[id] || id;
        const available = config.catalog?.[stage.provider] || [];
        return <div className="factory-stage" key={id}>
          <div className="factory-stage-name">{label}<small>{id}</small></div>
          <label className="factory-field"><span>Провайдер</span><select className="field-control" aria-label={`${label} — провайдер`} value={stage.provider} onChange={event => updateProvider(id, event.target.value)}>{Object.entries(providers).filter(([key]) => key === stage.provider || config.catalog?.[key]?.length).map(([key, title]) => <option key={key} value={key}>{title}</option>)}</select></label>
          <label className="factory-field"><span>Модель</span><select className="field-control" aria-label={`${label} — модель`} value={stage.model} onChange={event => updateStage(id, "model", event.target.value)}>{[...new Set([stage.model, ...available])].map(model => <option key={model}>{model}</option>)}</select></label>
          <label className="factory-field"><span>Effort</span><select className="field-control" aria-label={`${label} — effort`} value={stage.effort} onChange={event => updateStage(id, "effort", event.target.value)}>{efforts.map(effort => <option key={effort}>{effort}</option>)}</select></label>
          {stage.fallback?.length > 0 && <p className="ticket-meta">Резерв {stage.fallback.join(" → ")}</p>}
        </div>;
      })}</div>
      <p className="page-note">Настройки сохраняются в etapy.yaml и применяются при следующем запуске этапа.</p>
    </section>
    <div className="page-actions"><Button type="button" disabled={saving} onClick={onSave}>{saving ? "Сохраняю…" : "Сохранить настройки"}</Button><span className="ticket-meta" role="status">{message}</span></div>
  </>;
}

export function FactoryPage({ onSignedOut }) {
  const [config, setConfig] = useState(null);
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let stopped = false;
    api("/api/v1/fabrika/settings").then(value => { if (!stopped) setConfig(value); }).catch(failure => {
      if (failure instanceof SignedOutError) onSignedOut();
      else if (!stopped) setMessage(`Настройки недоступны: ${failure.message}`);
    });
    return () => { stopped = true; };
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
    <header className="page-head"><div><h1>Фабрика</h1><p className="page-sub">Настройки агентов.</p></div></header>
    {config ? <Settings config={config} setConfig={setConfig} onSave={save} saving={saving} message={message} /> : <p className="page-note" role="status">{message || "Загружаю настройки…"}</p>}
  </div>;
}
