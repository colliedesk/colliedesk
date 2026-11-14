import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import type { AIProvider, AISlot, AISlotInput, AISlotName } from '../../api/ai';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import { HelpTip } from '../../ui/help-tip';
import { messages } from '../../messages';
import { aiMessages as m } from './ai-messages';
import { aiChatMessages as chatMessages } from './ai-chat-messages';
import { AIChatPanel } from './ai-chat-panel';
import { AutoDraftDialog, AutoTriageDialog } from './ai-level-dialogs';
import { SettingsNav } from './settings-nav';
import { slotInput } from './ai-slot-input';
import { PlusIcon } from './ai-icons';
import { Features } from './ai-features';
import { Usage, recentMonths } from './ai-usage-panel';
import { Providers } from './ai-providers-panel';
import { History } from './ai-history-panel';
import { AddDialog, DeleteDialog, KeyDialog } from './ai-provider-dialogs';
import { AutomaticDialog } from './ai-automatic-dialog';
import type { AIPageProps, AITab } from './ai-page-props';
import { settingsMessages } from './settings-messages';
import './ai-settings.css';

export { recentMonths };
export type { AIPageProps, AITab, FieldError } from './ai-page-props';

// focusSwitch puts focus back on a feature's default level once its dialog closes (the dialog is gone
// from the page, so the browser would leave it on the body).
const focusSwitch = (slot: AISlotName) => requestAnimationFrame(() => document.querySelector<HTMLInputElement>(`#ai-auto-${slot} input:checked`)?.focus());

// useFocusAfterSave puts focus back on an automatic mode's switch once the change it started is saved:
// the row is drawn again meanwhile, and focus would otherwise land on the body.
function useFocusAfterSave(busySlot: AISlotName | undefined) {
  const pending = useRef<AISlotName>(undefined);
  useEffect(() => {
    if (busySlot === undefined && pending.current) {
      focusSwitch(pending.current);
      pending.current = undefined;
    }
  }, [busySlot]);
  return (slot: AISlotName) => { pending.current = slot; };
}

// BuiltInPanel is the built-in AI as the panel of its tab; where the page has no tabs it adds no element.
function BuiltInPanel({ panel, children }: { panel?: { id: string; label: string }; children: ReactNode }) {
  return panel ? <div role="tabpanel" id={panel.id} aria-labelledby={panel.label} className="ai-view-panel">{children}</div> : <>{children}</>;
}

