import { useEffect, useId, useRef, useState, useSyncExternalStore } from 'react';
import type { KeyboardEvent } from 'react';
import { api } from './api';
import type { ServerProviderProps } from './config-page';
import type { Draft } from './guards';
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

function modelsInUse(models: Models): string[] {
  return [...new Set(slots.flatMap((slot) => models[slot]))];
}

// One model for every feature is the usual case; anything else is chosen per feature.
function sameModel(models: Models): string | null {
  const first = models.triage[0] ?? '';
  return first !== '' && slots.every((slot) => models[slot].length === 1 && models[slot][0] === first) ? first : null;
}

// A price as ai.server.prices writes it: model=input:output[:cache_read[:cache_write]], dollars per million tokens.
export interface Price { input: string; output: string; cacheRead: string; cacheWrite: string }
type Prices = Array<[string, Price]>;

export function parsePrices(value: string): Prices {
  const prices: Prices = [];
  for (const entry of value.split(',')) {
    const at = entry.indexOf('=');
    if (at < 1) continue;
    const [input = '', output = '', cacheRead = '', cacheWrite = ''] = entry.slice(at + 1).split(':').map((part) => part.trim());
    prices.push([entry.slice(0, at).trim(), { input, output, cacheRead, cacheWrite }]);
  }
  return prices;
}

export function writePrices(prices: Prices): string {
  return prices.map(([model, price]) => {
    const parts = [price.input, price.output, price.cacheRead, price.cacheWrite];
    while (parts.length > 2 && parts[parts.length - 1] === '') parts.pop();
    return `${model}=${parts.join(':')}`;
  }).join(',');
}

// The monthly allowance as ai.server.monthly_allowance writes it: 10, workspace=2,trial=1 or agent=3,trial=2.
export interface Allowance { mode: 'none' | 'workspace' | 'agent'; amount: string; trial: string }

export function parseAllowance(value: string): Allowance {
  const text = value.trim();
  if (text === '') return { mode: 'none', amount: '', trial: '' };
  if (!text.includes('=')) return { mode: 'workspace', amount: text, trial: '' };
  const parts = Object.fromEntries(text.split(',').map((entry) => entry.split('=', 2).map((part) => part.trim())));
  return parts.agent !== undefined ? { mode: 'agent', amount: parts.agent, trial: parts.trial ?? '' } : { mode: 'workspace', amount: parts.workspace ?? '', trial: parts.trial ?? '' };
}

export function writeAllowance(allowance: Allowance): string {
  if (allowance.mode === 'none') return '';
  const trial = allowance.trial.trim() ? `,trial=${allowance.trial.trim()}` : '';
  if (allowance.mode === 'agent') return `agent=${allowance.amount.trim()}${trial}`;
  return trial ? `workspace=${allowance.amount.trim()}${trial}` : allowance.amount.trim();
}

type ListedPrice = { input: number; output: number; cache_read: number; cache_write: number };

// What the provider of the change lists, shared by the provider's card and the pricing card: the models its
// key may use and the price of each, when the provider says (OpenRouter).
type Listing = { state: 'idle' | 'loading' | 'failed' | 'loaded'; forKey: string; models: string[]; prices: Record<string, ListedPrice>; message: string };
let listing: Listing = { state: 'idle', forKey: '', models: [], prices: {}, message: '' };
const listeners = new Set<() => void>();
function setListing(next: Listing) {
  listing = next;
  listeners.forEach((listener) => listener());
}
function useListing(): Listing {
  return useSyncExternalStore((listener) => { listeners.add(listener); return () => listeners.delete(listener); }, () => listing);
}

const dollars = (value: number) => String(Math.round(value * 1e6) / 1e6);
function listedPrice(price: ListedPrice): Price {
  return { input: dollars(price.input), output: dollars(price.output), cacheRead: price.cache_read ? dollars(price.cache_read) : '', cacheWrite: price.cache_write ? dollars(price.cache_write) : '' };
}

// Above this many matches, the list shows the first ones and asks to type more.
const shownAtOnce = 60;

