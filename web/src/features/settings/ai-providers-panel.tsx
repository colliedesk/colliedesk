import type { AIProvider, AISettings } from '../../api/ai';
import { formatDate } from '../../lib/format-date';
import { aiMessages as m, slotLabel } from './ai-messages';
import { KeyIcon, TrashIcon } from './ai-icons';
import type { AIPageProps } from './ai-page-props';

function checkWhy(code: string | null) {
  return (code && m.checkWhy[code]) || m.checkWhyOther;
}

export function Providers({ settings, isAdmin, timezone, busyProvider, onCheck, onKey, onDelete }: AIPageProps & { settings: AISettings; onKey: (p: AIProvider) => void; onDelete: (p: AIProvider) => void }) {
  const left = settings.provider_limit - settings.providers.length;
  return (
    <section className="panel ai-providers" aria-label={m.tabProviders}>
      {settings.server && <div className="ai-prov">
        <div className="ai-prov-name"><strong>{settings.server.label} <span className="badge">{m.serverProviderBadge}</span></strong>
          <span>{settings.server.kind === 'anthropic' ? m.kindAnthropic : m.kindOpenAI}</span></div>
        <div className="ai-prov-meta"><span>{m.serverSetBy}</span>
          <span>{m.usedBy(settings.slots.filter((slot) => slot.source === 'server').map((slot) => slotLabel(slot.slot)).join(', '))}</span></div>
        <div className="ai-prov-state" />
        <div className="ai-prov-actions" />
      </div>}
      {settings.providers.length === 0 && !settings.server && <p className="ai-prov-empty muted-copy">{m.noProviders}</p>}
      {settings.providers.map((provider) => {
        const checking = busyProvider === provider.id;
        const when = formatDate(provider.checked_at, timezone);
        return <div className="ai-prov" key={provider.id}>
          <div className="ai-prov-name"><strong>{provider.label}</strong>
            <span>{provider.kind === 'anthropic' ? m.kindAnthropic : m.kindOpenAI} · {provider.key_hint ? <span className="ai-mono">{m.keyHint(provider.key_hint)}</span> : m.noKey}</span></div>
          <div className="ai-prov-meta">{provider.base_url && <span className="ai-mono">{provider.base_url}</span>}
            <span>{m.usedBy(provider.used_by.map(slotLabel).join(', '))}</span></div>
          <div className="ai-prov-state" aria-live="polite">{checking ? <span className="status ai-st-off">{m.checking}</span>
            : provider.check_status === 'ok' ? <><span className="status ai-st-ok">{m.working}</span><p>{m.checked(when)}</p></>
              : <><span className="status ai-st-err">{m.notWorking}</span><p className="error-copy">{m.failedCheck(checkWhy(provider.check_error_code), when)}</p></>}</div>
          {isAdmin && <div className="ai-prov-actions">
            <button type="button" onClick={() => onCheck(provider.id)} disabled={checking}>{m.check}</button>
            <button type="button" className="ai-icon-text" onClick={() => onKey(provider)} aria-label={m.changeKey}><KeyIcon /><span>{m.changeKey}</span></button>
            <button type="button" className="danger-action ai-icon-only" onClick={() => onDelete(provider)} aria-label={m.deleteProvider(provider.label)}><TrashIcon /></button>
          </div>}
        </div>;
      })}
      <p className="ai-foot" id="ai-limit-why">{left > 0 ? <span>{m.providersFoot(left)}</span> : <><strong>{m.providersFull(settings.provider_limit)}</strong> <span>{m.providersFullHelp}</span></>}
        <span>{m.keysFoot}</span></p>
    </section>
  );
}
