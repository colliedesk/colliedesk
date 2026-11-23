import { useMutation, useQueryClient } from '@tanstack/react-query';
import { InvalidRequestError } from '../../api/client';
import { messages } from '../../messages';
import { HelpTip } from '../../ui/help-tip';
import { kbApi } from './kb-api';
import type { HelpCenter } from './kb-api';

// HelpCenterShowTags is the switch that shows each article's published tags on its help center page. It saves on
// the click, sending the rest of the settings back as they are. Help center search finds articles by tag either way.

const showTagsMessages = {
  label: 'Show tags on article pages',
  help: 'Readers see an article\'s tags under its title. Search on the help center finds articles by their tags either way.',
  on: 'Article pages show their tags to readers.',
  off: 'Article pages do not show their tags to readers.',
};

// The primary's switch saves its settings; another help center's passes saveOn, which saves that one's by id.
export function HelpCenterShowTags({ settings, canManage, saveOn }: {
  settings: Pick<HelpCenter, 'enabled' | 'title' | 'locale' | 'show_tags'>; canManage: boolean; saveOn?: (on: boolean) => Promise<unknown>;
}) {
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (on: boolean) => saveOn ? saveOn(on) : kbApi.saveHelpCenter({ enabled: settings.enabled, title: settings.title, locale: settings.locale || 'en', show_tags: on }),
    onSuccess: (saved) => { if (!saveOn) client.setQueryData(['help-center'], saved); },
  });
  if (!canManage) return <p className="muted-copy">{settings.show_tags ? showTagsMessages.on : showTagsMessages.off}</p>;
  const on = save.isPending ? save.variables : settings.show_tags;
  return <div className="field">
    <div className="heading-with-tip"><label className="field-inline"><input type="checkbox" checked={on} disabled={save.isPending} aria-describedby="help-center-show-tags-help"
      onChange={(event) => save.mutate(event.target.checked)} /><span>{showTagsMessages.label}</span></label>
      <HelpTip id="help-center-show-tags-help" label={showTagsMessages.label} text={showTagsMessages.help} /></div>
    {save.isError && <p role="alert" className="error-copy">{save.error instanceof InvalidRequestError ? save.error.message : messages.settingsActionFailed}</p>}
  </div>;
}