// ModelBox is a model picked by typing: it searches the models the key may use as you type, and takes a name
// the list does not have (a provider that keeps no list). Arrow keys move, Enter picks, Escape closes.
function ModelBox({ label, value, models, prices, empty, disabled, onPick }: {
  label: string; value: string; models: string[]; prices: Record<string, ListedPrice>; empty?: string; disabled: boolean; onPick: (model: string) => void;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const typed = query.trim();
  const matches = models.filter((model) => aiModelMatches(model, typed));
  const options: Array<{ value: string; text: string }> = [
    ...(empty && !typed ? [{ value: '', text: empty }] : []),
    ...matches.slice(0, shownAtOnce).map((model) => ({ value: model, text: model })),
    ...(typed && !models.includes(typed) ? [{ value: typed, text: am.useTyped(typed) }] : []),
  ];
  const pick = (model: string) => { onPick(model); setOpen(false); setQuery(''); };
  const onKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') { event.preventDefault(); setOpen(true); setActive((index) => Math.min(index + 1, options.length - 1)); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); setActive((index) => Math.max(index - 1, 0)); }
    else if (event.key === 'Enter' && open && options[active]) { event.preventDefault(); pick(options[active].value); }
    else if (event.key === 'Escape' && open) { event.preventDefault(); setOpen(false); setQuery(''); }
  };
  const shownPrice = prices[value];
  return <div className="ops-ai-model">
    <label htmlFor={`${id}-input`}>{label}</label>
    <div className="ops-ai-box">
      <input id={`${id}-input`} role="combobox" aria-expanded={open} aria-controls={`${id}-list`} aria-autocomplete="list" autoComplete="off" spellCheck={false}
        aria-activedescendant={open && options[active] ? `${id}-option-${active}` : undefined} disabled={disabled}
        value={open ? query : value} placeholder={open ? value || am.searchModels : empty ?? am.searchModels}
        onFocus={() => { setOpen(true); setActive(0); }} onBlur={() => { setOpen(false); setQuery(''); }}
        onChange={(event) => { setQuery(event.target.value); setOpen(true); setActive(0); }} onKeyDown={onKey} />
      {open && <ul id={`${id}-list`} role="listbox" aria-label={label} className="ops-ai-options">
        {options.map((option, index) => <li key={`${option.value}-${index}`} id={`${id}-option-${index}`} role="option" aria-selected={option.value === value}
          data-active={index === active || undefined} onMouseDown={(event) => { event.preventDefault(); pick(option.value); }} onMouseEnter={() => setActive(index)}>
          <span className="ops-ai-option-name">{option.text}</span>
          {prices[option.value] && <span className="ops-ai-option-price">{am.perMillion(prices[option.value].input, prices[option.value].output)}</span>}
        </li>)}
        {options.length === 0 && <li className="ops-ai-options-note">{am.noMatch}</li>}
        {matches.length > shownAtOnce && <li className="ops-ai-options-note">{am.moreMatches(matches.length - shownAtOnce)}</li>}
      </ul>}
    </div>
    {shownPrice && !open && <span className="help">{am.perMillion(shownPrice.input, shownPrice.output)}</span>}
  </div>;
}

// listKeyOf names what a model list is for: the provider, address and key of the change.
function listKeyOf(kind: string, base: string, draft: Draft): string {
  return JSON.stringify([kind, base, draft.secrets['ai.server.api_key'] ?? '', draft.unset.includes('ai.server.api_key')]);
}

