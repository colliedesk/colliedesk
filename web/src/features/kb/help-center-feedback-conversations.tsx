import { useMutation, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { useHolds } from '../../lib/permissions-context';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { kbApi } from './kb-api';
import type { HelpCenter } from './kb-api';

// HelpCenterFeedbackConversations is the workspace switch that opens a conversation from a "No" vote that leaves an
// email address. It saves on the click, sending the rest of the settings back as they are. The server asks for
// channels:update as well as help_center:update to change it, since it lets readers open conversations, so a role
// without it reads the state instead.

const feedbackMessages = {
  label: 'Open a conversation from a "No" vote',
  help: 'A reader who answers No, writes what was missing and leaves an email address opens a conversation, and the article\'s author is asked on it. Replies go to that address.',
  needsChannels: 'Only a role that may change channels changes it.',
  on: 'A "No" vote that leaves an address opens a conversation.',
  off: 'A "No" vote never opens a conversation.',
};

export function HelpCenterFeedbackConversations({ settings, canManage }: { settings: HelpCenter; canManage: boolean }) {
  const client = useQueryClient();
  const holds = useHolds();
  const save = useMutation({
    mutationFn: (on: boolean) => kbApi.saveHelpCenter({ enabled: settings.enabled, title: settings.title, locale: settings.locale || 'en', feedback_conversations: on }),
    onSuccess: (saved) => client.setQueryData(['help-center'], saved),
  });
  if (!canManage || !holds('channels:update')) {
    return <p className="muted-copy">{settings.feedback_conversations ? feedbackMessages.on : feedbackMessages.off}
      {canManage && <> <HelpTip label={feedbackMessages.label} text={feedbackMessages.needsChannels} /></>}</p>;
  }
  const on = save.isPending ? save.variables : settings.feedback_conversations;
  return <div className="field">
    <div className="heading-with-tip"><label className="field-inline"><input type="checkbox" checked={on} disabled={save.isPending} aria-describedby="help-center-feedback-conversations-help"
      onChange={(event) => save.mutate(event.target.checked)} /><span>{feedbackMessages.label}</span></label>
      <HelpTip id="help-center-feedback-conversations-help" label={feedbackMessages.label} text={feedbackMessages.help} /></div>
    {save.isError && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed}</p>}
  </div>;
}
