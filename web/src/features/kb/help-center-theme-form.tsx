import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ColorRow, Hint, ImageField, controlId, linkShapes, normalize, validHex } from './help-center-theme-fields';
import { themeMessages as text } from './help-center-theme-messages';
import { HelpCenterPreview, useThemePreview } from './help-center-preview';
import { ThemeInvalidError, colorFields, fontKeys, themeApi } from './theme-api';
import type { ColorField, HelpCenterTheme, PreviewPage, ThemeCheck, ThemeInput } from './theme-api';
import './help-center-theme.css';

// The theme as the form edits it: text fields hold '' for the default.
type Draft = {
  logo_media_id: string | null; favicon_media_id: string | null; logo_alt: string; linkback_url: string; contact_url: string; contact_phone: string;
  colors: Record<ColorField, string>; body_font: string; heading_font: string;
};
type TextField = 'logo_alt' | LinkField;
type LinkField = keyof typeof linkShapes;

const blankColors = Object.fromEntries(colorFields.map((field) => [field, ''])) as Record<ColorField, string>;
const defaultLook: Draft = { logo_media_id: null, favicon_media_id: null, logo_alt: '', linkback_url: '', contact_url: '', contact_phone: '', colors: blankColors, body_font: '', heading_font: '' };

const draftOf = (theme: HelpCenterTheme): Draft => ({
  logo_media_id: theme.logo_media_id, favicon_media_id: theme.favicon_media_id, logo_alt: theme.logo_alt,
  linkback_url: theme.linkback_url ?? '', contact_url: theme.contact_url ?? '', contact_phone: theme.contact_phone ?? '',
  colors: Object.fromEntries(colorFields.map((field) => [field, theme.colors[field] ?? ''])) as Record<ColorField, string>,
  body_font: theme.body_font ?? '', heading_font: theme.heading_font ?? '',
});

const orNull = (value: string) => value.trim() || null;
// incomplete is true for a link or phone number written but not yet of a shape the server accepts.
const incomplete = (draft: Draft, field: LinkField) => draft[field].trim() !== '' && !linkShapes[field](draft[field].trim());
// For the preview, a link or phone number still being typed counts as none.
const previewable = (draft: Draft, field: LinkField) => (incomplete(draft, field) ? null : orNull(draft[field]));
type Font = (typeof fontKeys)[number];
const fontOrNull = (value: string) => (fontKeys as readonly string[]).includes(value) ? value as Font : null;

// inputOf is what Save sends. For the preview, a color, link or phone number still being typed counts as
// the default, so a half-written value does not stop the page from showing.
function inputOf(draft: Draft, forPreview = false): ThemeInput {
  const colors = Object.fromEntries(colorFields.map((field) => {
    const value = draft.colors[field].trim();
    return [field, !value ? null : validHex(value) ? normalize(value) : forPreview ? null : value];
  })) as Record<ColorField, string | null>;
  return {
    logo_media_id: draft.logo_media_id, favicon_media_id: draft.favicon_media_id, logo_alt: draft.logo_alt.trim(),
    linkback_url: forPreview ? previewable(draft, 'linkback_url') : orNull(draft.linkback_url),
    contact_url: forPreview ? previewable(draft, 'contact_url') : orNull(draft.contact_url),
    contact_phone: forPreview ? previewable(draft, 'contact_phone') : orNull(draft.contact_phone),
    colors, body_font: fontOrNull(draft.body_font), heading_font: fontOrNull(draft.heading_font),
  };
}

// The color in use for each field, read from the checks: a field left empty shows its default there.
function effectiveColors(checks: ThemeCheck[]): Partial<Record<ColorField, string>> {
  const pair = (name: string) => checks.find((check) => check.pair === name);
  return {
    primary: pair('primary_on_background')?.foreground, background: pair('text_on_background')?.background, text: pair('text_on_background')?.foreground,
    link: pair('link_on_background')?.foreground, header: pair('header_text_on_header')?.background, header_text: pair('header_text_on_header')?.foreground,
    footer: pair('footer_ink_on_footer')?.background,
  };
}

// The order of the controls on the page: a refused save puts the focus on the first one named.
const fieldOrder = ['logo_media_id', 'favicon_media_id', 'logo_alt', 'linkback_url', 'contact_url', 'contact_phone', ...colorFields.map((field) => `colors.${field}`), 'body_font', 'heading_font'];

