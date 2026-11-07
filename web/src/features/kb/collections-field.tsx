import { Icon } from './editor/icons';
import { kbMessages } from './kb-messages';
import { indent } from './kb-tree';
import type { TreeNode } from './kb-tree';

export interface Placement { home: string; extras: string[] }

// withHome makes home the article's home: a collection it was also listed in is no longer another one.
export const withHome = (placement: Placement, home: string): Placement => ({ home, extras: placement.extras.filter((id) => id !== home) });

interface CollectionsFieldProps {
  id: string;
  tree: TreeNode[];
  value: Placement;
  onChange: (next: Placement) => void;
  // describedBy names more hints for the home select (why the article cannot be saved yet).
  describedBy?: string;
}

// CollectionsField places an article: its home collection, which gives the breadcrumb, and the other
// collections the help center also lists it in. Its address never depends on them.
export function CollectionsField({ id, tree, value, onChange, describedBy }: CollectionsFieldProps) {
  const name = (collection: string) => tree.find((node) => node.collection.id === collection)?.collection.name ?? collection;
  const offered = tree.filter((node) => node.collection.id !== value.home && !value.extras.includes(node.collection.id));
  return (
    <div className="field kb-collections-field">
      <label htmlFor={`${id}-collection`}>{kbMessages.articleCollections}</label>
      <div className="kb-home-collection">
        <select id={`${id}-collection`} value={value.home} required aria-describedby={[`${id}-collections-help`, describedBy].filter(Boolean).join(' ')}
          onChange={(event) => onChange(withHome(value, event.target.value))}>
          {!value.home && <option value="">{kbMessages.chooseCollection}</option>}
          {tree.map((node) => <option key={node.collection.id} value={node.collection.id}>{indent(node)}</option>)}
        </select>
        {value.home && <span className="kb-home-badge" aria-hidden="true">{kbMessages.homeCollection}</span>}
      </div>
      {value.extras.length > 0 && <ul className="kb-extra-collections">
        {value.extras.map((extra) => (
          <li key={extra}><span>{name(extra)}</span>
            <button type="button" className="icon-button" aria-label={kbMessages.removeCollection(name(extra))}
              onClick={() => onChange({ ...value, extras: value.extras.filter((each) => each !== extra) })}><Icon name="close" /></button></li>
        ))}
      </ul>}
      {value.home && offered.length > 0 && <select aria-label={kbMessages.alsoListIn} value=""
        onChange={(event) => { if (event.target.value) onChange({ ...value, extras: [...value.extras, event.target.value] }); }}>
        <option value="">{kbMessages.alsoListInPlaceholder}</option>
        {offered.map((node) => <option key={node.collection.id} value={node.collection.id}>{indent(node)}</option>)}
      </select>}
      <small id={`${id}-collections-help`} className="kb-hint">{kbMessages.collectionsHelp}</small>
    </div>
  );
}
