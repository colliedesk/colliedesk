import type { ImportReport } from '../../api/imports';

// onlyArchiveRefused is true when the report is UNVERIFIED for one reason that is not the customer's to fix: the
// Freshdesk plan does not allow the archive API, and every other total Freshdesk reports was confirmed.
export function onlyArchiveRefused(report?: ImportReport): boolean {
  const verification = report?.verification;
  if (report?.status !== 'UNVERIFIED' || !verification?.tickets) return false;
  const { tickets, contacts, saved_replies: savedReplies, knowledge_base: knowledgeBase } = verification;
  return tickets.verified === false && tickets.cause === 'archive_refused' && [contacts, savedReplies, knowledgeBase].every((total) => total?.verified === true);
}