// ServerProvider is the server provider's part of AI & pricing: on or off, which service (as a workspace adds
// a provider), its name, key and address, and the model of each feature, searched in what the key may use.
// Off keeps every other value, so turning it on again needs nothing typed.
export default function ServerProvider({ byKey, draft, setDraft, errors, groupOf, SettingRow, change, currentValue, credit }: ServerProviderProps) {
  const id = useId();
  const provider = byKey.get('ai.server.provider');
  const baseSetting = byKey.get('ai.server.base_url');
  const modelsSetting = byKey.get('ai.server.models');
  const pricesSetting = byKey.get('ai.server.prices');
  const label = byKey.get('ai.server.label');
  const key = byKey.get('ai.server.api_key');
  const list = useListing();
  const [perFeature, setPerFeature] = useState<boolean | null>(null);
  const kind = provider ? currentValue(provider, draft) : '';
  const base = baseSetting ? currentValue(baseSetting, draft) : '';
  const on = kind !== '';
  const service = aiServiceOf(kind, base);
  const listKey = listKeyOf(kind, base, draft);
  const hasKey = Boolean(draft.secrets['ai.server.api_key']) || (Boolean(key?.set) && !draft.unset.includes('ai.server.api_key'));
  const canList = on && (kind === 'anthropic' ? hasKey : base.trim() !== '' && (hasKey || service === 'self_hosted'));
  // The list loads by itself for the provider, address and key on screen, a moment after they stop changing.
  const latest = useRef(draft);
  useEffect(() => { latest.current = draft; }, [draft]);
  useEffect(() => {
    if (!canList || (listing.forKey === listKey && listing.state !== 'failed')) return undefined;
    let live = true;
    const timer = window.setTimeout(() => {
      setListing({ ...listing, state: 'loading', forKey: listKey });
      const now = latest.current;
      api.check({ set: now.set, unset: now.unset, secrets: now.secrets, test: 'ai_models' }).then((result) => {
        if (!live) return;
        const test = result.test as (typeof result.test & { prices?: Record<string, ListedPrice> }) | undefined;
        if (test?.ok && test.models) setListing({ state: 'loaded', forKey: listKey, models: test.models, prices: test.prices ?? {}, message: '' });
        else setListing({ state: 'failed', forKey: listKey, models: [], prices: {}, message: test?.message ?? Object.values(result.errors).join(' ') });
      }, (error: unknown) => {
        if (live) setListing({ state: 'failed', forKey: listKey, models: [], prices: {}, message: error instanceof Error ? error.message : m.nothingSaved });
      });
    }, 400);
    return () => { live = false; window.clearTimeout(timer); };
  }, [canList, listKey]);
  if (!provider || !baseSetting || !modelsSetting) return null;
  const editable = provider.editable && !provider.locked;
  const models = parseModels(currentValue(modelsSetting, draft));
  const uniform = sameModel(models);
  const showPerFeature = perFeature ?? (uniform === null && slots.some((slot) => models[slot].length > 0));
  const current = list.forKey === listKey ? list : { ...listing, state: 'idle' as const, models: [], prices: {} };
  const offered = [...new Set([...current.models, ...modelsInUse(models)])];
  // Picking models gives each one picked its listed price, when it has none yet, so the allowance can count it.
  const setModels = (next: Models) => {
    change(setDraft, modelsSetting, writeModels(next));
    if (!pricesSetting) return;
    const prices = parsePrices(currentValue(pricesSetting, draft));
    const missing = modelsInUse(next).filter((model) => current.prices[model] && !prices.some(([priced]) => priced === model));
    if (missing.length > 0) change(setDraft, pricesSetting, writePrices([...prices, ...missing.map((model): [string, Price] => [model, listedPrice(current.prices[model])])]));
  };
  const pickService = (next: AIService) => {
    change(setDraft, provider, aiServiceKind(next));
    // A self-hosted server keeps an address typed for it; the services fill in their own.
    const address = aiServiceURL[next] ?? (next === 'self_hosted' && service === 'self_hosted' ? base : '');
    change(setDraft, baseSetting, address);
  };
  const status = !canList ? (kind === 'anthropic' || service !== 'self_hosted' ? am.listNeedsKey : am.listNeedsAddress)
    : current.state === 'loading' || current.state === 'idle' ? am.listing : current.state === 'failed' ? am.listFailed(current.message) : am.listed(current.models.length);
  const modelsError = errors['ai.server.models'];
  return <>
    <link rel="stylesheet" href={stylesheet} />
    {credit && on && <div className="ops-strip ops-ai-credit" data-tone="danger" role="alert">
      <span><strong>{am.outOfCredit}</strong> {am.outOfCreditBody(credit.detail ?? '', credit.since ? new Date(credit.since).toLocaleString() : '')}</span></div>}
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
      <fieldset className={`setting field ops-ai-models${modelsError ? ' field-bad' : ''}`}>
        <legend className="field-label">{am.serverModels}</legend>
        <code className="setting-key">ai.server.models</code>
        {!showPerFeature
          ? <ModelBox label={am.modelEveryFeature} value={uniform ?? ''} models={offered} prices={current.prices} disabled={!editable}
            onPick={(model) => setModels(model ? { triage: [model], draft: [model], chat: [model], intake: [model] } : { triage: [], draft: [], chat: [], intake: [] })} />
          : <div className="ops-ai-per-feature">
            {slots.map((slot) => <ModelBox key={slot} label={am.serverSlot[slot]} value={models[slot][0] ?? ''} models={offered} prices={current.prices} empty={am.notOffered} disabled={!editable}
              onPick={(model) => setModels({ ...models, [slot]: model ? [model, ...models[slot].slice(1).filter((other) => other !== model)] : [] })} />)}
            <ModelBox label={am.intakeFallback} value={models.intake[1] ?? ''} models={offered} prices={current.prices} empty={am.noFallback} disabled={!editable || models.intake.length === 0}
              onPick={(model) => setModels({ ...models, intake: model && model !== models.intake[0] ? [models.intake[0], model] : models.intake.slice(0, 1) })} />
          </div>}
        <div><span className={current.state === 'failed' && canList ? 'error-copy' : 'help'} role="status">{status}</span></div>
        {modelsError && <span className="error-copy field-note" role="alert">{modelsError}</span>}
        <span className="help field-note">{am.serverModelsHelp}</span>
        <div className="button-row">
          <button type="button" className="link-button" onClick={() => setPerFeature(!showPerFeature)}>{showPerFeature ? am.oneModelForAll : am.modelPerFeature}</button>
        </div>
      </fieldset>
    </>}
  </>;
}