// HelpCenterThemeForm is the brand part of the Help center card: an admin edits it beside a live preview;
// anyone else sees the saved values and the preview.
export function HelpCenterThemeForm({ canManage }: { canManage: boolean }) {
  const client = useQueryClient();
  const theme = useQuery({ queryKey: ['help-center-theme'], queryFn: themeApi.get });
  const [draft, setDraft] = useState<Draft>();
  const [page, setPage] = useState<PreviewPage>('home');
  const [errors, setErrors] = useState<Record<string, string>>({});
  // Set by a refused save: the next errors shown move the focus to the first control they name.
  const refused = useRef(false);
  // The form starts from the saved brand once; a refetch in the background never replaces what is being typed.
  useEffect(() => { if (theme.data) setDraft((editing) => editing ?? draftOf(theme.data)); }, [theme.data]);
  const save = useMutation({
    mutationFn: themeApi.save,
    onMutate: () => setErrors({}),
    onSuccess: (saved) => {
      client.setQueryData(['help-center-theme'], saved);
      setDraft(draftOf(saved));
    },
    onError: (error) => {
      if (!(error instanceof ThemeInvalidError)) return;
      refused.current = true;
      setErrors(error.fields);
    },
  });
  const current = draft ?? (theme.data ? draftOf(theme.data) : defaultLook);
  // No preview of the default look while the saved brand loads: the first one shows the saved brand.
  const live = useThemePreview(page, inputOf(canManage ? current : (theme.data ? draftOf(theme.data) : defaultLook), true), theme.data !== undefined);
  // After a refused save, the first control it names takes the focus; an edit that clears one does not move it.
  useEffect(() => {
    if (!refused.current) return;
    refused.current = false;
    const first = fieldOrder.find((field) => errors[field]);
    if (first) document.getElementById(controlId(first))?.focus();
  }, [errors]);
  if (theme.isError) return <p role="alert" className="error-copy">{text.loadFailed}</p>;
  if (!theme.data) return null;
  const checks = live.preview?.checks ?? theme.data.checks;
  const effective = effectiveColors(checks);
  // An edit clears the refusal of each field it changes, so the field shows its live hint again.
  const change = (next: Partial<Draft>) => {
    save.reset();
    const edited = Object.keys(next).flatMap((key) => (key === 'colors'
      ? colorFields.filter((field) => next.colors?.[field] !== current.colors[field]).map((field) => `colors.${field}`)
      : [key]));
    setErrors((shown) => (edited.some((field) => shown[field])
      ? Object.fromEntries(Object.entries(shown).filter(([field]) => !edited.includes(field)))
      : shown));
    setDraft({ ...current, ...next });
  };
  const preview = <HelpCenterPreview html={live.preview?.html} pending={live.pending} error={live.error} page={page} onPage={setPage} />;
  if (!canManage) return <div className="theme-layout"><ReadOnly theme={theme.data} />{preview}</div>;

  const textField = (field: TextField, label: string, help: string, type = 'text', maxLength = 2000) => {
    const id = controlId(field);
    return (
      <div className="field"><label htmlFor={id}>{label}</label>
        <input id={id} type={type} value={current[field]} maxLength={maxLength} aria-invalid={errors[field] ? true : undefined} aria-describedby={`${id}-hint`}
          onChange={(event) => change({ [field]: event.target.value })} />
        <Hint id={`${id}-hint`} help={help} error={errors[field]} warning={field !== 'logo_alt' && incomplete(current, field) ? text.shapeHints[field] : undefined} /></div>
    );
  };
  const fontField = (field: 'body_font' | 'heading_font', label: string, fallback: string) => (
    <div className="field"><label htmlFor={controlId(field)}>{label}</label>
      <select id={controlId(field)} value={current[field]} aria-invalid={errors[field] ? true : undefined} aria-describedby={errors[field] ? `${controlId(field)}-hint` : undefined}
        onChange={(event) => change({ [field]: event.target.value })}>
        <option value="">{text.defaultFont(text.fontNames[fallback]!)}</option>
        {fontKeys.map((key) => <option key={key} value={key}>{text.fontNames[key]}</option>)}
      </select>
      <Hint id={`${controlId(field)}-hint`} error={errors[field]} /></div>
  );
  const submit = (event: FormEvent) => { event.preventDefault(); if (!save.isPending) save.mutate(inputOf(current)); };
  const failure = save.isError ? (save.error instanceof ThemeInvalidError ? save.error.message : text.saveFailed) : undefined;
  return (
    <div className="theme-layout">
      <form className="stacked-form theme-form" onSubmit={submit} noValidate>
        <fieldset><legend>{text.brand}</legend>
          <ImageField field="logo_media_id" label={text.logo} id={current.logo_media_id} empty={text.noImage} error={errors.logo_media_id} onChange={(id) => change({ logo_media_id: id })} />
          {textField('logo_alt', text.logoAlt, text.logoAltHelp, 'text', 200)}
          <ImageField field="favicon_media_id" label={text.favicon} id={current.favicon_media_id} empty={text.noFavicon} error={errors.favicon_media_id} onChange={(id) => change({ favicon_media_id: id })} />
        </fieldset>
        <fieldset><legend>{text.links}</legend>
          {textField('linkback_url', text.website, text.websiteHelp, 'url')}
          {textField('contact_url', text.contact, text.contactHelp)}
          {textField('contact_phone', text.phone, text.phoneHelp, 'tel', 40)}
        </fieldset>
        <fieldset><legend>{text.colors}</legend>
          <p className="muted-copy theme-note">{text.colorsHelp}</p>
          {colorFields.map((field) => (
            <ColorRow key={field} field={field} label={text.colorNames[field]} value={current.colors[field]} effective={effective[field]}
              checks={checks.filter((check) => check.field === `colors.${field}`)} message={live.preview?.fields[`colors.${field}`]} error={errors[`colors.${field}`]}
              onChange={(value) => change({ colors: { ...current.colors, [field]: value } })} />
          ))}
        </fieldset>
        <fieldset><legend>{text.fonts}</legend>
          <p className="muted-copy theme-note">{text.fontsHelp}</p>
          {fontField('body_font', text.bodyFont, 'system-sans')}
          {fontField('heading_font', text.headingFont, 'classic-serif')}
        </fieldset>
        <span className="button-row">
          <button className="primary-action" type="submit" disabled={save.isPending}>{save.isPending ? text.saving : text.save}</button>
          <button type="button" aria-describedby="theme-reset-help" onClick={() => change(defaultLook)}>{text.resetAll}</button>
          {failure && <span role="alert" className="error-copy save-status">{failure}</span>}
          {save.isSuccess && <span role="status" className="save-status">{text.saved}</span>}
        </span>
        <small id="theme-reset-help" className="muted-copy">{text.resetAllHelp}</small>
      </form>
      {preview}
    </div>
  );
}

