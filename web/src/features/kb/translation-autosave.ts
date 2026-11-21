import { useRef, useState } from 'react';
import { ChangedElsewhereError, InvalidRequestError } from '../../api/client';
import { formatListTime } from '../../lib/format-date';
import { editableTranslation } from './article-draft';
import { autosaveMessages } from './article-autosave';
import { kbApi } from './kb-api';
import type { KBTranslation } from './kb-api';
import { kbMessages } from './kb-messages';
import { reason } from './kb-tree';
import { translationMessages as t } from './translation-messages';
import { useAutosave } from './use-autosave';

type State = { kind: 'saving' } | { kind: 'saved'; at: string } | undefined;

// A keepalive request outlives a closing page, but the browser takes at most 64 KiB of them at once.
const keepaliveBytes = 60_000;

interface Options {
  articleId: string;
  locale: string;
  title: string;
  body: string;
  timezone: string;
  // current is the translation as the server last answered, read at once (no autosave before it exists).
  current: () => KBTranslation | undefined;
  // blocked: a save, a conflict or a translation in the trash waits for the person.
  blocked: () => boolean;
  onSaved: (answer: KBTranslation) => void;
  onConflict: () => void;
  onProblem: (text: string) => void;
}

// useTranslationAutosave saves a translation's title and text while the person types, as an article's
// are: without a version and never to the help center. A published translation keeps them in its draft.
export function useTranslationAutosave(options: Options) {
  const { title, body, timezone } = options;
  const [state, setState] = useState<State>();
  // pending: an autosave stored text the history has no version of yet; Save adds it.
  const [pending, setPending] = useState(false);
  const inFlight = useRef<Promise<unknown>>(undefined);

  const run = (leaving: boolean) => {
    const of = options.current();
    if (!of || inFlight.current || options.blocked()) return;
    const shown = editableTranslation(of);
    const trimmed = title.trim();
    const change = {
      ...(trimmed && title !== shown.title ? { title } : {}),
      ...(body !== shown.body_md ? { body_md: body } : {}),
    };
    if (!trimmed || Object.keys(change).length === 0) return;
    const sent = JSON.stringify(change);
    setState({ kind: 'saving' });
    inFlight.current = kbApi.updateTranslation(options.articleId, options.locale, { updated_at: of.updated_at, autosave: true, ...change },
      leaving && new TextEncoder().encode(sent).length < keepaliveBytes ? { keepalive: true } : undefined)
      .then((answer) => {
        options.onSaved(answer);
        setPending(true);
        setState({ kind: 'saved', at: answer.updated_at });
      }, (error: unknown) => {
        setState(undefined);
        if (error instanceof ChangedElsewhereError) options.onConflict();
        else if (error instanceof InvalidRequestError) options.onProblem(reason(error, t.failed));
      })
      .finally(() => { inFlight.current = undefined; });
  };
  useAutosave(`${title}\u0000${body}`, run);

  // A manual action waits for an autosave in flight, so both never send the same updated_at.
  const settle = () => inFlight.current?.then(() => undefined, () => undefined);
  // saved: a manual save, publish or reload settled the text; at names the saved time when there is one.
  const saved = (at?: string) => {
    setPending(false);
    setState(at ? { kind: 'saved', at } : undefined);
  };
  const word = state?.kind === 'saving' ? kbMessages.saving : state?.kind === 'saved' ? autosaveMessages.savedAt(formatListTime(state.at, timezone)) : '';
  return { word, pending, settle, saved };
}
