import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import { mentionMessages } from '../inbox/mention-messages';
import { useInstantSave } from './settings-edits';

type MentionPage = Awaited<ReturnType<typeof api.mentions>>;

// MentionEmailSetting is the one checkbox for the email a note that mentions the person sends them; it is theirs on
// every browser (the server keeps it). The setting comes with the Mentions list the sidebar already reads.
export function MentionEmailSetting() {
  const client = useQueryClient();
  const mentions = useQuery({ queryKey: ['mentions', 'unread'], queryFn: () => api.mentions('unread') });
  const save = useMutation({ mutationFn: api.setMentionEmail,
    onSuccess: (answer) => client.setQueriesData<MentionPage>({ queryKey: ['mentions'] }, (page) => page && { ...page, email: answer.email }) });
  const on = save.isPending ? save.variables : mentions.data?.email;
  const instant = useInstantSave();
  return (
    <div className="field">
      <label className="field-inline"><input type="checkbox" checked={on ?? false} disabled={!mentions.data || save.isPending}
        onChange={(event) => void instant(save.mutateAsync, event.target.checked, on ?? false)} />
        <span>{mentionMessages.emailMe}</span></label>
      {save.isError && <small role="alert" className="error-copy">{mentionMessages.emailMeFailed}</small>}
    </div>
  );
}
