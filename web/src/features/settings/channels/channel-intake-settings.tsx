import { useId, useState } from 'react';
import type { FormEvent } from 'react';
import { InvalidRequestError } from '../../../api/client';
import type { AISettings, ChannelAIInput } from '../../../api/ai';
import { scopeName } from '../ai-levels';
import { channelsMessages as m } from './channels-messages';

type IntakeOptions = NonNullable<ChannelAIInput['options']>;

// The sources of context intake may send besides what the customer wrote, with the consent a source needs
// (Settings → Built-in AI) and whether it is on by default (contracts/openapi.yaml IntakeOptions.sources).
const sources: { key: string; consent?: string; byDefault: boolean }[] = [
  { key: 'thread_window', byDefault: true },
  { key: 'open_conversations', byDefault: true },
  { key: 'corrections', byDefault: false },
  { key: 'kb', byDefault: true },
  { key: 'contact', consent: 'contact_profile', byDefault: false },
  { key: 'images', consent: 'images', byDefault: false },
];

const thresholds = [
  { key: 'create_threshold', label: m.intakeCreateThreshold, byDefault: '0.85' },
  { key: 'attach_threshold', label: m.intakeAttachThreshold, byDefault: '0.90' },
  { key: 'ignore_threshold', label: m.intakeIgnoreThreshold, byDefault: '0.95' },
] as const;

// The fields this form edits; the server takes the options whole, so any other field (a gate override set in
// the Auto dialog, the masking patterns) goes back as it came.
const edited = ['create_threshold', 'attach_threshold', 'ignore_threshold', 'attach_requires_single_candidate', 'auto_max_age_seconds', 'reopen_window_days',
  'sources', 'instructions', 'redact_keywords', 'contact_labels', 'redact_patterns'];

type Mask = NonNullable<IntakeOptions['redact_patterns']>[number]['mask'];
const masks: Mask[] = ['phone', 'id_number', 'account_number', 'hidden'];

const lines = (text: string) => text.split('\n').map((line) => line.trim()).filter(Boolean);
const number = (text: string) => (text.trim() === '' ? undefined : Number(text));

interface Props {
  scope: string;
  // The scope's own level, sent back unchanged with the settings.
  level: ChannelAIInput['level'];
  options: IntakeOptions | null;
  settings?: AISettings;
  isAdmin: boolean;
  onSave: (input: ChannelAIInput) => Promise<unknown>;
}

