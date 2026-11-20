import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useCreateAction } from '../../api/use-create';
import { formatDate } from '../../lib/format-date';
import { useHolds } from '../../lib/permissions-context';
import { savedReplyChangeText } from '../../lib/saved-reply-change';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { SavedReplyHistory, SavedReplyTrash } from '../settings/saved-reply-panels';
import { settingsMessages } from '../settings/settings-messages';
import { kbMessages } from './kb-messages';
import { reason } from './kb-tree';
import { kbTemplatesApi, templatesKey } from './kb-templates-api';
import type { KBTemplate, KBTemplateInput, KBTemplateVersion, TrashedKBTemplate } from './kb-templates-api';
import { KBHead } from './kb-ui';
import { TemplateForm } from './template-form';
import { templateMessages as t } from './template-messages';

// TemplatesRoute lists the article templates, and adds, edits, deletes and restores them.
export function TemplatesRoute() {
  const client = useQueryClient();
  const holds = useHolds();
  const canChange = holds('kb_articles:update');
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const timezone = me.data?.timezone ?? 'UTC';
  const templates = useQuery({ queryKey: templatesKey, queryFn: kbTemplatesApi.list });
  // One thing open at a time: the new form ('new'), a template's form or history (its id), or the trash.
  const [open, setOpen] = useState<{ what: 'new' | 'edit' | 'history' | 'delete' | 'trash'; id?: string } | null>(null);
  const [problem, setProblem] = useState<string>();
  const [now] = useState(() => new Date());
  // A cached template would start a new article from its old text; a trashed one no longer is any collection's default.
  const reload = () => Promise.all([client.invalidateQueries({ queryKey: templatesKey }), client.removeQueries({ queryKey: ['kb-template'] }),
    client.invalidateQueries({ queryKey: ['kb-collections'] })]);
  // A press is one create, one undelete or one restore (Idempotency-Key), even when it is retried.
  const create = useCreateAction((value: KBTemplateInput, key: string) => kbTemplatesApi.create(value, key));
  const undelete = useCreateAction(({ id, updatedAt }: { id: string; updatedAt: string }, key: string) => kbTemplatesApi.restore(id, updatedAt, key));
  const restoreVersion = useCreateAction(({ id, version, updatedAt }: { id: string; version: number; updatedAt: string }, key: string) =>
    kbTemplatesApi.restoreVersion(id, version, updatedAt, key));
  const close = () => { setOpen(null); setProblem(undefined); };
  const remove = async (template: KBTemplate) => {
    try {
      await kbTemplatesApi.delete(template.id, template.updated_at);
      close();
      await reload();
    } catch (error) {
      setProblem(reason(error, t.actionFailed));
    }
  };
  const items = templates.data?.items ?? [];
  return (
    <main className="kb-frame kb-languages-page kb-templates-page" aria-labelledby="kb-heading">
      <KBHead current="templates" heading={kbMessages.heading} actions={canChange && <>
        <button type="button" aria-expanded={open?.what === 'trash'} onClick={() => (open?.what === 'trash' ? close() : setOpen({ what: 'trash' }))}>{t.trash}</button>
        <button type="button" className="primary-action" onClick={() => { setProblem(undefined); setOpen({ what: 'new' }); }}>{t.newTemplate}</button>
      </>} />
      <div className="kb-languages-scroll">
        <section className="kb-languages kb-templates" aria-labelledby="kb-templates-heading">
          <h2 id="kb-templates-heading">{t.templatesHeading}</h2>
          <p className="muted-copy">{t.templatesLead} <HelpTip label={t.templatesHeading} text={t.templatesTip} /></p>
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          {open?.what === 'trash' && <SavedReplyTrash<TrashedKBTemplate & { title: string }> timezone={timezone} now={now} heading={t.trash} description={t.trashDescription} empty={t.trashEmpty}
            closeLabel={t.closeTrash} failedMessage={t.actionFailed} text={(item) => item.description} onClose={close}
            load={async (cursor) => { const page = await kbTemplatesApi.trash(cursor); return { ...page, items: page.items.map((item) => ({ ...item, title: item.name })) }; }}
            onUndelete={async (id, updatedAt) => { await undelete.run({ id, updatedAt }); await reload(); }} />}
          {open?.what === 'new' && <TemplateForm label={t.addTemplate} onCancel={close}
            onSave={async (value) => { await create.run(value); close(); await reload(); }} />}
          {templates.isError && <p role="alert" className="error-copy">{t.loadFailed}{' '}
            <button type="button" className="link-action" onClick={() => void templates.refetch()}>{kbMessages.retry}</button></p>}
          {templates.isPending && <p role="status" className="muted-copy">{messages.loading}</p>}
          {templates.isSuccess && items.length === 0 && <p className="empty-copy">{t.noTemplates}</p>}
          {items.length > 0 && <ul className="kb-template-list" aria-label={t.templatesHeading}>{items.map((template) => (
            <li key={template.id}>
              {open?.what === 'edit' && open.id === template.id
                ? <TemplateForm label={t.editTemplateName(template.name)} initial={template} onCancel={close}
                    onSave={async (value) => { await kbTemplatesApi.update(template.id, { ...value, updated_at: template.updated_at }); close(); await reload(); }} />
                : <TemplateRow template={template} timezone={timezone} canChange={canChange} confirming={open?.what === 'delete' && open.id === template.id}
                    historyOpen={open?.what === 'history' && open.id === template.id}
                    onOpen={(what) => { setProblem(undefined); setOpen(open?.what === what && open.id === template.id ? null : { what, id: template.id }); }}
                    onDelete={() => void remove(template)} onCancel={close} />}
              {open?.what === 'history' && open.id === template.id && <SavedReplyHistory<KBTemplateVersion & { title: string }> title={template.name} timezone={timezone} onClose={close}
                failedMessage={t.actionFailed} text={(version) => version.body}
                load={async (before) => { const page = await kbTemplatesApi.versions(template.id, before); return { ...page, items: page.items.map((item) => ({ ...item, title: item.name })) }; }}
                onRestore={async (version) => { await restoreVersion.run({ id: template.id, version, updatedAt: template.updated_at }); await reload(); }} />}
            </li>
          ))}</ul>}
        </section>
      </div>
    </main>
  );
}

