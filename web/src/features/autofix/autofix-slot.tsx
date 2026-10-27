import { useQuery } from '@tanstack/react-query';
import { z } from 'zod';
import { request } from '../../api/client';
import { Part, PartUnavailable } from '../../ui/lazy-part';
import { autofixPart } from './autofix-part';

// The first bundle's only Autofix code: the rail's counts and the slots that load the Autofix chunk. Callers render
// the slots only where /api/v1/config says the server runs Autofix.

const statusSchema = z.object({ connected_repos: z.number(), needs_person: z.number(), running: z.number() });

// useAutofixStatus is what the rail needs on every page: whether a repository is connected (the Fixes link shows only
// then) and how many fixes need a person (its badge).
export const useAutofixStatus = (enabled: boolean) =>
  useQuery({ queryKey: ['autofix-status'], queryFn: () => request('/api/v1/autofix/status', statusSchema), enabled, refetchInterval: 60_000 }).data;

// AutofixDetails is the Autofix block at the top of a conversation's details.
export const AutofixDetails = (props: { conversationId: string; customerName?: string; timezone: string }) =>
  <Part part={autofixPart} failed={<PartUnavailable />} render={(chunk) => <chunk.AutofixConversation key={props.conversationId} {...props} />} />;

// AutofixStrip is the fix's one line under the thread header where the details hide behind a toggle.
export const AutofixStrip = (props: { conversationId: string; customerName?: string; timezone: string; onOpenDetails: () => void }) =>
  <Part part={autofixPart} render={(chunk) => <chunk.AutofixStrip {...props} />} />;
