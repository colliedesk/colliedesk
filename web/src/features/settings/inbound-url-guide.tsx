import { CopyButton } from './copy-button';

// How mail reaches a server that hosts no forwarding address: what the inbound URL is for, where its
// guide is, and one request that proves it works.
const text = {
  what: 'The inbound URL is where this server receives mail. Your mailbox cannot send to it by itself: a mail forwarder (a Cloudflare Email Worker, Postmark, Amazon SES, or a pipe on your mail server) posts each message there with the inbound secret. Usually the person who runs this server sets it up.',
  secret: 'The inbound secret is a password for that forwarder only, not your email password.',
  guide: 'Read how mail reaches the inbound URL',
  guideURL: 'https://colliedesk.com/docs/self-host/email#3-forward-mail-to-colliedesk',
  terminal: 'Test from a terminal instead',
  example: 'Test request',
  exampleHelp: 'For whoever runs the server: run it in a terminal to post one message the way your forwarder will.',
  copy: 'Copy test request',
  copied: 'Request copied',
  secretPlaceholder: 'YOUR_INBOUND_SECRET',
};

// shellQuote wraps a value in single quotes for a POSIX shell.
const shellQuote = (value: string) => `'${value.replaceAll("'", `'\\''`)}'`;

// testRequest is a shell command that posts one plain message to the inbound URL.
export function testRequest(inboundURL: string, supportAddress: string, secret?: string): string {
  const message = `From: Test Customer <customer@example.com>\\r\\nTo: ${supportAddress.replaceAll('%', '%%')}\\r\\nSubject: Test message\\r\\nMessage-ID: <test-%s@example.com>\\r\\n\\r\\nHello, this is a test.\\r\\n`;
  return `printf ${shellQuote(message)} "$(date +%s)" | curl -sS -X POST ${shellQuote(inboundURL)} -H 'Content-Type: message/rfc822' -H ${shellQuote(`X-Colliedesk-Inbound-Secret: ${secret ?? text.secretPlaceholder}`)} --data-binary @-`;
}

export function InboundURLGuide({ inboundURL, supportAddress, secret }: { inboundURL: string; supportAddress: string; secret?: string }) {
  const command = testRequest(inboundURL, supportAddress, secret);
  return <div className="email-channel-forwarding">
    <p className="muted-copy">{text.what} {text.secret}{' '}<a href={text.guideURL} target="_blank" rel="noreferrer">{text.guide}</a></p>
    <details><summary>{text.terminal}</summary>
      <div className="copy-field">
        <label className="invite-link"><span className="field-label">{text.example}</span>
          <textarea readOnly rows={4} value={command} spellCheck={false} /><small>{text.exampleHelp}</small></label>
        <span className="button-row"><CopyButton value={command} copiedText={text.copied}>{text.copy}</CopyButton></span>
      </div>
    </details>
  </div>;
}
