// Saved replies may hold two variables. The composer fills them in when an agent inserts the
// reply, so the agent reads and edits the final text before it is sent.
export interface SavedReplyContext {
  customerName?: string;
  agentName?: string;
}

// A space or tab before an empty variable goes with it, so "Hi {{customer.name}}, thanks" reads
// "Hi, thanks" when the customer has no name.
const variable = /([ \t]?)\{\{\s*(customer\.name|agent\.name)\s*\}\}/g;

// renderSavedReply fills {{customer.name}} and {{agent.name}}. A missing name leaves nothing behind,
// never a raw variable. Any other {{…}} stays as written so the agent sees it. One pass over the
// body, so a name that itself looks like a variable is inserted as text.
export function renderSavedReply(body: string, context: SavedReplyContext): string {
  const values: Record<string, string> = {
    'customer.name': context.customerName?.trim() ?? '',
    'agent.name': context.agentName?.trim() ?? '',
  };
  return body.replace(variable, (_match, space: string, name: string) => {
    const value = values[name];
    return value ? space + value : '';
  });
}

const fold = (text: string) => text.normalize('NFD').replace(/\p{M}/gu, '').replace(/đ/g, 'd').replace(/Đ/g, 'D').toLowerCase();

// savedReplyMatches is the picker's search: ignores case and accents, so "chao" finds "Chào".
export function savedReplyMatches(title: string, query: string): boolean {
  const wanted = fold(query.trim());
  return wanted === '' || fold(title).includes(wanted);
}
