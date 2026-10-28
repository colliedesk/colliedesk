import { CopyButton } from './copy-button';

// How mail reaches a server that hosts no forwarding address: what the inbound URL is for, where its
// guide is, and one request that proves it works.
const text = {
  what: 'To receive customer email here, ask whoever manages your email to send a copy of every message to the inbound URL above, with the inbound secret. The secret is a password for that only, not your email password.',
  details: 'Technical details',
  technical: 'Your mailbox cannot send to the inbound URL by itself. A mail forwarder posts each message there over HTTPS, with the inbound secret in the X-Colliedesk-Inbound-Secret header: a Cloudflare Email Worker, Postmark, Amazon SES, or a pipe on your mail server.',
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
    <p className="muted-copy">{text.what}</p>
    <details><summary>{text.details}</summary>
      <p className="muted-copy">{text.technical}{' '}<a href={text.guideURL} target="_blank" rel="noreferrer">{text.guide}</a></p>
    </details>
    <details><summary>{text.terminal}</summary>
      <div className="copy-field">
        <label className="invite-link"><span className="field-label">{text.example}</span>
          <textarea readOnly rows={4} value={command} spellCheck={false} /><small>{text.exampleHelp}</small></label>
        <span className="button-row"><CopyButton value={command} copiedText={text.copied}>{text.copy}</CopyButton></span>
      </div>
    </details>
  </div>;
}
