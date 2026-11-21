import { kbMessages } from './kb-messages';
import { lineDiff } from './line-diff';

type Text = { title: string; body: string };

export const conflictMessages = {
  legend: 'Struck through: only in theirs. Highlighted: only in yours.',
  title: 'Title',
  theirs: 'Only in theirs: ',
  yours: 'Only in yours: ',
};

// ConflictCompare says a save met a newer copy and shows both, line by line, so the person picks one
// knowing what the other holds. Nothing is overwritten until they choose.
export function ConflictCompare({ message, theirs, mine, busy, onKeepMine, onLoadTheirs }: {
  message: string; theirs?: Text; mine: Text; busy: boolean; onKeepMine: () => void; onLoadTheirs: () => void;
}) {
  return (
    <div role="alert" className="warning-note kb-conflict">
      <p>{message}</p>
      {theirs && <>
        <small className="kb-hint">{conflictMessages.legend}</small>
        <ul className="kb-diff">
          {theirs.title.trim() !== mine.title.trim() && <>
            <li data-change="removed"><span className="kb-diff-label">{conflictMessages.title}</span><span className="sr-only">{conflictMessages.theirs}</span>{theirs.title}</li>
            <li data-change="added"><span className="kb-diff-label">{conflictMessages.title}</span><span className="sr-only">{conflictMessages.yours}</span>{mine.title}</li>
          </>}
          {lineDiff(theirs.body, mine.body).map((line, at) => (
            <li key={at} data-change={line.change}>
              {line.change !== 'same' && <span className="sr-only">{line.change === 'removed' ? conflictMessages.theirs : conflictMessages.yours}</span>}{line.text}</li>
          ))}
        </ul>
      </>}
      <div className="button-row">
        <button type="button" disabled={busy} onClick={onKeepMine}>{kbMessages.keepMine}</button>
        <button type="button" disabled={busy} onClick={onLoadTheirs}>{kbMessages.loadTheirs}</button>
      </div>
      <small>{kbMessages.keepMineHelp}</small>
    </div>
  );
}
