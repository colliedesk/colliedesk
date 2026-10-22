import { useRef, useState } from 'react';
import { themeMessages } from './help-center-theme-messages';
import { kbApi } from './kb-api';
import type { ThemeCheck } from './theme-api';

// The library picker comes with the knowledge base chunk and its styles, loaded only when someone opens
// it (often already there: the rail link preloads it). A plain import, not ui/lazy-part, and no chunk of
// its own: either would add to what the first bundle carries for this card.
type Picker = typeof import('./kb-chunk').MediaPicker;
let loadPicker: Promise<Picker> | undefined;
const picker = () => (loadPicker ??= import('./kb-chunk').then((module) => module.MediaPicker, (error: unknown) => {
  loadPicker = undefined;
  throw error;
}));

// controlId is the id of the control that shows a field of the theme, so a refusal can name it.
export const controlId = (field: string) => `theme-${field.replace(/[._]/g, '-')}`;

// Hint holds what a control's description says: the server's refusal, else a local warning (a value the
// preview leaves out until it is complete), else its help.
export function Hint({ id, help, warning, error }: { id: string; help?: string; warning?: string; error?: string }) {
  if (!help && !warning && !error) return null;
  return <small id={id} className={error ? 'error-copy theme-hint' : warning ? 'theme-hint theme-hint-warn' : 'muted-copy theme-hint'}>{error ?? warning ?? help}</small>;
}

// ImageField chooses the logo or the favicon: an upload into the media library, a pick from it, or none.
export function ImageField({ field, label, id, empty, error, onChange }: {
  field: string; label: string; id: string | null; empty: string; error?: string; onChange: (id: string | null) => void;
}) {
  const file = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [Picker, setPicker] = useState<Picker>();
  const choose = () => picker().then((loaded) => setPicker(() => loaded), () => setProblem(themeMessages.pickerFailed));
  const upload = async (chosen: File) => {
    setBusy(true);
    setProblem(undefined);
    try {
      const item = await kbApi.uploadImage(chosen);
      onChange(item.id);
    } catch {
      setProblem(themeMessages.uploadFailed);
    } finally {
      setBusy(false);
    }
  };
  const hint = `${controlId(field)}-hint`;
  return (
    <div className="theme-image" role="group" aria-labelledby={`${controlId(field)}-label`}>
      <span id={`${controlId(field)}-label`} className="field-label">{label}</span>
      <div className="theme-image-row">
        <span className="theme-image-box">{id ? <img src={`/api/v1/kb/media/${id}/thumbnail`} alt={label} /> : <span className="muted-copy">{empty}</span>}</span>
        <span className="button-row">
          <input ref={file} type="file" hidden accept="image/png,image/jpeg,image/gif,image/webp" onChange={(event) => {
            const chosen = event.target.files?.[0];
            event.target.value = '';
            if (chosen) void upload(chosen);
          }} />
          <button id={controlId(field)} type="button" disabled={busy} aria-invalid={error ? true : undefined} aria-describedby={error || problem ? hint : undefined}
            onClick={() => file.current?.click()}>{busy ? themeMessages.uploading : themeMessages.upload(label)}</button>
          <button type="button" onClick={() => void choose()}>{themeMessages.choose}</button>
          {id && <button type="button" className="link-button" onClick={() => onChange(null)}>{themeMessages.remove(label)}</button>}
        </span>
      </div>
      <Hint id={hint} error={error ?? problem} />
      {Picker && <Picker onClose={() => setPicker(undefined)} onUpload={() => { setPicker(undefined); file.current?.click(); }}
        onInsert={(items) => {
          const image = items.find((item) => item.kind === 'image');
          setPicker(undefined);
          if (image) {
            setProblem(undefined);
            onChange(image.id);
          } else setProblem(themeMessages.chooseImage);
        }} />}
    </div>
  );
}

const hex = /^#?[0-9a-fA-F]{6}$/;

// ColorRow edits one color: its hex text, the browser's picker and a reset to the default. Its hint is the
// server's refusal after a save, else the live contrast of the preview.
export function ColorRow({ field, label, value, effective, checks, message, error, onChange }: {
  field: string; label: string; value: string; effective?: string; checks: ThemeCheck[]; message?: string; error?: string; onChange: (value: string) => void;
}) {
  const id = controlId(`colors.${field}`);
  const invalid = value.trim() !== '' && !hex.test(value.trim());
  const weakest = checks.reduce<ThemeCheck | undefined>((low, check) => (!low || check.ratio / check.required < low.ratio / low.required ? check : low), undefined);
  const live = message ?? (weakest ? themeMessages.contrast(weakest.ratio, weakest.required) : undefined);
  const shown = error ?? (invalid ? themeMessages.hexHint : live);
  const picker = hex.test(value.trim()) ? normalize(value) : effective ?? '#000000';
  return (
    <div className="theme-color">
      <label htmlFor={id}>{label}</label>
      <div className="theme-color-row">
        <input type="color" value={picker} aria-label={themeMessages.pickColor(label)} onChange={(event) => onChange(event.target.value)} />
        <input id={id} value={value} maxLength={7} placeholder={effective} spellCheck={false} autoComplete="off"
          aria-invalid={error ? true : undefined} aria-describedby={shown ? `${id}-hint` : undefined} onChange={(event) => onChange(event.target.value)} />
        <button type="button" aria-label={themeMessages.resetColor(label)} disabled={value === ''} onClick={() => onChange('')}>{themeMessages.reset}</button>
      </div>
      {shown && <small id={`${id}-hint`} className={error ? 'error-copy theme-hint' : (message || invalid) ? 'theme-hint theme-hint-warn' : 'muted-copy theme-hint'}>{shown}</small>}
    </div>
  );
}

// normalize writes a hex color the way the server stores it: lowercase, with its #.
export function normalize(value: string): string {
  const trimmed = value.trim().toLowerCase();
  return trimmed.startsWith('#') ? trimmed : `#${trimmed}`;
}

export const validHex = (value: string) => hex.test(value.trim());

// The shapes the server accepts for the links and the phone (domain.CleanTheme), checked here only to keep
// a value still being typed out of the preview; Save sends what is written and the server decides.
const atom = "[A-Za-z0-9!#$&'*+/=^_`{|}~-]+";
const dotAtom = `${atom}(?:\\.${atom})*`;
const mailto = new RegExp(`^mailto:${dotAtom}@${dotAtom}$`, 'i');
const phoneShape = /^[0-9 +\-()]+$/;

function webAddress(value: string, schemes: readonly string[]): boolean {
  if (value.length > 2000) return false;
  try {
    const parsed = new URL(value);
    return schemes.includes(parsed.protocol) && parsed.hostname !== '' && !parsed.username && !parsed.password;
  } catch {
    return false;
  }
}

export const linkShapes = {
  linkback_url: (value: string) => webAddress(value, ['http:', 'https:']),
  contact_url: (value: string) => mailto.test(value) || webAddress(value, ['https:']),
  contact_phone: (value: string) => value.length <= 40 && phoneShape.test(value) && /[0-9]/.test(value),
};
