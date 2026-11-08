import { useId, useState } from 'react';
import { api } from './api';
import type { ServerProviderProps } from './config-page';
import { m } from './messages';
import { am } from './ai-provider-messages';
// Its stylesheet loads with it (a link, as the console's CSP allows), so the first bundle carries neither the
// styles nor a stylesheet loader.
import stylesheet from './ai-provider.css?url';
import { aiModelMatches, aiServiceKind, aiServiceOf, aiServices, aiServiceURL } from '../lib/ai-services';
import type { AIService } from '../lib/ai-services';

// The features a server provider's models are set for, in the order ai.server.models writes them.
const slots = ['triage', 'draft', 'chat', 'intake'] as const;
type Slot = (typeof slots)[number];
type Models = Record<Slot, string[]>;

// parseModels reads ai.server.models (triage=a,draft=b,intake=c|d); a feature left out offers nothing.
export function parseModels(value: string): Models {
  const models: Models = { triage: [], draft: [], chat: [], intake: [] };
  for (const entry of value.split(',')) {
    const [slot, list] = entry.split('=', 2).map((part) => part.trim());
    if (list && (slots as readonly string[]).includes(slot)) models[slot as Slot] = list.split('|').map((model) => model.trim()).filter(Boolean);
  }
  return models;
}

export function writeModels(models: Models): string {
  return slots.filter((slot) => models[slot].length > 0).map((slot) => `${slot}=${models[slot].join('|')}`).join(',');
}

// One model for every feature is the usual case; anything else is chosen per feature.
function sameModel(models: Models): string | null {
  const first = models.triage[0] ?? '';
  return first !== '' && slots.every((slot) => models[slot].length === 1 && models[slot][0] === first) ? first : null;
}

type ModelList = { state: 'idle' } | { state: 'loading' } | { state: 'loaded'; models: string[]; forKey: string } | { state: 'failed'; message: string };

// Above this many listed models, the selects get a search box.
const searchFrom = 8;

