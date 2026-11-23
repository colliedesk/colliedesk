import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { can } from '../../lib/can';
import { messages } from '../../messages';
import { useSaveFeedback } from '../settings/save-feedback';
import { Icon } from './editor/icons';
import { HelpCenterAnalyticsSection } from './help-center-analytics';
import { Brand } from './help-center-card';
import { HelpCenterDomainSection } from './help-center-domain';
import { HelpCenterSearchConsoleSection } from './help-center-search-console';
import { HelpCenterShowTags } from './help-center-show-tags';
import { HelpCenterTarget } from './help-center-target';
import { helpCenterName, helpCentersApi, helpCentersKey } from './help-centers-api';
import type { HelpCenterSummary } from './help-centers-api';
import { helpCentersMessages as text } from './help-centers-messages';
import { kbMessages } from './kb-messages';
import { KBHead } from './kb-ui';
import { reason } from './kb-tree';
import { LanguagesSection } from './languages-page';
import './help-center-card.css';

// Knowledge base → Settings → one help center other than the primary (/knowledge/settings/help-centers/<id>):
// the same sections as the primary's card, each reading and saving this help center's settings by its id.
// Approval of articles and the Freshdesk redirect belong to the primary alone, so they are not here.
export function HelpCenterSettingsByIdRoute({ helpCenterId }: { helpCenterId: string }) {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const list = useQuery({ queryKey: helpCentersKey, queryFn: helpCentersApi.list });
  const center = list.data?.help_centers.find((each) => each.id === helpCenterId && !each.primary);
  const canManage = me.data ? can(me.data, 'help_center:update') : false;
  const timezone = me.data?.timezone ?? 'UTC';
  const heading = center ? helpCenterName(center, text.primaryName) : kbMessages.heading;
  return (
    <main className="kb-frame kb-settings-page" aria-labelledby="kb-heading">
      <KBHead current="settings" heading={heading} />
      <a className="kb-back" href="/knowledge/settings"><Icon name="chevronLeft" />{text.allHelpCenters}</a>
      {list.isError && <p role="alert" className="error-copy">{text.loadFailed}</p>}
      {(list.isPending || me.isPending) && <p role="status" className="muted-copy">{messages.loading}</p>}
      {list.data && !center && <p role="alert" className="error-copy">{text.notFound}</p>}
      {center && me.data && <HelpCenterTarget.Provider value={center.id}>
        <section className="settings-card" aria-labelledby="help-center-general-heading">
          <div className="section-heading"><h2 id="help-center-general-heading">{text.general}</h2><p>{text.settingsDescription}</p></div>
          <div className="settings-body"><General center={center} canManage={canManage} workspaceName={me.data.workspace.name} /></div>
        </section>
        <section className="settings-card" aria-label={text.settingsOf(heading)}>
          <div className="settings-body">
            <HelpCenterDomainSection canManage={canManage} timezone={timezone} />
            <HelpCenterSearchConsoleSection canManage={canManage} />
            <HelpCenterAnalyticsSection canManage={canManage} timezone={timezone} />
            <LanguagesSection />
            <Brand canManage={canManage} />
          </div>
        </section>
      </HelpCenterTarget.Provider>}
    </main>
  );
}

// General is whether the help center is public, its address and its title, saved on it by id.
function General({ center, canManage, workspaceName }: { center: HelpCenterSummary; canManage: boolean; workspaceName: string }) {
  const client = useQueryClient();
  const refresh = () => client.invalidateQueries({ queryKey: helpCentersKey });
  const save = useMutation({
    mutationFn: (value: { enabled: boolean; title: string }) => helpCentersApi.update(center.id, value),
    onSuccess: refresh,
  });
  const [enabled, setEnabled] = useState(center.enabled);
  const [title, setTitle] = useState(center.title);
  useEffect(() => setEnabled(center.enabled), [center.enabled]);
  useEffect(() => setTitle(center.title), [center.title]);
  const feedback = useSaveFeedback({ pending: save.isPending, saved: save.isSuccess, error: save.isError ? reason(save.error, messages.settingsActionFailed) : undefined });
  return <>
    <p role="status" className="saved-state" data-on={center.enabled ? 'true' : 'false'}>{center.enabled ? kbMessages.helpCenterOn : kbMessages.helpCenterOff}</p>
    <p className="muted-copy">{kbMessages.helpCenterAddress} <a href={center.url} target="_blank" rel="noopener noreferrer">{center.url}</a></p>
    {canManage
      ? <form className="stacked-form" onSubmit={(event) => { event.preventDefault(); if (!feedback.pending) save.mutate({ title: title.trim(), enabled }); }}>
          <label className="field-inline"><input type="checkbox" checked={enabled} onChange={(event) => { feedback.touch(); setEnabled(event.target.checked); }} /><span>{kbMessages.helpCenterEnable}</span></label>
          <div className="field"><label htmlFor="help-center-title">{kbMessages.helpCenterTitle}</label>
            <input id="help-center-title" value={title} maxLength={200} placeholder={workspaceName} aria-describedby="help-center-title-help" onChange={(event) => { feedback.touch(); setTitle(event.target.value); }} />
            <small id="help-center-title-help" className="muted-copy">{kbMessages.helpCenterTitleHelp}</small></div>
          <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(kbMessages.saveHelpCenter)}</button>
            {feedback.status}</span>
        </form>
      : <p className="muted-copy">{kbMessages.helpCenterAdminsOnly}</p>}
    <HelpCenterShowTags settings={center} canManage={canManage} saveOn={async (on) => { await helpCentersApi.update(center.id, { show_tags: on }); await refresh(); }} />
  </>;
}
