import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import { aiChatMessages as m } from './ai-chat-messages';
import { ChatList, ChatTranscript } from './ai-chat-panel';
import { aiMessages } from './ai-messages';
import { SettingsNav } from './settings-nav';

// The chat transcript pages: under Settings → Built-in AI for an admin (with Delete), and /ai-chat for every
// member of the team, read only.

function useMe() {
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config });
  const isAdmin = me.data?.agent.role === 'admin';
  const shown = { general: isAdmin, team: isAdmin, channels: true, savedReplies: true, exportData: isAdmin,
    billing: isAdmin && config.data?.billing.enabled === true, importData: isAdmin, ai: true };
  return { me: me.data, isAdmin, shown };
}

// AIChatSettingsRoute is /settings/ai/chats/:id.
export function AIChatSettingsRoute({ id, navigate }: { id: string; navigate: (href: string) => void }) {
  const { me, isAdmin, shown } = useMe();
  if (!me) return <main className="settings-page" aria-busy="true" />;
  return <main className="settings-page panel ai-settings">
    <header><h1>{aiMessages.heading}</h1><p>{aiMessages.description}</p></header>
    <div className="settings-layout">
      <SettingsNav page="/settings/ai" shown={shown} />
      <div className="settings-sections ai-page">
        <ChatTranscript id={id} timezone={me.timezone ?? 'UTC'} canDelete={isAdmin} back={m.back} backHref="/settings/ai?tab=chat"
          onBack={() => navigate('/settings/ai?tab=chat')} />
      </div>
    </div>
  </main>;
}

// AIChatListRoute is /ai-chat: every member reads the chats.
export function AIChatListRoute({ navigate }: { navigate: (href: string) => void }) {
  const { me } = useMe();
  if (!me) return <main className="settings-page" aria-busy="true" />;
  return <main className="settings-page panel ai-settings">
    <header><h1>{m.agentHeading}</h1><p>{m.agentDescription}</p></header>
    <ChatList timezone={me.timezone ?? 'UTC'} onOpen={(id) => navigate(`/ai-chat/sessions/${encodeURIComponent(id)}`)} />
  </main>;
}

// AIChatSessionRoute is /ai-chat/sessions/:id, read only.
export function AIChatSessionRoute({ id, navigate }: { id: string; navigate: (href: string) => void }) {
  const { me } = useMe();
  if (!me) return <main className="settings-page" aria-busy="true" />;
  return <main className="settings-page panel ai-settings">
    <ChatTranscript id={id} timezone={me.timezone ?? 'UTC'} canDelete={false} back={m.backAgent} backHref="/ai-chat" onBack={() => navigate('/ai-chat')} />
  </main>;
}
