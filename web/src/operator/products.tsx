import { useCallback, useEffect, useState } from 'react';
import { ApiError, api } from './api';
import type { Product } from './api';
import { ConsoleDialog } from './dialog';
import { m } from './messages';

const plans: Array<Product['plan']> = ['team', 'founding', 'solo', 'small', 'team25'];
const periods: Array<Product['period']> = ['monthly', 'yearly', 'single'];

// ProductsSection lists the Polar products on the billing page. Products are add-only: a row is never
// removed nor changed, only put on or taken off sale (one per plan and period is on sale).
export default function ProductsSection({ totp }: { totp: boolean }) {
  const [items, setItems] = useState<Product[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [dialog, setDialog] = useState<{ add: true } | { product: Product } | null>(null);
  const load = useCallback(async () => {
    try {
      setItems((await api.products()).items);
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  return <section className="settings-card">
    <div className="section-heading"><h2>{m.productsTitle}</h2><p>{m.productsLead}</p>
      <button type="button" onClick={() => setDialog({ add: true })}>{m.addProduct}</button></div>
    <div className="settings-body ops-table-wrap" tabIndex={0} role="region" aria-label={m.productsTitle}>
      {failed && <p className="error-copy" role="alert">{m.loadError} <button type="button" className="link-button" onClick={() => void load()}>{m.tryAgain}</button></p>}
      {items === null && !failed && <div className="skeleton" aria-busy="true" />}
      {items?.length === 0 && <p className="help">{m.noProducts}</p>}
      {items && items.length > 0 && <table className="ops-table">
        <thead><tr><th scope="col">{m.productId}</th><th scope="col">{m.plan}</th><th scope="col">{m.period}</th><th scope="col">{m.onSale}</th><th scope="col"><span className="sr-only">{m.onSale}</span></th></tr></thead>
        <tbody>{items.map((product) => <tr key={product.product_id}>
          <td><code>{product.product_id}</code></td><td>{product.plan}</td><td>{product.period}</td>
          <td>{product.on_sale ? <span className="badge" data-tone="ok">{m.on}</span> : <span className="badge">{m.off}</span>}</td>
          <td><button type="button" onClick={() => setDialog({ product })}>{product.on_sale ? m.takeOffSale : m.putOnSale}</button></td>
        </tr>)}</tbody>
      </table>}
    </div>
    {dialog && <ProductDialog dialog={dialog} totp={totp} onClose={() => setDialog(null)} onDone={() => { setDialog(null); void load(); }} />}
  </section>;
}

function ProductDialog({ dialog, totp, onClose, onDone }: { dialog: { add: true } | { product: Product }; totp: boolean; onClose: () => void; onDone: () => void }) {
  const adding = 'add' in dialog;
  const [id, setId] = useState('');
  const [plan, setPlan] = useState<Product['plan']>('team');
  const [period, setPeriod] = useState<Product['period']>('monthly');
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const ready = (!adding || id.trim() !== '') && (!totp || /^\d{6}$/.test(code));
  const submit = async () => {
    if (!ready || busy) return;
    setBusy(true);
    setError('');
    const reauth = totp ? code : undefined;
    try {
      if (adding) await api.addProduct({ product_id: id.trim(), plan, period, on_sale: true, reauth_code: reauth });
      else await api.setOnSale(dialog.product.product_id, { on_sale: !dialog.product.on_sale, reauth_code: reauth });
      onDone();
    } catch (failure) {
      setError(failure instanceof ApiError && failure.code === 'product_exists' ? m.productExists : failure instanceof ApiError && failure.code === 'reauth_code_invalid' ? m.codeNoMatch : m.nothingSaved);
      setCode('');
    } finally {
      setBusy(false);
    }
  };
  const title = adding ? m.addProduct : dialog.product.on_sale ? m.takeOffSale : m.putOnSale;
  return <ConsoleDialog title={title} help={adding ? m.productsLead : dialog.product.product_id} onClose={onClose} closeLabel={m.cancel} onSubmit={() => void submit()}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={!ready || busy}>{title}</button></>}>
    {adding && <>
      <div className="field"><label className="field-label" htmlFor="product-id">{m.productId}</label>
        <input id="product-id" autoComplete="off" spellCheck={false} value={id} onChange={(event) => setId(event.target.value)} /></div>
      <div className="field"><label className="field-label" htmlFor="product-plan">{m.plan}</label>
        <select id="product-plan" value={plan} onChange={(event) => setPlan(event.target.value as Product['plan'])}>{plans.map((value) => <option key={value} value={value}>{value}</option>)}</select></div>
      <div className="field"><label className="field-label" htmlFor="product-period">{m.period}</label>
        <select id="product-period" value={period} onChange={(event) => setPeriod(event.target.value as Product['period'])}>{periods.map((value) => <option key={value} value={value}>{value}</option>)}</select></div>
    </>}
    {totp && <div className="field"><label className="field-label" htmlFor="product-code">{m.confirmItsYou}: {m.authenticatorCode}</label>
      <input id="product-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} spellCheck={false} value={code}
        onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} /></div>}
    {error && <p className="error-copy" role="alert">{error}</p>}
  </ConsoleDialog>;
}
