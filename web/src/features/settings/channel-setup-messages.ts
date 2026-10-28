// Words of the email channel and web form settings. They live with those settings, which load as their
// own chunk, not in the first bundle.
export const channelSetupMessages = {
  emailChannelDescription: 'Customers write to your support address, and your mailbox forwards that mail to Colliedesk: to the forwarding address on Colliedesk Cloud, or to the inbound URL on your own server. Replies go out through the server SMTP relay, or through Gmail once it is connected below.',
  inboundSecretMissing: 'No inbound secret yet. Mail reaches the inbound URL only with this secret, a password for your mail forwarder (not your email password). Create one to start receiving mail.',
  smtpNotConfigured: 'Outgoing email is not configured on this server: ask whoever runs it to set it up (self-hosting guide, configuring email). Until then replies are marked Delivery failed.',
  webFormOriginsHelp: 'The websites that may send this form: one per line, only the start of the address, for example https://www.example.com. Leave out any page path, and do not use *.',
};
