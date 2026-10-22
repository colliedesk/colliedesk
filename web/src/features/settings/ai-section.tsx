import { useEffect, useState } from 'react';
import type { AISettingsChange, AIWorkspace } from './ai-settings-api';
import { useSaveFeedback } from './save-feedback';
import type { SaveState } from './save-feedback';

interface AISectionProps {
  workspace?: AIWorkspace;
  canManage: boolean;
  save?: SaveState;
  onSave?: (change: AISettingsChange) => void;
}

const text = {
  heading: 'AI drafts',
  description: 'Colliedesk drafts a reply and a triage suggestion from your knowledge base when a customer writes. A person sends, edits or discards it, unless a topic has earned auto-answer.',
  unavailable: 'This server has no AI provider. The administrator of the server sets AI_SERVER_PROVIDER and AI_SERVER_API_KEY (Self-hosting, AI).',
  drafts: 'Write AI drafts for new customer messages',
  autoAnswer: 'Answer on its own when a topic has earned it',
  autoAnswerHelp: 'Only drafts rated high, on an article whose drafts people sent with small changes at most, go out on their own, after the delay below. Anyone can stop one before it goes.',
  delay: 'Minutes before an answer goes out',
  reviewed: 'Drafts a person must review first, per article',
  accuracy: 'Share sent with small changes at most (%)',
  save: 'Save AI settings',
  on: 'Auto-answer is on', off: 'Auto-answer is off', draftsOff: 'AI drafts are off',
  topics: 'Record per knowledge base article',
  noTopics: 'No reviewed drafts yet. A topic earns its record as people send or discard drafts that rest on it.',
  usage: (u: AIWorkspace['usage']) => `Last 30 days: ${u.drafts} ${u.drafts === 1 ? 'draft' : 'drafts'}, ${u.sent_unchanged} sent unchanged, ${u.sent_edited} sent edited, ${u.discarded} discarded, ${u.auto_sent} sent on their own; ${u.input_tokens + u.output_tokens} tokens.`,
};

const percent = (accepted: number, reviewed: number) => (reviewed === 0 ? '–' : `${Math.round((100 * accepted) / reviewed)}%`);

export function AISection({ workspace, canManage, save, onSave }: AISectionProps) {
  const settings = workspace?.settings;
  const [drafts, setDrafts] = useState(settings?.drafts_enabled ?? true);
  const [autoAnswer, setAutoAnswer] = useState(settings?.auto_answer_enabled ?? false);
  const [minutes, setMinutes] = useState(String((settings?.auto_answer_delay_seconds ?? 300) / 60));
  const [reviewed, setReviewed] = useState(String(settings?.auto_answer_min_reviewed ?? 10));
  const [accuracy, setAccuracy] = useState(String(settings?.auto_answer_min_accuracy ?? 90));
  useEffect(() => {
    if (!settings) return;
    setDrafts(settings.drafts_enabled); setAutoAnswer(settings.auto_answer_enabled); setMinutes(String(settings.auto_answer_delay_seconds / 60));
    setReviewed(String(settings.auto_answer_min_reviewed)); setAccuracy(String(settings.auto_answer_min_accuracy));
  }, [settings]);
  const feedback = useSaveFeedback(save);
  if (!workspace || !settings) return null;
  const state = !settings.drafts_enabled ? text.draftsOff : settings.auto_answer_enabled ? text.on : text.off;
  const touch = <T,>(set: (value: T) => void) => (value: T) => { feedback.touch(); set(value); };
  return (
    <section className="settings-card" aria-labelledby="ai-heading">
      <div className="section-heading"><h2 id="ai-heading">{text.heading}</h2><p>{text.description}</p></div>
      <div className="settings-body">
        {!settings.available ? <p className="muted-copy">{text.unavailable}</p> : <>
          <p role="status" className="saved-state" data-on={settings.drafts_enabled && settings.auto_answer_enabled ? 'true' : 'false'}>{state}</p>
          {canManage && <form className="stacked-form" onSubmit={(event) => {
            event.preventDefault();
            if (feedback.pending) return;
            onSave?.({ drafts_enabled: drafts, auto_answer_enabled: autoAnswer, auto_answer_delay_seconds: Math.round(Number(minutes) * 60),
              auto_answer_min_reviewed: Number(reviewed), auto_answer_min_accuracy: Number(accuracy) });
          }}>
            <label className="field-inline"><input type="checkbox" checked={drafts} onChange={(event) => touch(setDrafts)(event.target.checked)} /><span>{text.drafts}</span></label>
            <label className="field-inline"><input type="checkbox" checked={autoAnswer} disabled={!drafts} onChange={(event) => touch(setAutoAnswer)(event.target.checked)} /><span>{text.autoAnswer}</span></label>
            <small className="muted-copy">{text.autoAnswerHelp}</small>
            <label className="field"><span>{text.delay}</span><input type="number" min={0} max={1440} value={minutes} onChange={(event) => touch(setMinutes)(event.target.value)} /></label>
            <label className="field"><span>{text.reviewed}</span><input type="number" min={3} max={1000} value={reviewed} onChange={(event) => touch(setReviewed)(event.target.value)} /></label>
            <label className="field"><span>{text.accuracy}</span><input type="number" min={50} max={100} value={accuracy} onChange={(event) => touch(setAccuracy)(event.target.value)} /></label>
            <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(text.save)}</button>{feedback.status}</span>
          </form>}
          <p className="muted-copy">{text.usage(workspace.usage)}</p>
          <h3>{text.topics}</h3>
          {workspace.topics.length === 0 ? <p className="muted-copy">{text.noTopics}</p>
            : <table className="ai-topics"><thead><tr><th scope="col">Article</th><th scope="col">Reviewed</th><th scope="col">Sent as drafted</th><th scope="col">Auto-answer</th></tr></thead>
              <tbody>{workspace.topics.map((topic) => <tr key={topic.article_id}>
                <td><a href={`/knowledge/articles/${topic.article_id}`}>#{topic.article_number} {topic.title}</a></td>
                <td>{topic.reviewed}</td><td>{topic.accepted} ({percent(topic.accepted, topic.reviewed)})</td><td>{topic.eligible ? 'Earned' : 'Not yet'}</td></tr>)}</tbody></table>}
        </>}
      </div>
    </section>
  );
}
