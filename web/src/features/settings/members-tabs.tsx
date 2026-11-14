import { useId } from 'react';
import type { ReactNode } from 'react';
import { panelID, tabID, Tabs } from '../../ui/tabs';
import type { MembersTab } from './settings-anchors';
import { settingsMessages } from './settings-messages';
import { ComingCard } from './coming-card';

interface MembersTabsProps {
  tab: MembersTab;
  onTab: (tab: MembersTab) => void;
  people: ReactNode;
  invites: ReactNode;
  advanced: ReactNode;
  // How many people and pending invites the tabs count, once known.
  counts: { people?: number; invites?: number };
}

// MembersTabs is the page's four views: the people, the invites that add to them, the reserved Groups, and the
// roles under Advanced. One panel is drawn at a time.
export function MembersTabs({ tab, onTab, people, invites, advanced, counts }: MembersTabsProps) {
  const prefix = useId();
  const tabs = [
    { id: 'people' as const, label: settingsMessages.tabPeople, count: counts.people }, { id: 'invites' as const, label: settingsMessages.tabInvites, count: counts.invites },
    { id: 'groups' as const, label: settingsMessages.tabGroups, count: settingsMessages.coming }, { id: 'advanced' as const, label: settingsMessages.tabAdvanced },
  ];
  return <>
    <Tabs label={settingsMessages.membersTabs} variant="underline" tabs={tabs} value={tab} onChange={onTab} prefix={prefix} />
    <div role="tabpanel" id={panelID(prefix, tab)} aria-labelledby={tabID(prefix, tab)} className="settings-tab-panel">
      {tab === 'people' && people}
      {tab === 'invites' && invites}
      {tab === 'groups' && <ComingCard title={settingsMessages.groupsComingTitle} body={settingsMessages.groupsComingBody} issue={442} />}
      {tab === 'advanced' && advanced}
    </div>
  </>;
}
