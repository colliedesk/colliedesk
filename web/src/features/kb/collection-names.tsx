import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { kbApi } from './kb-api';
import type { KBCollection } from './kb-api';
import { KBDialog } from './kb-ui';
import { reason } from './kb-tree';
import { languageName, translationMessages as t } from './translation-messages';

// CollectionNames sets a collection's name and description in each of the help center's other languages.
export function CollectionNames({ collection, onChanged, onClose }: { collection: KBCollection; onChanged: () => Promise<unknown>; onClose: () => void }) {
  const languages = useQuery({ queryKey: ['kb-languages'], queryFn: kbApi.languages });
  return (
    <KBDialog title={t.collectionNamesOf(collection.name)} className="kb-collection-names" closeLabel={t.close} onClose={onClose}>
      {languages.isError && <p role="alert" className="error-copy">{t.loadFailed}</p>}
      {languages.data?.languages.length === 0 && <p className="kb-hint">{t.noOtherLanguages} <a href="/knowledge/languages">{t.manageLanguages}</a></p>}
      {languages.data?.languages.map((language) => (
        <CollectionName key={language.locale} collection={collection} locale={language.locale} onChanged={onChanged} />
      ))}
    </KBDialog>
  );
}

function CollectionName({ collection, locale, onChanged }: { collection: KBCollection; locale: string; onChanged: () => Promise<unknown> }) {
  const current = collection.translations.find((each) => each.locale === locale);
  const [name, setName] = useState(current?.name ?? '');
  const [description, setDescription] = useState(current?.description ?? '');
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const label = languageName(locale);
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true);
    setProblem(undefined);
    try {
      await action();
      await onChanged();
    } catch (error) {
      setProblem(reason(error, t.saveFailed));
    } finally {
      setBusy(false);
    }
  };
  const changed = name.trim() !== (current?.name ?? '') || description !== (current?.description ?? '');
  return (
    <form className="kb-collection-name" onSubmit={(event) => { event.preventDefault(); void run(() => kbApi.nameCollection(collection.id, locale, { name: name.trim(), description })); }}>
      <fieldset>
        <legend>{label}{current?.imported && <small className="kb-hint"> · {t.importedName}</small>}</legend>
        <div className="field"><label htmlFor={`kb-name-${locale}`}>{t.nameIn(label)}</label>
          <input id={`kb-name-${locale}`} value={name} maxLength={200} placeholder={collection.name} onChange={(event) => setName(event.target.value)} /></div>
        <div className="field"><label htmlFor={`kb-description-${locale}`}>{t.descriptionIn(label)}</label>
          <textarea id={`kb-description-${locale}`} rows={2} value={description} maxLength={2000} placeholder={collection.description} onChange={(event) => setDescription(event.target.value)} /></div>
        {problem && <p role="alert" className="error-copy">{problem}</p>}
        <div className="button-row">
          <button type="submit" disabled={busy || !name.trim() || !changed}>{t.saveName}</button>
          {current && <button type="button" className="danger-action" disabled={busy}
            onClick={() => void run(async () => { await kbApi.unnameCollection(collection.id, locale); setName(''); setDescription(''); })}>{t.removeName}</button>}
        </div>
      </fieldset>
    </form>
  );
}
