# Migrate to Colliedesk Cloud

This page moves a workspace from a self-hosted server to [Colliedesk Cloud](https://colliedesk.com).
There is no self-service import of a workspace export yet, so HC Labs does the import with you.

## Requirements

- A self-hosted server on the latest release. See [upgrading](README.md#upgrading).
- An admin account in each workspace you move. Only admins can export.

## Steps

1. Export the workspace: in the app, open **Settings → Export all data**. Details are in
   [export all your data](../user/export.md).
2. Check that the ZIP file contains `manifest.json`. A file without it is incomplete; export again.
3. Email [support@colliedesk.com](mailto:support@colliedesk.com) with the subject "Migrate to Cloud".
   Give the workspace slug, the record counts and the file size from `manifest.json`. Do not attach
   the file; we reply with how to send it.
4. Keep the self-hosted server running and receiving mail until the Cloud workspace is ready.

## After the move

The export does not hold sessions, API tokens, pairing or invite links, or the email channel's
inbound secret. On the Cloud workspace:

1. Each agent signs in again with their email address.
2. Create new [agent tokens](../user/sessions.md#agent-tokens) and update the tools that use them.
3. Point your mail forwarder at the Cloud workspace address. See
   [forward to your address](../user/email-channel.md#2-forward-its-mail-to-colliedesk).
4. Turn the [web form](../user/web-form.md) on again with your allowed origins, and change the form
   on your site to send to the Cloud address.
5. When mail arrives in Cloud, stop the self-hosted server and keep its last backup.

## Coming from another helpdesk

To move from Freshdesk or Help Scout, see [import from Freshdesk](../user/import-freshdesk.md)
and [import from Help Scout](../user/import-helpscout.md). Both work on Cloud and on your own server.
