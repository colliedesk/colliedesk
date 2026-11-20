import { useQuery } from '@tanstack/react-query';
import { Icon } from './editor/icons';
import { kbTemplatesApi, templatesKey } from './kb-templates-api';
import { KBMenu } from './kb-ui';
import { templateItems } from './new-article-menu';
import { templateMessages as t } from './template-messages';

// useTemplateStart is the text a new article starts with: the template picked in New article, else its
// collection's default; 'none' (Blank) starts it empty. A template trashed meanwhile starts it empty and says so.
export function useTemplateStart(templateId: string | undefined, collectionDefault: string | null | undefined) {
  const id = templateId === 'none' ? undefined : templateId ?? collectionDefault ?? undefined;
  const template = useQuery({ queryKey: ['kb-template', id], queryFn: () => kbTemplatesApi.get(id ?? ''), enabled: id !== undefined, retry: false });
  const missing = template.error instanceof Error && template.error.message === 'API 404';
  return {
    pending: id !== undefined && template.isPending,
    body: template.data?.body ?? '',
    notice: template.isError ? (missing ? t.inTrash : t.templateLoadFailed) : undefined,
  };
}

// InsertTemplateMenu adds a template's text where the cursor is. It is offered only when there are templates.
export function InsertTemplateMenu({ defaultTemplateId, disabled, onInsert }: { defaultTemplateId?: string | null; disabled?: boolean; onInsert: (body: string) => void }) {
  const templates = useQuery({ queryKey: templatesKey, queryFn: kbTemplatesApi.list });
  const items = templates.data?.items ?? [];
  if (items.length === 0) return null;
  return <KBMenu label={t.insertTemplate} title={t.insertTemplate} describe disabled={disabled} buttonClassName="kb-insert-template" align="end"
    trigger={<><Icon name="article" /><span className="kb-label">{t.insertTemplate}</span></>}
    items={templateItems(items, defaultTemplateId, (template) => onInsert(template.body))} />;
}
