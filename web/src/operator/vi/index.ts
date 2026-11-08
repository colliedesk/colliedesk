import { fields, sections } from '../fields';
import { lazyWords } from '../lang';
import { m } from '../messages';
import { am as viAm } from './ai-provider-messages';
import { cloudMessages as viCloud } from './cloud-messages';
import { fields as viFields, sections as viSections } from './fields';
import { m as viM } from './messages';

// apply rewrites the console's English strings in Vietnamese, in place, before the first render (lang.ts).
export function apply() {
  Object.assign(m, viM);
  lazyWords.am = viAm;
  lazyWords.cloud = viCloud;
  for (const [key, words] of Object.entries(viFields)) if (fields[key]) Object.assign(fields[key], words);
  for (const [group, list] of Object.entries(viSections)) list.forEach((words, at) => Object.assign(sections[group]?.[at] ?? {}, words));
  document.documentElement.lang = 'vi';
  document.title = `${m.consoleName} · Colliedesk`;
}