// IntakeSettings is how intake decides on one scope of a chat channel (docs/user/ai.md "Intake"): its
// thresholds for deciding alone, when a resolved conversation is still a candidate, what context it reads,
// the channel's instructions and the words that mark what must be hidden from AI. A field left empty
// keeps the server's default.
export function IntakeSettings({ scope, level, options, settings, isAdmin, onSave }: Props) {
  const id = useId();
  const saved = options ?? {};
  const [values, setValues] = useState(() => ({
    create_threshold: saved.create_threshold?.toString() ?? '', attach_threshold: saved.attach_threshold?.toString() ?? '',
    ignore_threshold: saved.ignore_threshold?.toString() ?? '',
    reopen: saved.reopen_window_days?.toString() ?? '', age: saved.auto_max_age_seconds === undefined ? '' : String(Math.round(saved.auto_max_age_seconds / 60)),
    single: saved.attach_requires_single_candidate ?? true, sources: { ...(saved.sources ?? {}) }, instructions: saved.instructions ?? '',
    patterns: (saved.redact_patterns ?? []).map((item) => ({ ...item })),
    keywords: (saved.redact_keywords ?? []).join('\n'), accounts: (saved.contact_labels?.account ?? []).join('\n'), ids: (saved.contact_labels?.id ?? []).join('\n'),
  }));
  const [state, setState] = useState<'idle' | 'saving' | 'saved'>('idle');
  const [ageTouched, setAgeTouched] = useState(false);
  const [problem, setProblem] = useState<string>();
  const consents = new Set(settings?.source_consents.map((consent) => consent.source_class));
  const set = <K extends keyof typeof values>(key: K, value: (typeof values)[K]) => { setValues((current) => ({ ...current, [key]: value })); setState('idle'); };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const next: IntakeOptions = Object.fromEntries(Object.entries(saved).filter(([key]) => !edited.includes(key)));
    for (const item of thresholds) { const value = number(values[item.key]); if (value !== undefined) next[item.key] = value; }
    if (!values.single) next.attach_requires_single_candidate = false;
    if (number(values.reopen) !== undefined) next.reopen_window_days = number(values.reopen);
    // Shown in whole minutes: a value set in seconds goes back as it was unless the field was changed.
    if (ageTouched && number(values.age) !== undefined) next.auto_max_age_seconds = Math.round((number(values.age) ?? 0) * 60);
    else if (!ageTouched && saved.auto_max_age_seconds !== undefined) next.auto_max_age_seconds = saved.auto_max_age_seconds;
    if (Object.keys(values.sources).length > 0) next.sources = values.sources;
    if (lines(values.keywords).length > 0) next.redact_keywords = lines(values.keywords);
    const patterns = values.patterns.filter((item) => item.pattern.trim() !== '').map((item) => ({ pattern: item.pattern.trim(), mask: item.mask }));
    if (patterns.length > 0) next.redact_patterns = patterns;
    if (lines(values.accounts).length > 0 || lines(values.ids).length > 0) {
      next.contact_labels = { ...(lines(values.accounts).length > 0 ? { account: lines(values.accounts) } : {}), ...(lines(values.ids).length > 0 ? { id: lines(values.ids) } : {}) };
    }
    if (values.instructions.trim()) next.instructions = values.instructions.trim();
    setState('saving');
    setProblem(undefined);
    try {
      await onSave({ scope, level, options: next });
      setState('saved');
    } catch (error) {
      setProblem(error instanceof InvalidRequestError ? error.message : m.actionFailed);
      setState('idle');
    }
  };
  // A scope that follows the default takes the default's settings: the server keeps settings with a level.
  const follows = level === null;
  const locked = !isAdmin || follows || state === 'saving';
  const heading = `${id}-heading`;
  return <form className="ch-intake" aria-labelledby={heading} onSubmit={(event) => void submit(event)} noValidate>
    <h3 id={heading}>{m.intakeSettings(scopeName(scope))}</h3>
    <p className="ch-ai-lead">{follows ? m.intakeSettingsFollow(scopeName(scope)) : m.intakeSettingsLead}</p>
    <fieldset className="ch-intake-group" disabled={locked}>
      <legend>{m.intakeThresholds}</legend>
      <p className="help">{m.intakeThresholdsHelp}</p>
      <div className="ch-intake-row">
        {thresholds.map((item) => <label key={item.key} className="field"><span>{item.label}</span>
          <input type="number" min={0.5} max={0.99} step={0.01} inputMode="decimal" placeholder={item.byDefault} value={values[item.key]}
            onChange={(event) => set(item.key, event.target.value)} /></label>)}
      </div>
      <label className="ch-intake-check"><input type="checkbox" checked={values.single} onChange={(event) => set('single', event.target.checked)} />
        <span>{m.intakeSingleCandidate}</span></label>
      <div className="ch-intake-row">
        <label className="field"><span>{m.intakeReopenWindow}</span>
          <input type="number" min={0} max={90} step={1} placeholder="7" value={values.reopen} onChange={(event) => set('reopen', event.target.value)} /></label>
        <label className="field"><span>{m.intakeMaxAge}</span>
          <input type="number" min={1} max={1440} step={1} placeholder="120" value={values.age} onChange={(event) => { setAgeTouched(true); set('age', event.target.value); }} /></label>
      </div>
    </fieldset>
    <fieldset className="ch-intake-group" disabled={locked}>
      <legend>{m.intakeSources}</legend>
      <p className="help">{m.intakeSourcesHelp}</p>
      {sources.map((source) => {
        const missing = Boolean(source.consent) && !consents.has(source.consent ?? '');
        return <label key={source.key} className="ch-intake-check">
          <input type="checkbox" disabled={missing} checked={!missing && (values.sources[source.key] ?? source.byDefault)}
            onChange={(event) => set('sources', { ...values.sources, [source.key]: event.target.checked })} />
          <span>{m.intakeSource[source.key]}{missing && <small className="muted-copy"> · {isAdmin
            ? <a href="/settings/ai">{m.intakeNeedsConsent}</a> : m.intakeNeedsConsent}</small>}</span>
        </label>;
      })}
    </fieldset>
    <fieldset className="ch-intake-group" disabled={locked}>
      <legend>{m.intakePrivacy}</legend>
      <div className="field"><label htmlFor={`${id}-keywords`}>{m.intakeKeywords}</label>
        <textarea id={`${id}-keywords`} rows={3} aria-describedby={`${id}-keywords-help`} value={values.keywords} onChange={(event) => set('keywords', event.target.value)} />
        <small className="help" id={`${id}-keywords-help`}>{m.intakeKeywordsHelp}</small></div>
      <div className="ch-intake-row">
        <label className="field"><span>{m.intakeAccountLabels}</span>
          <textarea rows={2} value={values.accounts} onChange={(event) => set('accounts', event.target.value)} /></label>
        <label className="field"><span>{m.intakeIDLabels}</span>
          <textarea rows={2} value={values.ids} onChange={(event) => set('ids', event.target.value)} /></label>
      </div>
      <small className="help">{m.intakeLabelsHelp}</small>
      <fieldset className="ch-intake-patterns">
        <legend>{m.intakePatterns}</legend>
        <small className="help">{m.intakePatternsHelp}</small>
        {values.patterns.map((item, index) => <div key={index} className="ch-intake-pattern">
          <input aria-label={m.intakePattern(index + 1)} value={item.pattern} maxLength={200} spellCheck={false} className="mono"
            onChange={(event) => set('patterns', values.patterns.map((entry, at) => (at === index ? { ...entry, pattern: event.target.value } : entry)))} />
          <select aria-label={m.intakePatternMask(index + 1)} value={item.mask}
            onChange={(event) => set('patterns', values.patterns.map((entry, at) => (at === index ? { ...entry, mask: event.target.value as Mask } : entry)))}>
            {masks.map((mask) => <option key={mask} value={mask}>{m.intakeMask[mask]}</option>)}
          </select>
          <button type="button" className="ghost" aria-label={m.intakePatternRemove(index + 1)}
            onClick={() => set('patterns', values.patterns.filter((_, at) => at !== index))}>{m.intakePatternRemoveShort}</button>
        </div>)}
        {values.patterns.length < 20 && <button type="button" onClick={() => set('patterns', [...values.patterns, { pattern: '', mask: 'hidden' }])}>{m.intakePatternAdd}</button>}
      </fieldset>
    </fieldset>
    <fieldset className="ch-intake-group" disabled={locked}>
      <legend className="sr-only">{m.intakeInstructions}</legend>
      <div className="field"><label htmlFor={`${id}-instructions`}>{m.intakeInstructions}</label>
        <textarea id={`${id}-instructions`} rows={4} maxLength={4000} aria-describedby={`${id}-instructions-count`} value={values.instructions}
          onChange={(event) => set('instructions', event.target.value)} />
        <small className="help" id={`${id}-instructions-count`}>{m.intakeInstructionsCount(values.instructions.length)}</small></div>
    </fieldset>
    {isAdmin && !follows && <div className="button-row">
      <button type="submit" className="primary-action" disabled={locked}>{m.intakeSave}</button>
      {state === 'saved' && <span role="status" className="muted-copy">{m.intakeSaved}</span>}
    </div>}
    {problem && <p role="alert" className="error-copy">{problem}</p>}
  </form>;
}
