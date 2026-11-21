import { useId, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { useHolds } from '../../lib/permissions-context';
import { kbApi } from './kb-api';
import { filtersBeyondCollection, nextDay, previousDay } from './knowledge-search';
import type { KnowledgeSearch } from './knowledge-search';
import { ListMenu } from './list-menu';
import { listMessages as text } from './list-messages';
import { languageName } from './translation-messages';

interface ListFiltersProps {
  search: KnowledgeSearch;
  onChange: (change: Partial<KnowledgeSearch>) => void;
}

type DateKey = 'created_after' | 'created_before' | 'updated_after' | 'updated_before';

// The filters beyond the search box, status and visibility: tags, authors, dates and translations. Their
// lists load when the menu opens, or at once when the address already holds one of them.
export function ListFilters({ search, onChange }: ListFiltersProps) {
  const [opened, setOpened] = useState(false);
  const id = useId();
  const holds = useHolds();
  const tags = useQuery({ queryKey: ['kb-tags'], queryFn: kbApi.tags, enabled: opened || Boolean(search.tag) });
  const team = useQuery({ queryKey: ['agents'], queryFn: api.agents, enabled: holds('team:read') && (opened || Boolean(search.author)) });
  const languages = useQuery({ queryKey: ['kb-languages'], queryFn: kbApi.languages, enabled: holds('help_center:read') && (opened || Boolean(search.translation)) });
  const chosenTags = search.tag ?? [];
  const chosenAuthors = search.author ?? [];
  const toggle = (key: 'tag' | 'author', value: string, on: boolean) => {
    const values = (search[key] ?? []).filter((item) => item !== value);
    if (on) values.push(value);
    onChange({ [key]: values.length > 0 ? values : undefined, ...(key === 'tag' && values.length === 0 ? { tag_match: undefined } : {}) });
  };
  // A tag or author in the address that the list no longer has stays listed, so it can be unticked.
  const tagCounts = new Map((tags.data?.items ?? []).map((item) => [item.tag, item.article_count]));
  const offeredTags = [...tagCounts.keys(), ...chosenTags.filter((tag) => !tagCounts.has(tag))];
  const people = (team.data?.items ?? []).filter((agent) => !agent.deactivated_at || chosenAuthors.includes(agent.id));
  const known = new Set(people.map((agent) => agent.id));
  const others = (languages.data?.languages ?? []).map((item) => item.locale);
  const count = Object.keys(search).filter((key) => ['tag', 'author', 'subcollections', 'created_after', 'created_before', 'updated_after', 'updated_before', 'translation'].includes(key)).length;
  const date = (key: DateKey, label: string) => {
    const before = key.endsWith('_before');
    const value = search[key];
    return (
      <label className="kb-filter-date"><span>{label}</span>
        <input type="date" value={value ? (before ? previousDay(value) : value) : ''}
          onChange={(event) => { const day = event.currentTarget.value; onChange({ [key]: day ? (before ? nextDay(day) : day) : undefined }); }} />
      </label>
    );
  };
  return (
    <ListMenu label={text.filters} count={count} countLabel={text.filtersOn(count)} className="kb-filters-menu" onOpen={() => setOpened(true)}>
      {search.collection && <label className="check-row"><input type="checkbox" checked={search.subcollections === 'true'}
        onChange={(event) => onChange({ subcollections: event.currentTarget.checked ? 'true' : undefined })} /><span>{text.subcollections}</span></label>}
      <fieldset><legend>{text.tags}</legend>
        {tags.isSuccess && offeredTags.length === 0 && <p className="filter-hint">{text.noTags}</p>}
        {offeredTags.map((tag) => <label key={tag} className="check-row"><input type="checkbox" checked={chosenTags.includes(tag)}
          onChange={(event) => toggle('tag', tag, event.currentTarget.checked)} /><span>{text.tagCount(tag, tagCounts.get(tag) ?? 0)}</span></label>)}
      </fieldset>
      {chosenTags.length > 0 && <fieldset><legend>{text.tagMatch}</legend>
        <label className="check-row"><input type="radio" name="kb-tag-match" checked={search.tag_match !== 'all'} onChange={() => onChange({ tag_match: undefined })} /><span>{text.anyTag}</span></label>
        <label className="check-row"><input type="radio" name="kb-tag-match" checked={search.tag_match === 'all'} onChange={() => onChange({ tag_match: 'all' })} /><span>{text.allTags}</span></label>
      </fieldset>}
      {holds('team:read') && <fieldset><legend>{text.authors}</legend>
        {people.map((agent) => <label key={agent.id} className="check-row"><input type="checkbox" checked={chosenAuthors.includes(agent.id)}
          onChange={(event) => toggle('author', agent.id, event.currentTarget.checked)} /><span>{agent.display_name || agent.email}</span></label>)}
        {chosenAuthors.filter((id) => team.isSuccess && !known.has(id)).map((id) => <label key={id} className="check-row"><input type="checkbox" checked
          onChange={() => toggle('author', id, false)} /><span>{id}</span></label>)}
      </fieldset>}
      <fieldset className="kb-filter-dates"><legend>{text.created}</legend>{date('created_after', text.from(text.created))}{date('created_before', text.to(text.created))}</fieldset>
      <fieldset className="kb-filter-dates"><legend>{text.updated}</legend>{date('updated_after', text.from(text.updated))}{date('updated_before', text.to(text.updated))}</fieldset>
      <label className="kb-filter-select" htmlFor={`${id}-translation`}>{text.translation}</label>
        <select id={`${id}-translation`} className="kb-filter-select" value={search.translation ?? ''} onChange={(event) => {
          const value = event.currentTarget.value;
          onChange({ translation: value === 'missing' || value === 'outdated' ? value : undefined, locale: undefined });
        }}>
          <option value="">{text.anyTranslation}</option>
          <option value="missing">{text.missing}</option>
          <option value="outdated">{text.outdated}</option>
        </select>
      {search.translation && others.length > 0 && <>
        <label className="kb-filter-select" htmlFor={`${id}-locale`}>{text.language}</label>
        <select id={`${id}-locale`} className="kb-filter-select" value={search.locale ?? ''} onChange={(event) => onChange({ locale: event.currentTarget.value || undefined })}>
          <option value="">{text.anyLanguage}</option>
          {others.map((locale) => <option key={locale} value={locale}>{languageName(locale)}</option>)}
        </select>
      </>}
      {filtersBeyondCollection(search) && <button type="button" className="link-action kb-filter-clear" onClick={() => onChange(clearedOf(search))}>{text.clearFilters}</button>}
    </ListMenu>
  );
}

// clearedOf leaves only the collection open in the tree: every other key of the address is set to undefined.
function clearedOf(search: KnowledgeSearch): Partial<KnowledgeSearch> {
  return Object.fromEntries(Object.keys(search).filter((key) => key !== 'collection').map((key) => [key, undefined]));
}
