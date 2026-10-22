import { useCallback, useEffect, useRef, useState } from 'react';
import type { ConversationViewer } from '../../api/schemas';
import { presenceViewPart } from './presence-part';
import type { PresenceSession } from './presence-runtime';

const nobody: ConversationViewer[] = [];

// useConversationPresence tells the team that this tab has a conversation open, and whether its reply
// box holds a reply not sent yet (writing), and returns the others who have it open (presence-runtime.ts
// says how). The code comes with the presence chunk, which starts loading only once a conversation is
// open, after its own requests. markEdited says the reply text changed.
export function useConversationPresence(conversationId: string | undefined, writing: boolean) {
  const part = presenceViewPart.use(Boolean(conversationId));
  const runtime = part.status === 'ready' ? part.module : undefined;
  const [answer, setAnswer] = useState<{ id: string; viewers: ConversationViewer[] }>();
  const writingNow = useRef(writing);
  const session = useRef<PresenceSession | undefined>(undefined);

  useEffect(() => {
    if (!conversationId || !runtime) return;
    const current = runtime.startPresence(conversationId, writingNow.current, (viewers) => setAnswer({ id: conversationId, viewers }));
    session.current = current;
    return () => {
      current.stop();
      session.current = undefined;
    };
  }, [conversationId, runtime]);

  useEffect(() => {
    writingNow.current = writing;
    session.current?.setWriting(writing);
  }, [writing]);

  const markEdited = useCallback(() => session.current?.markEdited(), []);
  return { viewers: answer && answer.id === conversationId ? answer.viewers : nobody, markEdited };
}
