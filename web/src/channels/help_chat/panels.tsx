import { useEffect, useId, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { channelsApi } from '../../api/channels';
import type { HelpChatConfig } from '../../api/channels';
import { kbApi } from '../../features/kb/kb-api';
import { CopyButton } from '../../features/settings/copy-button';
import { saveState, useSaveFeedback } from '../../features/settings/save-feedback';
import type { SaveState } from '../../features/settings/save-feedback';
import type { ChannelPanelProps, ConnectProps } from '../registry';

const limits = { greeting: 300, suggestions: 4, suggestion: 120, instructions: 2000 };

const m = {
  greeting: 'Greeting',
  greetingHint: 'Shown first when a chat starts, under “AI assistant · answers from your help articles”.',
  suggestions: 'Suggested questions',
  suggestionsHint: 'A customer selects one to ask it.',
  suggestion: (n: number) => `Suggested question ${n}`,
  remove: (text: string) => `Remove “${text || 'empty question'}”`,
  addQuestion: 'Add a question',
  full: (n: number) => `${n} of ${limits.suggestions}. Remove one to add another.`,
  instructions: 'Extra instructions',
  instructionsHint: 'The AI still answers only from published articles; instructions cannot add facts.',
  count: (n: number, max: number) => `${n.toLocaleString()} / ${max.toLocaleString()}`,
  save: 'Save',
  saveSite: 'Save sites',
  name: 'Name',
  nameHint: 'Tells this chat apart in the inbox and in Settings. Leave it empty to number it.',
  add: 'Add help chat',
  origins: 'Sites that may embed the chat',
  originsHint: 'One per line, exactly as the browser shows it: https://shop.example.com. No path, no wildcard. At most 20.',
  code: 'Code for your site',
  codeHelp: 'Paste it before </body> on every page that shows the chat button. The button takes your help center’s brand colour and adds no cookie to your site.',
  copyCode: 'Copy code',
  codeCopied: 'Code copied',
  chatPage: 'Open the chat page',
  noHelpCenter: 'Turn the help center on in Knowledge base to get the chat page and the code for your site.',
};

function lines(text: string): string[] {
  return text.split('\n').map((line) => line.trim()).filter(Boolean);
}

interface SurfaceFormProps {
  config?: HelpChatConfig;
  canManage: boolean;
  save: SaveState;
  label: string;
  onSave: (value: { greeting: string; suggestions: string[]; instructions: string }) => void;
  nameField?: ReactNode;
}

// SurfaceForm is what the chat says and offers: the greeting, up to 4 questions and the AI's extra instructions.
function SurfaceForm({ config, canManage, save, label, onSave, nameField }: SurfaceFormProps) {
  const [greeting, setGreeting] = useState(config?.greeting ?? '');
  const [suggestions, setSuggestions] = useState<string[]>(config?.suggestions ?? []);
  const [instructions, setInstructions] = useState(config?.instructions ?? '');
  useEffect(() => setGreeting(config?.greeting ?? ''), [config?.greeting]);
  useEffect(() => setSuggestions(config?.suggestions ?? []), [config?.suggestions]);
  useEffect(() => setInstructions(config?.instructions ?? ''), [config?.instructions]);
  const feedback = useSaveFeedback(save);
  const greetingHint = useId();
  const suggestionsHint = useId();
  const instructionsHint = useId();
  if (!canManage) {
    return <dl className="settings-body">
      <dt>{m.greeting}</dt><dd>{config?.greeting || '—'}</dd>
      <dt>{m.suggestions}</dt><dd>{(config?.suggestions ?? []).join(' · ') || '—'}</dd>
      <dt>{m.instructions}</dt><dd>{config?.instructions || '—'}</dd>
    </dl>;
  }
  const change = (index: number, value: string) => { feedback.touch(); setSuggestions((list) => list.map((item, i) => (i === index ? value : item))); };
  return <form className="stacked-form settings-body" onSubmit={(event) => {
    event.preventDefault();
    if (!feedback.pending) onSave({ greeting: greeting.trim(), suggestions: suggestions.map((item) => item.trim()).filter(Boolean), instructions: instructions.trim() });
  }}>
    {nameField}
    <label className="field"><span>{m.greeting}</span>
      <textarea rows={2} value={greeting} maxLength={limits.greeting} aria-describedby={greetingHint} onChange={(event) => { feedback.touch(); setGreeting(event.target.value); }} /></label>
    <small id={greetingHint} className="muted-copy">{m.greetingHint} {m.count(greeting.length, limits.greeting)}</small>
    <fieldset className="field"><legend>{m.suggestions}</legend>
      {suggestions.map((suggestion, index) => <span className="input-with-actions" key={index}>
        <input aria-label={m.suggestion(index + 1)} value={suggestion} maxLength={limits.suggestion} aria-describedby={suggestionsHint}
          onChange={(event) => change(index, event.target.value)} />
        <button type="button" className="icon-button" aria-label={m.remove(suggestion)} title={m.remove(suggestion)}
          onClick={() => { feedback.touch(); setSuggestions((list) => list.filter((_, i) => i !== index)); }}>×</button>
      </span>)}
      {suggestions.length < limits.suggestions
        ? <span className="button-row"><button type="button" onClick={() => { feedback.touch(); setSuggestions((list) => [...list, '']); }}>{m.addQuestion}</button></span>
        : <small className="muted-copy">{m.full(suggestions.length)}</small>}
    </fieldset>
    <small id={suggestionsHint} className="muted-copy">{m.suggestionsHint}</small>
    <label className="field"><span>{m.instructions}</span>
      <textarea rows={4} value={instructions} maxLength={limits.instructions} aria-describedby={instructionsHint}
        onChange={(event) => { feedback.touch(); setInstructions(event.target.value); }} /></label>
    <small id={instructionsHint} className="muted-copy">{m.instructionsHint} {m.count(instructions.length, limits.instructions)}</small>
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(label)}</button>
      {feedback.status}</span>
  </form>;
}

