import { useId } from 'react';
import { HelpTip } from '../../ui/help-tip';
import type { KBCollection } from './kb-api';
import { kbMessages } from './kb-messages';

type Order = KBCollection['article_order'];
const orders: Order[] = ['manual', 'title', 'newest', 'updated'];

// CollectionOrderField picks how a collection lists its articles, in the help center and in this list.
// Every order but By hand sorts by itself, so the buttons that move an article go away.
export function CollectionOrderField({ value, onChange }: { value: Order; onChange: (order: Order) => void }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>{kbMessages.articleOrder}</label>
    <select id={id} value={value} aria-describedby={`${id}-tip`} onChange={(event) => onChange(event.target.value as Order)}>
      {orders.map((order) => <option key={order} value={order}>{kbMessages.articleOrders[order]}</option>)}
    </select>
    <small className="muted-copy"><HelpTip label={kbMessages.articleOrder} text={kbMessages.articleOrderTip} id={`${id}-tip`} /></small></div>;
}
