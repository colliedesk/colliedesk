# Changelog

Colliedesk is alpha: releases are `0.0.x`, and any release may change the interface or the
database schema. Every release ships the migrations to upgrade from the previous one. Back up
before upgrading ([data to back up](docs/user/install.md#data-to-back-up)).

## 0.0.1

First tagged release. Everything built so far:

- Passwordless sign-in with one-time pairing and invite links, revocable sessions, and cloud
  sign-in through Clerk on Colliedesk Cloud.
- Shared inbox: replies, private notes, status, priority, assignee, tags, search, bulk actions,
  saved replies, attachments (10 MiB per file, 10 files per message), live updates and browser
  notifications.
- Email channel: inbound and outbound mail, threading, spoof and loop protection.
- Freshdesk importer: tickets including archived ones, replies, notes, checksummed attachments,
  tags, contacts, companies, time entries and canned responses, with a reconcile report and undo.
- Team settings: invite, deactivate and reactivate agents.
- Workspace isolation, per-workspace timezone with per-agent override, full workspace export.
- One-command install with a signed container image. The installer now defaults to `latest`
  (the newest release) instead of `edge`.
- Colliedesk Cloud: 14-day free trial, Team and Founding plans; a workspace whose trial ended is
  locked and can still export everything.