// ServerProvider is the server provider's part of AI & pricing: on or off, which service (as a workspace adds
// a provider), its name, key and address, and the model of each feature, picked from what the key may use.
// Off keeps every other value, so turning it on again needs nothing typed.
export default function ServerProvider({ byKey, draft, setDraft, errors, groupOf, SettingRow, change, currentValue }: ServerProviderProps) {
  const id = useId();
  const provider = byKey.get('ai.server.provider');
  const baseSetting = byKey.get('ai.server.base_url');
  const modelsSetting = byKey.get('ai.server.models');
  const label = byKey.get('ai.server.label');
  const key = byKey.get('ai.server.api_key');
  const [list, setList] = useState<ModelList>({ state: 'idle' });
  const [query, setQuery] = useState('');
  const [perFeature, setPerFeature] = useState<boolean | null>(null);
  const [typing, setTyping] = useState(false);
  if (!provider || !baseSetting || !modelsSetting) return null;
  const kind = currentValue(provider, draft);
  const base = currentValue(baseSetting, draft);
  const on = kind !== '';
  const service = aiServiceOf(kind, base);
  const editable = provider.editable && !provider.locked;
  const models = parseModels(currentValue(modelsSetting, draft));
  const uniform = sameModel(models);
  const showPerFeature = perFeature ?? (uniform === null && slots.some((slot) => models[slot].length > 0));
  // A list loaded for another service, address or key is stale.
  const listKey = JSON.stringify([kind, base, draft.secrets['ai.server.api_key'] ?? '', draft.unset.includes('ai.server.api_key')]);
  const loaded = list.state === 'loaded' && list.forKey === listKey ? list.models : null;
  const current = [...new Set(slots.flatMap((slot) => models[slot]))];
  const offered = [...new Set([...(loaded ?? []), ...current])];
  const shown = offered.filter((model) => aiModelMatches(model, query));
  const setModels = (next: Models) => change(setDraft, modelsSetting, writeModels(next));
  const pickService = (next: AIService) => {
    change(setDraft, provider, aiServiceKind(next));
    // A self-hosted server keeps an address typed for it; the services fill in their own.
    const address = aiServiceURL[next] ?? (next === 'self_hosted' && service === 'self_hosted' ? base : '');
    change(setDraft, baseSetting, address);
  };
  const load = async () => {
    setList({ state: 'loading' });
    try {
      const result = await api.check({ set: draft.set, unset: draft.unset, secrets: draft.secrets, test: 'ai_models' });
      if (result.test?.ok && result.test.models) setList({ state: 'loaded', models: result.test.models, forKey: listKey });
      else setList({ state: 'failed', message: result.test?.message ?? Object.values(result.errors).join(' ') });
    } catch (error) {
      setList({ state: 'failed', message: error instanceof Error ? error.message : m.nothingSaved });
    }
  };
  const options = (picked: string | undefined, empty: string) => <>
    <option value="">{empty}</option>
    {picked && !shown.includes(picked) && <option value={picked}>{picked}</option>}
    {shown.map((model) => <option key={model} value={model}>{model}</option>)}
  </>;
  const modelsError = errors['ai.server.models'];
  return <>
    <link rel="stylesheet" href={stylesheet} />
    <div className="setting field">
      <span className="field-label" id={`${id}-on-label`}>{am.serverProviderOn}</span>
      <code className="setting-key">ai.server.provider</code>
      <label className="switch"><input id={`${id}-on`} type="checkbox" role="switch" checked={on} disabled={!editable} aria-labelledby={`${id}-on-label`}
        onChange={(event) => change(setDraft, provider, event.target.checked ? aiServiceKind(aiServiceOf('', base)) : '')} /><span>{on ? m.on : m.off}</span></label>
      <span className="help field-note">{on ? am.serverProviderOnHelp : am.serverProviderOffHelp}</span>
      {errors['ai.server.provider'] && <span className="error-copy field-note" role="alert">{errors['ai.server.provider']}</span>}
    </div>
    {on && <>
      <fieldset className="setting field ops-ai-service">
        <legend className="field-label">{am.serverProviderService}</legend>
        <div className="ops-ai-kind">{aiServices.map((value) => <label key={value}>
          <input type="radio" name={`${id}-service`} value={value} checked={service === value} disabled={!editable} onChange={() => pickService(value)} />
          <strong>{am.aiService[value][0]}</strong><span>{am.aiService[value][1]}</span></label>)}</div>
        {service !== 'self_hosted' && errors['ai.server.base_url'] && <span className="error-copy field-note" role="alert">{errors['ai.server.base_url']}</span>}
      </fieldset>
      {label && <SettingRow setting={label} draft={draft} setDraft={setDraft} error={errors[label.key]} groupOf={groupOf} />}
      {service === 'self_hosted' && <SettingRow setting={baseSetting} draft={draft} setDraft={setDraft} error={errors[baseSetting.key]} groupOf={groupOf} />}
      {key && <SettingRow setting={key} draft={draft} setDraft={setDraft} error={errors[key.key]} groupOf={groupOf} />}
      {typing
        ? <>
          <SettingRow setting={modelsSetting} draft={draft} setDraft={setDraft} error={modelsError} groupOf={groupOf} />
          <button type="button" className="link-button ops-ai-switch" onClick={() => setTyping(false)}>{am.modelsPickInstead}</button>
        </>
        : <fieldset className={`setting field ops-ai-models${modelsError ? ' field-bad' : ''}`}>
          <legend className="field-label">{am.serverModels}</legend>
          <code className="setting-key">ai.server.models</code>
          <div className="ops-ai-models-head">
            {loaded && loaded.length > searchFrom
              ? <input type="search" aria-label={am.modelsSearch} placeholder={am.modelsSearch} value={query} onChange={(event) => setQuery(event.target.value)} />
              : <span className="help">{loaded ? am.modelsListed(loaded.length) : am.modelsBeforeLoad}</span>}
            <button type="button" disabled={!editable || list.state === 'loading'} onClick={() => void load()}>
              {list.state === 'loading' ? am.loadingModels : loaded ? am.reloadModels : am.loadModels}</button>
          </div>
          {list.state === 'failed' && <span className="error-copy field-note" role="alert">{list.message}</span>}
          {!showPerFeature
            ? <label className="ops-ai-model">
              <span>{am.modelEveryFeature}</span>
              <select id={`${id}-model`} value={uniform ?? ''} disabled={!editable || offered.length === 0}
                onChange={(event) => { const model = event.target.value; setModels(model ? { triage: [model], draft: [model], chat: [model], intake: [model] } : { triage: [], draft: [], chat: [], intake: [] }); }}>
                {options(uniform ?? undefined, offered.length === 0 ? am.loadModelsFirst : am.pickModel)}</select>
            </label>
            : <div className="ops-ai-per-feature">{slots.map((slot) => <label className="ops-ai-model" key={slot}>
              <span>{am.serverSlot[slot]}</span>
              <select value={models[slot][0] ?? ''} disabled={!editable}
                onChange={(event) => { const model = event.target.value; setModels({ ...models, [slot]: model ? [model, ...models[slot].slice(1).filter((other) => other !== model)] : [] }); }}>
                {options(models[slot][0], am.notOffered)}</select>
            </label>)}
              <label className="ops-ai-model">
                <span>{am.intakeFallback}</span>
                <select value={models.intake[1] ?? ''} disabled={!editable || models.intake.length === 0}
                  onChange={(event) => { const model = event.target.value; setModels({ ...models, intake: model && model !== models.intake[0] ? [models.intake[0], model] : models.intake.slice(0, 1) }); }}>
                  {options(models.intake[1], am.noFallback)}</select>
              </label>
            </div>}
          {modelsError && <span className="error-copy field-note" role="alert">{modelsError}</span>}
          <span className="help field-note">{am.serverModelsHelp}</span>
          <div className="button-row">
            <button type="button" className="link-button" onClick={() => setPerFeature(!showPerFeature)}>{showPerFeature ? am.oneModelForAll : am.modelPerFeature}</button>
            <button type="button" className="link-button" onClick={() => setTyping(true)}>{am.modelsTypeInstead}</button>
          </div>
        </fieldset>}
    </>}
  </>;
}
