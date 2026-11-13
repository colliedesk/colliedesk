import { useId } from 'react';
import type { ReactNode } from 'react';
import { messages } from '../../messages';
import { ChatIcon, FormIcon, MailIcon, UserPlusIcon } from '../../ui/icons';
import type { SidebarChannel, SidebarView } from './sidebar-items';

function ChannelIcon({ type }: { type: string }) {
  if (type === 'email') return <MailIcon />;
  if (type === 'web_form') return <FormIcon />;
  return <ChatIcon />;
}

// SidebarSection is a titled list of the sidebar: the saved views or the channels. An empty one is not shown.
function SidebarSection({ title, children }: { title: string; children: ReactNode[] }) {
  const id = useId();
  if (children.length === 0) return null;
  return <div className="sidebar-section">
    <h2 id={id} className="sidebar-section-title">{title}</h2>
    <ul aria-labelledby={id}>{children}</ul>
  </div>;
}

// SidebarLists are the saved views (with the dot of what each holds) and the channels (with a warning dot for one
// that needs attention, said in its name too), each a link to its inbox list.
export function SidebarLists({ views = [], channels = [] }: { views?: SidebarView[]; channels?: SidebarChannel[] }) {
  return <>
    <SidebarSection title={messages.savedViewsTitle}>{views.map((view) => <li key={view.key}>
      <a href={view.href} className="rail-link" aria-current={view.current ? 'page' : undefined}>
        <span className="view-mark" data-mark={view.mark} aria-hidden="true" /><span className="rail-label">{view.label}</span></a></li>)}</SidebarSection>
    <SidebarSection title={messages.channels}>{channels.map((channel) => <li key={channel.key}>
      <a href={channel.href} className="rail-link" aria-current={channel.current ? 'page' : undefined}
        aria-label={channel.warn ? messages.channelNeedsAttention(channel.label) : undefined}>
        <ChannelIcon type={channel.type} /><span className="rail-label">{channel.label}</span>
        {channel.warn && <span className="channel-warn" aria-hidden="true" />}</a></li>)}</SidebarSection>
  </>;
}

// InviteLink opens Members, where invites are made; only for someone who may invite.
export function InviteLink() {
  return <a href="/settings/members" className="rail-link sidebar-invite" data-tip={messages.inviteTeammates}>
    <UserPlusIcon /><span className="rail-label">{messages.inviteTeammates}</span></a>;
}
