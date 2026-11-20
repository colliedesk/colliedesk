import { useState } from 'react';
import { messages } from '../../messages';
import { reason } from './kb-tree';
import type { KBTemplateInput } from './kb-templates-api';
import { templateMessages as t } from './template-messages';

interface TemplateFormProps {
  label: string;
  initial?: KBTemplateInput;
  // onSave resolves when the server took the template; a rejection keeps the form open with its reason.
  onSave: (value: Required<KBTemplateInput>) => Promise<unknown>;
  onCancel: () => void;
}

// TemplateForm writes a template: its name, the line that says when to use it, and its Markdown. A template
// has no status, address or collections: it is never published.
export function TemplateForm({ label, initial, onSave, onCancel }: TemplateFormProps) {
  const [name, setName] = useState(initial?.name ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [body, setBody] = useState(initial?.body ?? '');
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const save = async () => {
    if (busy) return;
    if (!name.trim() || !body.trim()) { setProblem(t.nameAndTextRequired); return; }
    setBusy(true);
    setProblem(undefined);
    try {
      await onSave({ name, description, body });
    } catch (error) {
      setProblem(reason(error, t.actionFailed));
      setBusy(false);
    }
  };
  return (
    <form className="kb-template-form" aria-label={label} onSubmit={(event) => { event.preventDefault(); void save(); }}>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
      <label className="field"><span>{t.name}</span><input autoFocus value={name} maxLength={200} onChange={(event) => setName(event.target.value)} /></label>
      <label className="field"><span>{t.description}</span><input value={description} maxLength={300} onChange={(event) => setDescription(event.target.value)} /></label>
      <label className="field"><span>{t.body}</span><textarea rows={12} value={body} maxLength={100000} onChange={(event) => setBody(event.target.value)} /></label>
      <div className="kb-template-actions">
        <button type="submit" className="primary-action" disabled={busy}>{busy ? t.saving : t.save}</button>
        <button type="button" disabled={busy} onClick={onCancel}>{messages.cancel}</button>
      </div>
    </form>
  );
}
