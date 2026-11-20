import { useId, useState } from 'react';
import type { KBArticle } from './kb-api';
import { kbMessages } from './kb-messages';
import { KBDialog } from './kb-ui';
import { lineDiff } from './line-diff';
import type { DiffLine } from './line-diff';

// A published article keeps its page on the help center while someone changes it: a save keeps the changes
// apart (the article's draft) until Publish changes puts them live, or Discard draft drops them.

// draftMessages live with the editor's chunk, not the first bundle.
export const draftMessages = {
  unpublishedChanges: 'Unpublished changes',
  draftHelp: (by: string | undefined) => (by ? `Saved by ${by}. The help center shows the published version until you publish these changes.`
    : 'The help center shows the published version until you publish these changes.'),
  saveFirst: 'Save your edits first, then publish them.',
  publishChanges: 'Publish changes',
  publishing: 'Publishing…',
  compare: 'Compare with published',
  discard: 'Discard draft',
  discardHeading: 'Discard the unpublished changes?',
  discardBody: 'The published article and its history stay as they are.',
  confirmDiscard: 'Discard changes',
  changesPublished: 'Changes published',
  changesDiscarded: 'Unpublished changes discarded',
  noDifference: 'The text is the same as the published version.',
  close: 'Close',
  title: 'Title',
  removed: 'Removed: ',
  added: 'Added: ',
};

// editable is the article as the editor shows and saves it: with unpublished changes, those, and their
// updated_at, which every save, publish, discard, restore and delete sends while they exist.
export const editable = (article: KBArticle): KBArticle => {
  const draft = article.draft;
  if (!draft) return article;
  return { ...article, title: draft.title, slug: draft.slug, body_md: draft.body_md, seo_title: draft.seo_title, seo_description: draft.seo_description,
    collection_id: draft.collection_id, collection_ids: draft.collection_ids, updated_at: draft.updated_at };
};

interface DraftBarProps {
  // article is the article as the server answered, with draft set.
  article: KBArticle;
  // title and body are the text on screen, which Compare with published sets beside the published one.
  title: string;
  body: () => string;
  busy: boolean;
  // dirty: edits on screen are not saved yet, and Publish changes would not include them.
  dirty: boolean;
  onPublish: () => void;
  onDiscard: () => Promise<void>;
}

// ArticleDraftBar says the article has unpublished changes and offers what comes next for them.
export function ArticleDraftBar({ article, title, body, busy, dirty, onPublish, onDiscard }: DraftBarProps) {
  const id = useId();
  const [compared, setCompared] = useState<{ title: string; lines: DiffLine[] }>();
  const [discarding, setDiscarding] = useState(false);
  const live = article.title;
  return (
    <div className="warning-note kb-draft-bar" role="group" aria-labelledby={`${id}-heading`}>
      <p><strong id={`${id}-heading`}>{draftMessages.unpublishedChanges}</strong>{' '}{draftMessages.draftHelp(article.draft?.updated_by?.name)}</p>
      <div className="button-row">
        <button type="button" className="primary-action" disabled={busy || dirty} aria-describedby={dirty ? `${id}-save-first` : undefined} onClick={onPublish}>
          {draftMessages.publishChanges}</button>
        <button type="button" onClick={() => setCompared({ title: title.trim(), lines: lineDiff(article.body_md, body()) })}>{draftMessages.compare}</button>
        <button type="button" className="danger-action" disabled={busy} onClick={() => setDiscarding(true)}>{draftMessages.discard}</button>
      </div>
      {dirty && <small id={`${id}-save-first`} className="kb-hint">{draftMessages.saveFirst}</small>}
      {compared && <KBDialog title={draftMessages.unpublishedChanges} className="kb-compare" phone="full" closeLabel={draftMessages.close} onClose={() => setCompared(undefined)}>
        {compared.title === live && compared.lines.every((line) => line.change === 'same') ? <p className="muted-copy">{draftMessages.noDifference}</p>
          : <ul className="kb-diff">
            {compared.title !== live && <>
              <li data-change="removed"><span className="kb-diff-label">{draftMessages.title}</span><span className="sr-only"> removed: </span>{live}</li>
              <li data-change="added"><span className="kb-diff-label">{draftMessages.title}</span><span className="sr-only"> added: </span>{compared.title}</li>
            </>}
            {compared.lines.map((line, at) => (
              <li key={at} data-change={line.change}>
                {line.change !== 'same' && <span className="sr-only">{line.change === 'removed' ? draftMessages.removed : draftMessages.added}</span>}{line.text}</li>
            ))}
          </ul>}
      </KBDialog>}
      {discarding && <KBDialog title={draftMessages.discardHeading} role="alertdialog" onClose={() => setDiscarding(false)} footer={<>
        <button type="button" disabled={busy} onClick={() => setDiscarding(false)}>{kbMessages.cancel}</button>
        <button type="button" className="danger-solid" disabled={busy} onClick={() => void onDiscard().then(() => setDiscarding(false))}>{draftMessages.confirmDiscard}</button>
      </>}><p className="muted-copy">{draftMessages.discardBody}</p></KBDialog>}
    </div>
  );
}
