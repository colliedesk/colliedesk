import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { aiApi } from '../../api/ai';
import { aiChatMessages as m } from './ai-chat-messages';
import { ChatTranscript } from './ai-chat-panel';
import { aiMessages } from './ai-messages';
import { SettingsNav } from './settings-nav';
import { useBillingShown } from '../billing/billing-shown';

// A chat's transcript page under Settings → Built-in AI: every member of the team reads it, an admin
// deletes it.

function useMe() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const billingShown = useBillingShown(isAdmin);
  const shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin,
    billing: billingShown, importData: isAdmin, ai: true };
  return { me: me.data, isAdmin, shown };
}

// AIChatSettingsRoute is /settings/ai/chats/:id, and /settings/ai/conversations/:id/chat.
// conversation: the transcript of the chat that handed off to it (the inbox's Open chat transcript).
export function AIChatSettingsRoute({ id, conversation, navigate }: { id?: string; conversation?: string; navigate: (href: string) => void }) {
  const { me, isAdmin, shown } = useMe();
  const handedOff = useQuery({ queryKey: ['ai-chats', 'conversation', conversation], queryFn: () => aiApi.chatSessions({ conversationId: conversation }), enabled: Boolean(conversation) });
  const chatID = id ?? handedOff.data?.items[0]?.id;
  if (!me) return <main className="settings-page" aria-busy="true" />;
  return <main className="settings-page panel ai-settings">
    <header><h1>{aiMessages.heading}</h1><p>{aiMessages.description}</p></header>
    <div className="settings-layout">
      <SettingsNav page="/settings/ai" shown={shown} />
      <div className="settings-sections ai-page">
        {chatID ? <ChatTranscript id={chatID} timezone={me.timezone ?? 'UTC'} canDelete={isAdmin} back={m.back} backHref="/settings/ai?tab=chat"
          onBack={() => navigate('/settings/ai?tab=chat')} />
          : <p role={handedOff.data || handedOff.isError ? 'alert' : undefined} className="error-copy" aria-busy={!handedOff.data && !handedOff.isError}>
            {handedOff.data ? m.notFound : handedOff.isError ? m.loadFailed : null}</p>}
      </div>
    </div>
  </main>;
}