function useSave(channelID: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (config: Partial<Omit<HelpChatConfig, 'channel_id' | 'enabled'>>) => channelsApi.update(channelID, { config }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['channels'] }),
  });
}

function Setup({ channel, canManage }: ChannelPanelProps) {
  const save = useSave(channel.id);
  return <SurfaceForm config={channel.config as HelpChatConfig} canManage={canManage} save={saveState(save)} label={m.save} onSave={(value) => save.mutate(value)} />;
}

// Embed is the sites that may frame the chat and the code that puts its button on them.
function Embed({ channel, canManage }: ChannelPanelProps) {
  const config = channel.config as HelpChatConfig;
  const save = useSave(channel.id);
  const [origins, setOrigins] = useState(config.widget_origins.join('\n'));
  useEffect(() => setOrigins(config.widget_origins.join('\n')), [config.widget_origins]);
  const feedback = useSaveFeedback(saveState(save));
  const hint = useId();
  const center = useQuery({ queryKey: ['help-center'], queryFn: kbApi.helpCenter });
  const home = center.data?.enabled ? center.data.url.replace(/\/$/, '') : undefined;
  const code = home ? `<script src="${home}/chat/widget.js" data-channel="${channel.id}" async></script>` : undefined;
  return <div className="settings-body">
    {canManage
      ? <form className="stacked-form" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) save.mutate({ widget_origins: lines(origins) }); }}>
          <label className="field"><span>{m.origins}</span>
            <textarea rows={3} value={origins} placeholder="https://shop.example.com" aria-describedby={hint} onChange={(event) => { feedback.touch(); setOrigins(event.target.value); }} /></label>
          <small id={hint} className="muted-copy">{m.originsHint}</small>
          <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveSite)}</button>
            {feedback.status}</span>
        </form>
      : <ul className="muted-copy">{config.widget_origins.map((origin) => <li key={origin}>{origin}</li>)}</ul>}
    {code && home
      ? <section className="embed-code" aria-labelledby="help-chat-code-heading">
          <h3 id="help-chat-code-heading" className="field-label">{m.code}</h3>
          <pre><code>{code}</code></pre>
          <p className="muted-copy">{m.codeHelp}</p>
          <span className="button-row"><CopyButton value={code} copiedText={m.codeCopied}>{m.copyCode}</CopyButton>
            <a href={`${home}/chat`} target="_blank" rel="noreferrer">{m.chatPage} ↗</a></span>
        </section>
      : center.isSuccess && <p className="notice notice-plain" role="status"><span>{m.noHelpCenter}</span></p>}
  </div>;
}

// Connect adds the chat, on, with what it says first; a name left empty is the type's.
function Connect({ canManage, create }: ConnectProps) {
  const [name, setName] = useState('');
  const save = useMutation({
    mutationFn: (value: { greeting: string; suggestions: string[]; instructions: string }) =>
      create({ ...(name.trim() ? { name: name.trim() } : {}), enabled: true, config: value }),
  });
  const hint = useId();
  const nameField = <>
    <label className="field"><span>{m.name}</span>
      <input value={name} maxLength={80} aria-describedby={hint} onChange={(event) => setName(event.target.value)} /></label>
    <small id={hint} className="muted-copy">{m.nameHint}</small>
  </>;
  return <SurfaceForm canManage={canManage} save={saveState(save)} label={m.add} nameField={nameField} onSave={(value) => save.mutate(value)} />;
}

export const Tabs = { setup: Setup, embed: Embed };
export { Connect };