// ServerPricing is the pricing card of AI & pricing: a price for each model, filled in from the provider's list
// when it has one, and the monthly allowance, as numbers instead of written settings.
export function ServerPricing({ byKey, draft, setDraft, errors, change, currentValue }: ServerProviderProps) {
  const id = useId();
  const list = useListing();
  const pricesSetting = byKey.get('ai.server.prices');
  const allowanceSetting = byKey.get('ai.server.monthly_allowance');
  const modelsSetting = byKey.get('ai.server.models');
  if (!pricesSetting || !allowanceSetting || !modelsSetting) return null;
  const editable = pricesSetting.editable && !pricesSetting.locked;
  const prices = parsePrices(currentValue(pricesSetting, draft));
  const inUse = modelsInUse(parseModels(currentValue(modelsSetting, draft)));
  const rows: Prices = [...inUse.map((model): [string, Price] => [model, prices.find(([priced]) => priced === model)?.[1] ?? { input: '', output: '', cacheRead: '', cacheWrite: '' }]),
    ...prices.filter(([model]) => !inUse.includes(model))];
  const allowance = parseAllowance(currentValue(allowanceSetting, draft));
  const save = (next: Prices) => change(setDraft, pricesSetting, writePrices(next.filter(([, price]) => price.input !== '' || price.output !== '' || price.cacheRead !== '')));
  const setPrice = (model: string, part: keyof Price, value: string) => save(rows.map(([each, price]): [string, Price] => [each, each === model ? { ...price, [part]: value } : price]));
  const fillable = rows.filter(([model, price]) => price.input === '' && price.output === '' && list.prices[model]);
  const setAllowance = (next: Allowance) => change(setDraft, allowanceSetting, writeAllowance(next));
  const number = (label: string, value: string, onChange: (value: string) => void, extra: { id?: string; disabled?: boolean } = {}) =>
    <input id={extra.id} type="text" inputMode="decimal" aria-label={label} value={value} disabled={!editable || extra.disabled} placeholder="0" onChange={(event) => onChange(event.target.value.replace(',', '.'))} />;
  return <>
    <link rel="stylesheet" href={stylesheet} />
    <div className={`setting field${errors['ai.server.prices'] ? ' field-bad' : ''}`}>
      <span className="field-label">{am.pricesTitle}</span>
      <code className="setting-key">ai.server.prices</code>
      {rows.length === 0 ? <div><span className="help">{am.noModelsYet}</span></div> : <table className="ops-ai-prices">
        <thead><tr><th scope="col">{am.priceModel}</th><th scope="col">{am.priceInput}</th><th scope="col">{am.priceOutput}</th><th scope="col">{am.priceCache}</th><th /></tr></thead>
        <tbody>{rows.map(([model, price]) => {
          const used = inUse.includes(model);
          const missing = used && (price.input === '' || price.output === '');
          return <tr key={model} data-missing={missing || undefined}>
            <th scope="row"><span className="ai-mono">{model}</span>{!used && <span className="help"> · {am.notInUse}</span>}{missing && <span className="error-copy"> · {am.needsPrice}</span>}</th>
            <td>{number(`${am.priceInput} ${model}`, price.input, (value) => setPrice(model, 'input', value))}</td>
            <td>{number(`${am.priceOutput} ${model}`, price.output, (value) => setPrice(model, 'output', value))}</td>
            <td>{number(`${am.priceCache} ${model}`, price.cacheRead, (value) => setPrice(model, 'cacheRead', value))}</td>
            <td>{!used && <button type="button" className="link-button" aria-label={am.removePrice(model)} disabled={!editable} onClick={() => save(rows.filter(([each]) => each !== model))}>×</button>}</td>
          </tr>;
        })}</tbody>
      </table>}
      {fillable.length > 0 && <button type="button" disabled={!editable} onClick={() => save(rows.map(([model, price]): [string, Price] => [model, fillable.some(([each]) => each === model) ? listedPrice(list.prices[model]) : price]))}>{am.fillPrices(fillable.length)}</button>}
      {errors['ai.server.prices'] && <span className="error-copy field-note" role="alert">{errors['ai.server.prices']}</span>}
      <span className="help field-note">{am.pricesHelp}</span>
    </div>
    <fieldset className={`setting field ops-ai-allowance${errors['ai.server.monthly_allowance'] ? ' field-bad' : ''}`}>
      <legend className="field-label">{am.allowanceTitle}</legend>
      <code className="setting-key">ai.server.monthly_allowance</code>
      <div className="ops-ai-modes">{(['none', 'workspace', 'agent'] as const).map((mode) => <label key={mode}>
        <input type="radio" name={`${id}-allowance`} checked={allowance.mode === mode} disabled={!editable}
          onChange={() => setAllowance({ ...allowance, mode, amount: allowance.amount || '2' })} />
        <span>{mode === 'none' ? am.allowanceNone : mode === 'workspace' ? am.allowanceWorkspace : am.allowanceAgent}</span></label>)}</div>
      {allowance.mode !== 'none' && <div className="ops-ai-amounts">
        <label htmlFor={`${id}-amount`}>{allowance.mode === 'agent' ? am.allowanceAgentAmount : am.allowanceAmount}</label>
        <span className="ops-ai-dollars">{number(allowance.mode === 'agent' ? am.allowanceAgentAmount : am.allowanceAmount, allowance.amount, (amount) => setAllowance({ ...allowance, amount }), { id: `${id}-amount` })}<span>{am.dollarsPerMonth}</span></span>
        <label htmlFor={`${id}-trial`}>{am.allowanceTrial}</label>
        <span className="ops-ai-dollars">{number(am.allowanceTrial, allowance.trial, (trial) => setAllowance({ ...allowance, trial }), { id: `${id}-trial` })}<span>{am.dollarsPerMonth}</span></span>
        <span />
        <span className="help">{am.allowanceTrialHelp}</span>
      </div>}
      {errors['ai.server.monthly_allowance'] && <span className="error-copy field-note" role="alert">{errors['ai.server.monthly_allowance']}</span>}
      <span className="help field-note">{am.allowanceHelp}</span>
    </fieldset>
  </>;
}