interface RowProps {
  template: KBTemplate;
  timezone: string;
  canChange: boolean;
  confirming: boolean;
  historyOpen: boolean;
  onOpen: (what: 'edit' | 'history' | 'delete') => void;
  onDelete: () => void;
  onCancel: () => void;
}

function TemplateRow({ template, timezone, canChange, confirming, historyOpen, onOpen, onDelete, onCancel }: RowProps) {
  return <>
    <div className="kb-template-text">
      <strong>{template.name}</strong>
      {template.description && <span>{template.description}</span>}
      {template.last_change && <small className="muted-copy">{savedReplyChangeText(template.last_change)} · <time dateTime={template.last_change.created_at}>{formatDate(template.last_change.created_at, timezone)}</time></small>}
    </div>
    {confirming
      ? <div role="group" aria-label={t.deleteTemplateName(template.name)} className="kb-template-actions">
          <span>{t.confirmDelete(template.name)}</span>
          <button type="button" className="danger-action" onClick={onDelete}>{settingsMessages.confirmDelete}</button>
          <button type="button" onClick={onCancel}>{messages.cancel}</button>
        </div>
      : <div className="kb-template-actions">
          <button type="button" aria-label={t.historyOf(template.name)} aria-expanded={historyOpen} onClick={() => onOpen('history')}>{settingsMessages.history}</button>
          {canChange && <button type="button" aria-label={t.editTemplateName(template.name)} onClick={() => onOpen('edit')}>{messages.edit}</button>}
          {canChange && <button type="button" className="danger-action" aria-label={t.deleteTemplateName(template.name)} onClick={() => onOpen('delete')}>{settingsMessages.confirmDelete}</button>}
        </div>}
  </>;
}
