import { useState } from 'react';
import { useCreateAction } from '../../api/use-create';
import { HelpTip } from '../../ui/help-tip';
import { SavedReplyTrash } from '../settings/saved-reply-panels';
import { kbApi } from './kb-api';
import { kbMessages } from './kb-messages';
import { KBDialog } from './kb-ui';
import { languageName, translationMessages } from './translation-messages';

// KBTrash holds the deleted articles and collections, each restorable for 30 days.
export function KBTrash({ timezone, onChanged, onClose }: { timezone: string; onChanged: () => void; onClose: () => void }) {
  const [now] = useState(() => new Date());
  // A translation's id here is "<article id>/<locale>"; one press is one restore (Idempotency-Key).
  const undeleteTranslation = useCreateAction(({ id, updatedAt }: { id: string; updatedAt: string }, key: string) => {
    const [articleId, locale] = id.split('/');
    return kbApi.restoreTranslation(articleId!, locale!, updatedAt, key);
  });
  return (
    <KBDialog title={kbMessages.trash} className="kb-trash" phone="full" closeLabel={kbMessages.closeTrash} onClose={onClose}>
      <p className="muted-copy">{kbMessages.trashLead} <HelpTip label={kbMessages.trashLabel} text={kbMessages.trashTip} /></p>
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.articleTrash} description={kbMessages.articleTrashDescription}
        empty={kbMessages.articleTrashEmpty} failedMessage={kbMessages.articleActionFailed} text={() => ''}
        load={(cursor) => kbApi.trashedArticles(cursor)}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreArticle(id, updatedAt); onChanged(); }} />
      <SavedReplyTrash timezone={timezone} now={now} heading={kbMessages.collectionTrash} description={kbMessages.collectionTrashDescription}
        empty={kbMessages.collectionTrashEmpty} failedMessage={kbMessages.collectionActionFailed} text={(item) => item.description}
        load={async () => ({ items: (await kbApi.trashedCollections()).items.map((item) => ({ ...item, title: item.name })) })}
        onUndelete={async (id, updatedAt) => { await kbApi.restoreCollection(id, updatedAt); onChanged(); }} />
      <SavedReplyTrash timezone={timezone} now={now} heading={translationMessages.translationTrash} description={translationMessages.translationTrashDescription}
        empty={translationMessages.translationTrashEmpty} failedMessage={kbMessages.articleActionFailed} text={() => ''}
        load={async (cursor) => {
          const page = await kbApi.trashedTranslations(cursor);
          return { ...page, items: page.items.map((item) => ({ ...item, id: `${item.article_id}/${item.locale}`, title: `${item.title} · ${languageName(item.locale)} · ${item.article_title}` })) };
        }}
        onUndelete={async (id, updatedAt) => { await undeleteTranslation.run({ id, updatedAt }); onChanged(); }} />
    </KBDialog>
  );
}
