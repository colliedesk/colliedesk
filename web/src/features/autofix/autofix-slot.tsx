import { Part, PartUnavailable } from '../../ui/lazy-part';
import { autofixPart } from './autofix-part';

// The first bundle's only Autofix code: the slots that load the Autofix chunk. Callers render
// the slots only where /api/v1/config says the server runs Autofix.

// AutofixDetails is the Autofix block at the top of a conversation's details.
export const AutofixDetails = (props: { conversationId: string; customerName?: string; timezone: string }) =>
  <Part part={autofixPart} failed={<PartUnavailable />} render={(chunk) => <chunk.AutofixConversation key={props.conversationId} {...props} />} />;

// AutofixStrip is the fix's one line under the thread header where the details hide behind a toggle.
export const AutofixStrip = (props: { conversationId: string; customerName?: string; timezone: string; onOpenDetails: () => void }) =>
  <Part part={autofixPart} render={(chunk) => <chunk.AutofixStrip {...props} />} />;