// HelpCenterThemeSection is the Brand section of the Help center card.
export function HelpCenterThemeSection({ canManage }: { canManage: boolean }) {
  return (
    <section className="theme-settings" aria-labelledby="help-center-brand-heading">
      <h3 id="help-center-brand-heading">{text.heading}</h3>
      <p className="muted-copy">{text.intro}</p>
      <HelpCenterThemeForm canManage={canManage} />
    </section>
  );
}

// ReadOnly lists the saved brand for someone who cannot change it.
function ReadOnly({ theme }: { theme: HelpCenterTheme }) {
  const font = (key: string | null, fallback: string) => text.fontNames[key ?? fallback]!;
  const rows: [string, string][] = [
    [text.logo, theme.logo_media_id ? text.isSet : text.none], [text.favicon, theme.favicon_media_id ? text.isSet : text.none],
    [text.website, theme.linkback_url ?? text.none], [text.contact, theme.contact_url ?? text.none], [text.phone, theme.contact_phone ?? text.none],
    ...colorFields.map((field): [string, string] => [text.colorNames[field], theme.colors[field] ?? text.byDefault]),
    [text.bodyFont, font(theme.body_font, 'system-sans')], [text.headingFont, font(theme.heading_font, 'classic-serif')],
  ];
  return (
    <div className="theme-form">
      <p className="muted-copy">{text.adminsOnly}</p>
      <ul className="theme-values" aria-label={text.heading}>
        {rows.map(([label, value]) => <li key={label}><span className="field-label">{label}</span> <span>{value}</span></li>)}
      </ul>
    </div>
  );
}
