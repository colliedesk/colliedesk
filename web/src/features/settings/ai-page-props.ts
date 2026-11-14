import type { ReactNode } from 'react';
import type { AIModelsInput, AIProviderInput, AISettings, AISlotInput, AISlotName, AIUsage } from '../../api/ai';
import type { TabChange } from '../../ui/tabs';
import type { AIView } from './settings-anchors';
import type { Shown } from './settings-nav';

export type AITab = 'features' | 'providers' | 'chat' | 'usage' | 'history';
// FieldError places a refusal under the field it is about.
export interface FieldError { field: 'label' | 'base_url' | 'api_key' | 'models' | 'form'; text: string }

export interface AIPageProps {
  isAdmin: boolean;
  workspaceName: string;
  timezone: string;
  shown: Shown;
  settings?: AISettings;
  loadFailed?: boolean;
  // showConsent offers the one-time banner that turns both features on with the server's provider.
  showConsent: boolean;
  // busySlot is the feature being saved; busyProvider the provider being checked.
  busySlot?: AISlotName;
  busyProvider?: string;
  // error is the answer of the last page action (a feature or a check) that failed.
  error?: string;
  // Resolves when the server took the change and rejects when it refused it: a switch says Saved only after the first.
  onSetSlot: (slot: AISlotName, input: AISlotInput) => Promise<unknown> | void;
  onConsent: () => void;
  onDismissConsent: () => void;
  onCheck: (id: string) => void;
  // The dialogs' actions resolve to a refusal to show, or nothing when they worked.
  onAdd: (input: AIProviderInput) => Promise<FieldError | undefined>;
  // The models a key may use, before the provider is saved, or why they could not be listed.
  onListModels: (input: AIModelsInput) => Promise<string[] | FieldError>;
  onSetKey: (id: string, key: string) => Promise<FieldError | undefined>;
  onRemove: (id: string) => Promise<FieldError | undefined>;
  onSetAutomatic: (perDay: number, perMinute: number, draftPerDay?: number) => Promise<FieldError | undefined>;
  // The usage of the month shown in the Usage tab, asked for when the tab opens or the month changes.
  usage?: AIUsage;
  usageFailed?: boolean;
  onUsageMonth: (month: string) => void;
  // onOpenChat opens a chat's transcript (the AI chat tab); initialTab is the tab the page opens on.
  onOpenChat?: (id: string) => void;
  initialTab?: AITab;
  // onSetChatCap sets the AI chat feature's own monthly spend limit (null removes it).
  onSetChatCap?: (cap: number | null) => Promise<unknown>;
  // chatChannels: the workspace has a chat channel that collects messages, so intake has a row.
  chatChannels?: boolean;
  // Gives (true) or takes back (false) the consent that intake may send a kind of context.
  onSourceConsent?: (source: string, on: boolean) => Promise<unknown>;
  // builtIn is false for a person whose roles may not read the AI settings (ai_settings:read): the page then
  // holds the AI agents alone.
  builtIn?: boolean;
  // agents are the sections for AI agents working through a token: the External agents view, beside the built-in AI.
  agents?: ReactNode;
  // The view open (the address's ?tab=) and the way to change it.
  view?: AIView;
  onView?: (view: AIView, how: TabChange) => void;
}
