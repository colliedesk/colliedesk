// Words of the email channel and web form settings. They live with those settings, which load as their
// own chunk, not in the first bundle.
export const channelSetupMessages = {
  emailChannelDescription: 'Where customer mail arrives and how replies leave.',
  emailChannelWhy: 'Customers write to your support address, and your mailbox forwards that mail to Colliedesk: to the forwarding address on Colliedesk Cloud, or to the inbound URL on your own server. Replies go out through the server SMTP relay, or through Gmail once it is connected below.',
  inboundSecretMissing: 'No inbound secret yet. Create one to receive mail.',
  inboundSecretWhy: 'Mail reaches this server only with this secret, a password used only to send mail here (not your email password).',
  smtpNotConfigured: 'Outgoing email is not configured on this server: ask whoever runs it to set it up (self-hosting guide, configuring email). Until then replies are marked Delivery failed.',
  webFormOriginsHelp: 'One website per line, like https://www.example.com.',
  webFormOriginsWhy: 'These websites may send this form. Write only the start of the address: leave out any page path, and do not use *.',
};
