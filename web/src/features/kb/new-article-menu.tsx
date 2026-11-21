import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { usePreloadEditor } from './editor-part';
import { Icon } from './editor/icons';
import { kbTemplatesApi, templatesKey } from './kb-templates-api';
import type { KBTemplate } from './kb-templates-api';
import { KBMenu } from './kb-ui';
import type { MenuItem } from './kb-ui';
import { templateMessages as t } from './template-messages';

// templateItems lists the templates for a menu, the collection's default first.
export function templateItems(templates: KBTemplate[], defaultId: string | null | undefined, pick: (template: KBTemplate) => void): MenuItem[] {
  const first = templates.find((template) => template.id === defaultId);
  return [...(first ? [first] : []), ...templates.filter((template) => template !== first)].map((template) => ({
    label: template.name,
    hint: template === first ? t.collectionDefault : template.description || undefined,
    onSelect: () => pick(template),
  }));
}

// NewArticleMenu starts an article blank or from a template, in the collection open on the page. Without a
// template to offer it stays one link.
export function NewArticleMenu({ collectionId, defaultTemplateId }: { collectionId?: string; defaultTemplateId?: string | null }) {
  const navigate = useNavigate();
  const preloadEditor = usePreloadEditor();
  const templates = useQuery({ queryKey: templatesKey, queryFn: kbTemplatesApi.list });
  const items = templates.data?.items ?? [];
  const start = (template: string) => void navigate({ to: '/knowledge/new', search: { ...(collectionId ? { collection: collectionId } : {}), template } });
  const label = <><Icon name="plus" />{t.newArticle}</>;
  return (
    // A pointer or the focus on New article loads the editor's chunk ahead of the click (editor-part.ts).
    <span onPointerOver={preloadEditor} onFocus={preloadEditor}>
      {items.length === 0
        ? <a className="button-link primary-action" href={`/knowledge/new${collectionId ? `?${new URLSearchParams({ collection: collectionId })}` : ''}`}>{label}</a>
        : <KBMenu label={t.newArticle} title={t.newArticle} trigger={label} buttonClassName="primary-action" describe items={[
            { label: t.blank, hint: t.blankHint, onSelect: () => start('none') },
            ...templateItems(items, defaultTemplateId, (template) => start(template.id)),
            { label: t.manageTemplates, separated: true, onSelect: () => void navigate({ to: '/knowledge/templates' }) },
          ]} />}
    </span>
  );
}
