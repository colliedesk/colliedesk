import { useMutation, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { kbApi } from './kb-api';
import type { HelpCenter } from './kb-api';
import { reviewMessages as r } from './review-messages';

// HelpCenterApproval is the workspace switch that makes publishing need a teammate's approval. It saves on
// the click, sending the rest of the settings back as they are. The server asks for kb_articles:publish
// as well as help_center:update to change it, so a role without it reads the state instead.
export function HelpCenterApproval({ settings, canManage }: { settings: HelpCenter; canManage: boolean }) {
  const client = useQueryClient();
  const holds = useHolds();
  const save = useMutation({
    mutationFn: (on: boolean) => kbApi.saveHelpCenter({ enabled: settings.enabled, title: settings.title, locale: settings.locale || 'en', require_approval: on }),
    onSuccess: (saved) => client.setQueryData(['help-center'], saved),
  });
  const on = save.isPending ? save.variables : settings.require_approval;
  if (!canManage || !holds('kb_articles:publish')) {
    return <p className="muted-copy">{r.approvalState(settings.require_approval)}{canManage && <> {r.approvalNeedsPublish}</>}</p>;
  }
  return <div className="field">
    <label className="field-inline"><input type="checkbox" checked={on} disabled={save.isPending} aria-describedby="help-center-approval-help"
      onChange={(event) => save.mutate(event.target.checked)} /><span>{r.requireApproval}</span></label>
    <small id="help-center-approval-help" className="muted-copy">{r.requireApprovalHelp}</small>
    {save.isError && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed}</p>}
  </div>;
}
