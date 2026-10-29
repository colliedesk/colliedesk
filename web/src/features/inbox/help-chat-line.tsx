// A help chat conversation's sender typed their address: nothing checked it. The line says so and links
// the chat it came from. It loads with the open conversation's chunk (presence-view), not the first bundle.
const words = {
  sender: 'Unverified sender (Help chat)',
  help: 'Anyone can type any address into the help chat, so it may not be the sender’s. Check the request before you share account details.',
  transcript: 'Open chat transcript',
};

export function HelpChatLine({ conversationId }: { conversationId: string }) {
  return <>
    <span className="badge badge-none" title={words.help}>{words.sender}<span className="sr-only"> {words.help}</span></span>
    <a href={`/settings/ai/conversations/${conversationId}/chat`}>{words.transcript}</a>
  </>;
}
