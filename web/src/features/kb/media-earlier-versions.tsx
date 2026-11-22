import { useId, useState } from 'react';
import { mediaApi } from './media-api';
import type { KBMediaDetails } from './media-api';
import { mediaMessages } from './media-messages';
import { reason } from './kb-tree';

type Notify = (notice: { text: string; undo?: () => void } | undefined) => void;

// EarlierVersions lists the items a replacement put this one in the place of (an annotated copy names
// the original), each with Undo, which puts the original back in the articles the replacement changed.
export function EarlierVersions({ item, onChanged, onNotice }: { item: KBMediaDetails; onChanged: () => void; onNotice: Notify }) {
  const id = useId();
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState<string>();
  if (item.earlier_versions.length === 0) return null;
  const undo = async (replacement: string) => {
    setBusy(replacement);
    setProblem(undefined);
    try {
      const done = await mediaApi.undoReplacement(replacement);
      onNotice({ text: done.skipped_article_ids.length ? mediaMessages.undoneSkipped(done.skipped_article_ids.length) : mediaMessages.undone });
      onChanged();
    } catch (error) {
      setProblem(reason(error, mediaMessages.actionFailed));
    } finally {
      setBusy(undefined);
    }
  };
  return (
    <section className="media-earlier" aria-labelledby={`${id}-heading`}>
      <h3 id={`${id}-heading`} title={mediaMessages.earlierVersionTip}>{mediaMessages.earlierVersion}</h3>
      <ul>
        {item.earlier_versions.map((version) => (
          <li key={version.replacement_id}>
            <img src={version.url} alt="" />
            <span><strong>{version.name}</strong><span className="kb-hint">{mediaMessages.earlierVersionUsed(version.article_ids.length)}</span></span>
            <button type="button" disabled={busy !== undefined} onClick={() => void undo(version.replacement_id)}>{mediaMessages.undo}</button>
          </li>
        ))}
      </ul>
      {problem && <p role="alert" className="error-copy">{problem}</p>}
    </section>
  );
}
