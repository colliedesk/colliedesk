import { useId, useRef, useState } from 'react';
import type { AIProvider, AIProviderInput, AISettings } from '../../api/ai';
import { Field } from '../../ui/dialog';
import { selfHostGuide } from '../../lib/self-host-guide';
import { aiModelMatches as modelMatches, aiServices as services, aiServiceURL as serviceURL } from '../../lib/ai-services';
import type { AIService as Service } from '../../lib/ai-services';
import { aiMessages as m, slotLabel } from './ai-messages';
import { AIDialog as Dialog } from './ai-level-dialogs';
import { KeyIcon } from './ai-icons';
import type { AIPageProps, FieldError } from './ai-page-props';

// The most models a provider keeps (contracts/openapi.yaml, AIProviderInput).
const maxProviderModels = 20;
// Above this many listed models, the list gets a search box.
const modelSearchFrom = 8;

// What Add provider offers (lib/ai-services): Anthropic, two OpenAI-compatible services whose address is
// filled in, and any other OpenAI-compatible server, whose address the person types.
const serviceText: Record<Service, [string, string]> = {
  anthropic: [m.kindAnthropic, m.kindAnthropicHelp], openai: [m.serviceOpenAI, m.serviceOpenAIHelp],
  openrouter: [m.serviceOpenRouter, m.serviceOpenRouterHelp], self_hosted: [m.serviceSelfHosted, m.serviceSelfHostedHelp],
};

type ModelList = { state: 'idle' } | { state: 'loading' } | { state: 'loaded'; models: string[] } | { state: 'typing' };