export function AIPage(props: AIPageProps) {
  const { settings, isAdmin } = props;
  const focusAfterSave = useFocusAfterSave(props.busySlot);
  const setSlot = (slot: AISlotName, input: AISlotInput) => { focusAfterSave(slot); props.onSetSlot(slot, input); };
  const [tab, setTab] = useState<AITab>(props.initialTab ?? 'features');
  const [dialog, setDialog] = useState<{ kind: 'add' } | { kind: 'automatic' } | { kind: 'auto-triage' | 'auto-draft'; slot: AISlot } | { kind: 'key' | 'delete'; provider: AIProvider }>();
  const ids = { tabs: useId(), views: useId() };
  const full = settings ? settings.providers.length >= settings.provider_limit : false;
  // With no provider at all the Features tab carries its own Add provider in a notice, so the tab row does not repeat it.
  const noProvider = Boolean(settings) && settings?.providers.length === 0 && !settings?.server;
  const tabs: { id: AITab; label: string; count?: number }[] = [
    { id: 'features', label: m.tabFeatures }, { id: 'providers', label: m.tabProviders, count: settings ? settings.providers.length + (settings.server ? 1 : 0) : undefined },
    { id: 'chat', label: chatMessages.tab }, ...(isAdmin ? [{ id: 'usage' as const, label: m.tabUsage }] : []), { id: 'history', label: m.tabHistory },
  ];
  const [month, setMonth] = useState(() => recentMonths()[0]);
  const show = (next: AITab) => { setTab(next); if (next === 'usage') props.onUsageMonth(month); };
  const openAdd = () => { setTab('providers'); setDialog({ kind: 'add' }); };
  // Two views: the built-in AI, and the agents that work through a token. A person who may not read the AI
  // settings has the agents alone, and a page given no agents has the built-in AI alone: neither has a tab row.
  const views = props.builtIn !== false && props.agents !== undefined;
  // An address that names the agents opens the built-in AI where there are no agents to show.
  const view = props.builtIn === false ? 'external' : views ? props.view ?? 'built-in' : 'built-in';
  const viewTabs = [{ id: 'built-in' as const, label: settingsMessages.tabBuiltIn }, { id: 'external' as const, label: settingsMessages.tabExternal }];
  return (
    <main className="settings-page panel ai-settings">
      <header><h1>{messages.aiAndAgents}</h1><p>{m.pageLead}</p></header>
      <div className="settings-layout">
        <SettingsNav page="/settings/ai" shown={props.shown} />
        <div className="settings-sections ai-page">
          {views && <Tabs label={messages.aiAndAgents} variant="underline" tabs={viewTabs} value={view} onChange={(next, how) => props.onView?.(next, how)} prefix={ids.views} />}
          {view === 'built-in' && <BuiltInPanel panel={views ? { id: panelID(ids.views, 'built-in'), label: tabID(ids.views, 'built-in') } : undefined}>
          {props.builtIn !== false && <div className="section-heading ai-intro"><div className="heading-with-tip"><h2>{m.heading}</h2><HelpTip label={m.heading} text={m.description} /></div><p>{m.lead}</p></div>}
          {props.loadFailed && <p role="alert" className="error-copy">{m.loadFailed}</p>}
          {!settings ? (!props.loadFailed && <div className="ai-loading" aria-busy="true" />) : <>
            <div className="ai-tabs-row">
              <Tabs label={m.tabs} tabs={tabs} value={tab} onChange={show} prefix={ids.tabs} />
              {isAdmin && !(tab === 'features' && noProvider) && <button type="button" className="ai-add" aria-label={m.addProvider} onClick={openAdd} disabled={full}
                aria-describedby={full ? 'ai-limit-why' : undefined}><PlusIcon /><span className="ai-wide">{m.addProvider}</span><span className="ai-narrow">{m.addShort}</span></button>}
            </div>
            {props.error && <p role="alert" className="error-copy">{props.error}</p>}
            <div role="tabpanel" id={panelID(ids.tabs, tab)} aria-labelledby={tabID(ids.tabs, tab)} className="ai-panel-stack">
              {tab === 'features' && <Features {...props} settings={settings} onOpenAdd={openAdd} onOpenAutomatic={() => setDialog({ kind: 'automatic' })}
                onConfirmAuto={(slot) => setDialog({ kind: slot.slot === 'draft' ? 'auto-draft' : 'auto-triage', slot })} onSetAuto={setSlot} />}
              {tab === 'chat' && <AIChatPanel timezone={props.timezone} onOpen={(id) => props.onOpenChat?.(id)} slot={settings.slots.find((slot) => slot.slot === 'chat')}
                onSetCap={props.onSetChatCap} isAdmin={isAdmin} serverPrices={settings.server !== null} />}
              {tab === 'usage' && <Usage usage={props.usage} failed={props.usageFailed} month={month} onMonth={(next) => { setMonth(next); props.onUsageMonth(next); }} />}
              {tab === 'providers' && <Providers {...props} settings={settings}
                onKey={(provider) => setDialog({ kind: 'key', provider })} onDelete={(provider) => setDialog({ kind: 'delete', provider })} />}
              {tab === 'history' && <History events={settings.events} timezone={props.timezone} />}
            </div>
          </>}
          </BuiltInPanel>}
          {view === 'external' && (views ? <div role="tabpanel" id={panelID(ids.views, view)} aria-labelledby={tabID(ids.views, view)} className="ai-view-panel">{props.agents}</div> : props.agents)}
        </div>
      </div>
      {dialog?.kind === 'add' && settings && <AddDialog settings={settings} onClose={() => setDialog(undefined)} onAdd={props.onAdd} onListModels={props.onListModels} />}
      {dialog?.kind === 'key' && <KeyDialog provider={dialog.provider} onClose={() => setDialog(undefined)} onSetKey={props.onSetKey} />}
      {dialog?.kind === 'automatic' && settings && <AutomaticDialog settings={settings} onClose={() => setDialog(undefined)} onSave={props.onSetAutomatic} />}
      {dialog?.kind === 'delete' && settings && <DeleteDialog provider={dialog.provider} settings={settings} onClose={() => setDialog(undefined)} onRemove={props.onRemove} />}
      {dialog?.kind === 'auto-draft' && settings && <AutoDraftDialog slot={dialog.slot} settings={settings} onClose={() => { setDialog(undefined); focusSwitch('draft'); }}
        onTurnOn={() => { setDialog(undefined); setSlot('draft', { ...slotInput(dialog.slot), default_level: 'suggest' }); }} />}
      {dialog?.kind === 'auto-triage' && <AutoTriageDialog slot={dialog.slot} onClose={() => { setDialog(undefined); focusSwitch('triage'); }}
        onTurnOn={() => { setDialog(undefined); setSlot('triage', { ...slotInput(dialog.slot), default_level: 'auto' }); }} />}
    </main>
  );
}
