import type { ComponentType } from 'react';
import type { Channel, ChannelCreate, ChannelDetail, ChannelList, ChannelType } from '../api/channels';
import type { LazyPart } from '../ui/lazy-part';
import { emailChannel } from './email';
import { webFormChannel } from './web_form';

// What a type's tab shows: the channel it belongs to, read fresh from GET /api/v1/channels/{id}.
export interface ChannelPanelProps {
  channel: ChannelDetail;
  canManage: boolean;
  timezone: string;
  // navigate opens another page of the app (a conversation an action started).
  navigate?: (href: string) => void;
}

// What the type's connect form gets: create posts POST /api/v1/channels for this type.
export interface ConnectProps {
  canManage: boolean;
  timezone: string;
  create: (input: Omit<ChannelCreate, 'type'>) => Promise<unknown>;
}

// What a type's home (Settings → the type's name) gets: the workspace's channels, every type.
export interface HomeProps {
  list: ChannelList;
  canManage: boolean;
  timezone: string;
  navigate: (href: string, replace?: boolean) => void;
  create: ConnectProps['create'];
}

// What a type's setup gets: the channel it sets up (none yet: the first step creates it with create).
export interface SetupProps extends HomeProps {
  channel?: Channel;
  // named: the setup adds another channel of the type, told apart by its name.
  named?: boolean;
}

export interface ChannelPanels {
  // One component per tab id the type declares.
  Tabs: Record<string, ComponentType<ChannelPanelProps>>;
  Connect: ComponentType<ConnectProps>;
  // Home is the type's own page, /settings/channels/type/<type>; without it that address opens the
  // workspace's channel of the type. Setup is the guided connect, /settings/channels/<id>/setup and new/<type>.
  Home?: ComponentType<HomeProps>;
  Setup?: ComponentType<SetupProps>;
}

// ChannelUI is all the web app knows about a channel type. The shared pages (features/settings/channels)
// read it and never name a type; a new type adds its folder and one line below.
export interface ChannelUI {
  type: ChannelType;
  label: string;
  description: string;
  connectLabel: string;
  Icon: ComponentType;
  // The words for a capability code the server lists (Channel.capabilities); undefined hides the code.
  capabilityLabel: (code: string) => string | undefined;
  tabs: { id: string; label: string }[];
  // The type's panels load with the channel pages, never in the first bundle.
  panels: LazyPart<ChannelPanels>;
  // removal: the type's own words for removing a channel (email is disconnected); the page asks and removes.
  removal?: { label: string; title: string; body: (channel: ChannelDetail) => string; confirm: string };
  // fieldLabel: the words for a setting or secret the history names (ChannelEvent.fields).
  fieldLabel?: (field: string) => string | undefined;
  // waitingHelp says how to send the first test message while nothing has arrived; a type without it gets the shared words.
  waitingHelp?: string;
  // caches: the query keys of the type's own settings, reloaded when the channel is turned on or off.
  caches?: string[];
}

// The order here is the order of the Add channel list and of the empty page.
export const channelTypes: ChannelUI[] = [emailChannel, webFormChannel];

// Types the roadmap promises with their tracking issue; shown as not available yet, never added.
export const upcomingChannels: { label: string; issue: number }[] = [{ label: 'Live chat', issue: 124 }];

export const channelUI = (type: string): ChannelUI | undefined => channelTypes.find((ui) => ui.type === type);