export function AddDialog({ settings, onClose, onAdd, onListModels }: { settings: AISettings; onClose: () => void; onAdd: AIPageProps['onAdd']; onListModels: AIPageProps['onListModels'] }) {
  const [service, setService] = useState<Service>('anthropic');
  const kind = service === 'anthropic' ? 'anthropic' : 'openai_compatible';
  const [label, setLabel] = useState('');
  const [typedURL, setTypedURL] = useState('');
  const baseURL = serviceURL[service] ?? typedURL;
  const [key, setKey] = useState('');
  // The models picked from the provider's list, in the order picked: the first is the default.
  const [picked, setPicked] = useState<string[]>([]);
  // The names typed when the provider keeps no list (or the person prefers to type).
  const [typed, setTyped] = useState('');
  const [list, setList] = useState<ModelList>({ state: 'idle' });
  // Each load is numbered; an answer for a kind, address or key changed since is dropped.
  const loads = useRef(0);
  const [error, setError] = useState<FieldError>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const keyOptional = service === 'self_hosted' && settings.allow_private_base_urls;
  const noKeys = !settings.can_store_keys && !keyOptional;
  const left = settings.provider_limit - settings.providers.length;
  const focus = (field: FieldError['field']) => document.getElementById(`${id}-${field}`)?.focus();
  const canLoad = (key.trim() !== '' || keyOptional) && (kind === 'anthropic' || baseURL.trim() !== '') && list.state !== 'loading';
  // asked is true when the person pressed Load: only then does a refusal take the focus to its field.
  const loadModels = async (asked: boolean) => {
    if (!canLoad) return;
    const load = ++loads.current;
    setList({ state: 'loading' });
    setError(undefined);
    const answer = await onListModels({ kind, ...(key.trim() ? { api_key: key.trim() } : {}), ...(kind === 'openai_compatible' ? { base_url: baseURL.trim() } : {}) });
    if (load !== loads.current) return;
    if (Array.isArray(answer)) {
      setList({ state: 'loaded', models: answer });
      // Claude's newest Sonnet is a sound, middle-priced default; the newest model overall is often the
      // dearest. Other lists are sorted by name, so nothing is ticked for the person.
      const kept = picked.filter((model) => answer.includes(model));
      const sonnet = kind === 'anthropic' ? answer.find((model) => model.startsWith('claude-sonnet-')) : undefined;
      setPicked(kept.length > 0 ? kept : sonnet ? [sonnet] : []);
      return;
    }
    // A provider that keeps no list: the names are typed instead, with the reason under the field.
    setList(answer.field === 'models' ? { state: 'typing' } : { state: 'idle' });
    setError(answer);
    if (asked && answer.field !== 'form') focus(answer.field);
  };
  // A new kind, address or key makes the loaded list stale, and an answer still on its way with it.
  const changed = (next: () => void) => {
    next();
    setError(undefined);
    if (list.state === 'loaded' || list.state === 'loading') { loads.current += 1; setList({ state: 'idle' }); }
  };
  const submit = async () => {
    // Only what is on screen is sent: models ticked in a list loaded for this kind, address and key, or the names typed.
    const models = list.state === 'typing' ? typed.split(',').map((model) => model.trim()).filter(Boolean) : list.state === 'loaded' ? picked : [];
    const refuse = (field: FieldError['field'], text: string) => { setError({ field, text }); focus(field); };
    if (!label.trim()) return refuse('label', m.labelMissing);
    if (kind === 'openai_compatible' && !baseURL.trim()) return refuse('base_url', m.baseURLMissing);
    if (!key.trim() && !keyOptional) return refuse('api_key', m.keyMissing);
    if (models.length === 0) return refuse('models', list.state === 'typing' ? m.modelsMissing : m.modelsPick);
    setBusy(true);
    setError(undefined);
    const input: AIProviderInput = { kind, label: label.trim(), models, ...(key.trim() ? { api_key: key.trim() } : {}), ...(kind === 'openai_compatible' ? { base_url: baseURL.trim() } : {}) };
    const refused = await onAdd(input);
    setBusy(false);
    if (!refused) { onClose(); return; }
    setError(refused);
    if (refused.field !== 'form') focus(refused.field);
  };
  const fieldError = (field: FieldError['field']) => (error?.field === field ? error.text : undefined);
  return (
    <Dialog title={m.addHeading} help={m.addHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><span className="ai-dialog-left">{left > 0 ? m.providersFoot(left) : ''}</span><button type="button" onClick={onClose}>{m.cancel}</button>
        <button type="submit" className="primary-action" disabled={busy || noKeys}>{busy ? m.checking : m.checkAndSave}</button></>}>
      {noKeys && <div className="notice notice-danger" role="alert"><span><strong>{m.noKeysNotice}</strong> {m.noKeysBody}{' '}
        <a href={selfHostGuide('environment-variables.md', 'variables')} target="_blank" rel="noopener noreferrer">{m.noKeysGuide}</a></span></div>}
      {error?.field === 'form' && <p role="alert" className="error-copy">{error.text}</p>}
      <fieldset className="ai-kind"><legend className="sr-only">{m.columnProvider}</legend>
        {services.map((value) => <label key={value}>
          <input type="radio" name={`${id}-kind`} value={value} checked={service === value} onChange={() => changed(() => setService(value))} />
          <strong>{serviceText[value][0]}</strong><span>{serviceText[value][1]}</span></label>)}
      </fieldset>
      <Field label={m.labelField} id={`${id}-label`} error={fieldError('label')}>{(attrs) => <input {...attrs} value={label} maxLength={80} onChange={(event) => setLabel(event.target.value)} autoFocus />}</Field>
      {service === 'self_hosted' && <Field label={m.baseURLField} id={`${id}-base_url`} error={fieldError('base_url')} help={m.baseURLHelp}>
        {(attrs) => <input {...attrs} type="url" inputMode="url" value={typedURL} placeholder="http://localhost:11434/v1" onChange={(event) => changed(() => setTypedURL(event.target.value))} />}</Field>}
      {service !== 'self_hosted' && fieldError('base_url') && <p role="alert" className="error-copy">{fieldError('base_url')}</p>}
      <Field label={keyOptional ? m.keyFieldOptional : m.keyField} id={`${id}-api_key`} error={fieldError('api_key')} help={m.keyHelpStored}>
        {(attrs) => <input {...attrs} type="password" autoComplete="off" value={key} placeholder={keyOptional ? m.keyOptionalPlaceholder : undefined}
          onChange={(event) => changed(() => setKey(event.target.value))}
          // Leaving the key with nothing loaded yet loads the list, so the next thing to do is pick.
          onBlur={() => { if (list.state === 'idle' && key.trim() !== '') void loadModels(false); }} />}</Field>
      {list.state === 'typing'
        ? <Field label={m.modelsTyped} id={`${id}-models`} error={fieldError('models')} help={m.modelsHelp}>
            {(attrs) => <input {...attrs} value={typed} placeholder={kind === 'anthropic' ? 'claude-sonnet-5-5, claude-haiku-5-5' : 'llama3.1, qwen3'}
              onChange={(event) => { setTyped(event.target.value); if (error?.field === 'models') setError(undefined); }} />}</Field>
        : <ModelPicker id={`${id}-models`} list={list} picked={picked} onPick={setPicked} error={fieldError('models')} canLoad={canLoad}
            needsAddress={service === 'self_hosted'} onLoad={() => void loadModels(true)} />}
      {list.state === 'typing'
        ? <button type="button" className="link-button ai-models-switch" disabled={!canLoad} onClick={() => void loadModels(true)}>{m.modelsPickInstead}</button>
        : <button type="button" className="link-button ai-models-switch" onClick={() => { setError(undefined); setTyped(picked.join(', ')); setList({ state: 'typing' }); }}>{m.modelsTypeInstead}</button>}
    </Dialog>
  );
}

// ModelPicker lists the models the provider offers this key, to tick instead of typing ids. The picked
// ones stay on top in the order picked; the first is the default.
function ModelPicker({ id, list, picked, onPick, error, canLoad, needsAddress, onLoad }: { id: string; list: ModelList; picked: string[]; onPick: (models: string[]) => void;
  error?: string; canLoad: boolean; needsAddress: boolean; onLoad: () => void }) {
  const [query, setQuery] = useState('');
  const note = `${id}-note`;
  const models = list.state === 'loaded' ? list.models : [];
  const shown = models.filter((model) => modelMatches(model, query));
  const full = picked.length >= maxProviderModels;
  const toggle = (model: string) => onPick(picked.includes(model) ? picked.filter((item) => item !== model) : full ? picked : [...picked, model]);
  return (
    <fieldset className={`field ai-models${error ? ' field-bad' : ''}`} aria-describedby={note}>
      <legend className="field-label">{m.modelsField}</legend>
      <div className="ai-models-head">
        {list.state === 'loaded' && models.length > modelSearchFrom
          ? <input id={id} type="search" className="ai-models-search" aria-label={m.modelsSearch} placeholder={m.modelsSearch} value={query} onChange={(event) => setQuery(event.target.value)} />
          : <span className="help">{list.state === 'loaded' ? '' : needsAddress ? m.modelsBeforeLoadAddress : m.modelsBeforeLoad}</span>}
        <button type="button" id={list.state === 'loaded' && models.length > modelSearchFrom ? undefined : id} disabled={!canLoad} onClick={onLoad}>
          {list.state === 'loading' ? m.loadingModels : list.state === 'loaded' ? m.reloadModels : m.loadModels}</button>
      </div>
      {list.state === 'loaded' && <ul className="ai-models-list">
        {models.length === 0 && <li className="ai-models-empty">{m.modelsNone}</li>}
        {models.length > 0 && shown.length === 0 && <li className="ai-models-empty">{m.modelsNoMatch}</li>}
        {/* The picked models first, in the order picked, then the rest as the provider lists them. */}
        {[...picked.filter((model) => shown.includes(model)), ...shown.filter((model) => !picked.includes(model))].map((model) => (
          <li key={model}><label>
            <input type="checkbox" checked={picked.includes(model)} disabled={!picked.includes(model) && full} onChange={() => toggle(model)} />
            <span className="ai-mono" title={model}>{model}</span>{picked[0] === model && <span className="ai-models-default">{m.modelsDefault}</span>}
          </label></li>
        ))}
      </ul>}
      {error ? <span className="error-copy field-note" id={note} role="alert">{error}</span>
        : <span className="help field-note" id={note}>{full ? m.modelsLimit(maxProviderModels) : list.state === 'loaded' ? m.modelsPicked(picked.length, maxProviderModels) : ''}</span>}
    </fieldset>
  );
}

export function KeyDialog({ provider, onClose, onSetKey }: { provider: AIProvider; onClose: () => void; onSetKey: AIPageProps['onSetKey'] }) {
  const [key, setKey] = useState('');
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const id = useId();
  const submit = async () => {
    if (!key.trim()) { setError(m.keyMissing); return; }
    setBusy(true);
    const refused = await onSetKey(provider.id, key.trim());
    setBusy(false);
    if (refused) setError(provider.key_hint ? `${refused.text} ${m.oldKeyStays(provider.key_hint)}` : refused.text); else onClose();
  };
  return (
    <Dialog title={m.rotateHeading(provider.label)} help={m.rotateHelp} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={busy}>{busy ? m.checking : m.checkAndReplace}</button></>}>
      {provider.key_hint && <p className="ai-current"><KeyIcon />{m.currentKey} <span className="ai-mono">••••{provider.key_hint}</span> ·
        {provider.check_status === 'ok' ? <span className="status ai-st-ok">{m.working}</span> : <span className="status ai-st-err">{m.notWorking}</span>}</p>}
      {provider.base_url && <p className="ai-address"><span>{m.addressStays(provider.base_url)}</span></p>}
      <Field label={m.newKey} id={`${id}-key`} error={error}>{(attrs) => <input {...attrs} type="password" autoComplete="off" value={key} onChange={(event) => setKey(event.target.value)} autoFocus />}</Field>
    </Dialog>
  );
}

export function DeleteDialog({ provider, settings, onClose, onRemove }: { provider: AIProvider; settings: AISettings; onClose: () => void; onRemove: AIPageProps['onRemove'] }) {
  const automatic = settings.slots.some((slot) => slot.automatic && provider.used_by.includes(slot.slot));
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    const refused = await onRemove(provider.id);
    setBusy(false);
    if (refused) setError(refused.text); else onClose();
  };
  return (
    <Dialog alert title={m.deleteHeading(provider.label)} onClose={onClose} onSubmit={() => void submit()}
      footer={<><button type="button" onClick={onClose} autoFocus>{m.cancel}</button><button type="submit" className="ai-danger-solid" disabled={busy}>{m.deleteConfirm}</button></>}>
      {error && <p role="alert" className="error-copy">{error}</p>}
      {provider.used_by.length > 0 ? <><p>{m.deleteUsed}</p><ul className="ai-delete-list">{provider.used_by.map((slot) => <li key={slot}><b>{slotLabel(slot)}</b></li>)}</ul></>
        : <p>{m.deleteUnused}</p>}
      <p>{m.deleteBody}</p>
      {automatic && <p>{m.deleteAutomatic}</p>}
    </Dialog>
  );
}
