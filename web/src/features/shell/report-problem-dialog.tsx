import { useId, useRef, useState } from 'react';
import type { ChangeEvent } from 'react';
import { ProblemReportRefusedError, ProblemReportsNotConfiguredError, sendProblemReport } from '../../api/problem-report';
import { collectDiagnostics } from '../../lib/diagnostics';
import { track } from '../../lib/product-analytics';
import { messages } from '../../messages';
import { reportMessages } from './report-messages';
import { dialogKeys, keepDialogFocus, useReturnFocus } from '../../ui/dialog-keys';
import { issueURL } from './report-issue-url';

const ALLOWED = ['image/png', 'image/jpeg', 'image/webp'];
const MAX_FILES = 3;
const MAX_BYTES = 5 << 20;

interface Props {
  appVersion?: string;
  onClose: () => void;
}

function screenshotProblem(files: File[]): string | null {
  if (files.length > MAX_FILES) return reportMessages.reportScreenshotCount;
  if (files.some((file) => !ALLOWED.includes(file.type))) return reportMessages.reportScreenshotType;
  if (files.reduce((total, file) => total + file.size, 0) > MAX_BYTES) return reportMessages.reportScreenshotSize;
  return null;
}

// ReportProblemDialog lets a person send what went wrong with the diagnostics listed under "What will be
// sent". The diagnostics are read once when the dialog opens, so the preview is exactly what is sent.
export function ReportProblemDialog({ appVersion, onClose }: Props) {
  useReturnFocus();
  const [diagnostics] = useState(() => collectDiagnostics(appVersion));
  const [description, setDescription] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [outcome, setOutcome] = useState<{ reference: string } | { github: string } | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const fieldId = useId();
  const fileId = useId();
  const text = description.trim();

  const choose = (event: ChangeEvent<HTMLInputElement>) => {
    const chosen = Array.from(event.target.files ?? []);
    const refusal = screenshotProblem(chosen);
    setProblem(refusal);
    setFiles(refusal ? [] : chosen);
    if (refusal) event.target.value = '';
  };
  const send = async () => {
    setSending(true);
    setProblem(null);
    try {
      const reference = await sendProblemReport(text, diagnostics, files);
      track('problem_reported');
      setOutcome({ reference });
    } catch (error) {
      if (error instanceof ProblemReportsNotConfiguredError) setOutcome({ github: issueURL(text, diagnostics) });
      else setProblem(error instanceof ProblemReportRefusedError ? error.message : reportMessages.reportFailed);
    } finally {
      setSending(false);
    }
  };
  // Escape closes; Tab wraps, so the page behind the dialog is not reached from the keyboard.
  const keys = dialogKeys(root, onClose);

  return (
    <div className="dialog-backdrop" onMouseDown={keepDialogFocus}>
      <div className="dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} ref={root} onKeyDown={keys}>
        <h2 id={titleId}>{messages.reportProblem}</h2>
        {outcome && 'reference' in outcome && <>
          <p role="status">{reportMessages.reportThanks(outcome.reference)}</p>
          <div className="button-row"><button type="button" className="primary-action" autoFocus onClick={onClose}>{messages.reportClose}</button></div>
        </>}
        {outcome && 'github' in outcome && <>
          <p>{reportMessages.reportNotConfigured}</p>
          <div className="button-row">
            <a className="button primary-action" href={outcome.github} target="_blank" rel="noopener noreferrer">{reportMessages.reportGithubIssue}</a>
            <button type="button" onClick={onClose}>{messages.reportClose}</button>
          </div>
        </>}
        {!outcome && <>
          <p className="muted-copy">{reportMessages.reportIntro}</p>
          <label htmlFor={fieldId}>{reportMessages.reportDescription}</label>
          <textarea id={fieldId} autoFocus rows={5} maxLength={5000} value={description} onChange={(event) => setDescription(event.target.value)} />
          <label htmlFor={fileId}>{reportMessages.reportScreenshots}</label>
          <input id={fileId} type="file" multiple accept={ALLOWED.join(',')} onChange={choose} aria-describedby={`${fileId}-hint`} />
          <p id={`${fileId}-hint`} className="muted-copy">{reportMessages.reportScreenshotsHint}</p>
          {problem && <p role="alert" className="error-copy">{problem}</p>}
          <details className="report-preview">
            <summary>{reportMessages.reportPreview}</summary>
            <dl>
              <dt>{reportMessages.reportYourDescription}</dt><dd>{text || reportMessages.reportNone}</dd>
              <dt>{reportMessages.reportShots}</dt><dd>{files.map((file) => file.name).join(', ') || reportMessages.reportNone}</dd>
              <dt>{reportMessages.reportAppVersion}</dt><dd>{diagnostics.app_version}</dd>
              <dt>{reportMessages.reportBrowser}</dt><dd>{diagnostics.user_agent}</dd>
              <dt>{reportMessages.reportWindowSize}</dt><dd>{diagnostics.viewport?.width}x{diagnostics.viewport?.height}</dd>
              <dt>{reportMessages.reportPage}</dt><dd>{diagnostics.page_path}</dd>
              <dt>{reportMessages.reportErrors(diagnostics.client_errors?.length ?? 0)}</dt>
              <dd>{(diagnostics.client_errors ?? []).map((item, index) => <span key={index}>{item.at} {item.source}: {item.message}</span>)}</dd>
              <dt>{reportMessages.reportFailedCalls(diagnostics.failed_api_calls?.length ?? 0)}</dt>
              <dd>{(diagnostics.failed_api_calls ?? []).map((item, index) =>
                <span key={index}>{item.at} {item.method} {item.route} {item.status}, {reportMessages.reportRequestId(item.request_id ?? '')}</span>)}</dd>
              <dt>{reportMessages.reportActions}</dt><dd>{diagnostics.product_events?.join(', ') || reportMessages.reportNone}</dd>
            </dl>
            <p className="muted-copy">{reportMessages.reportFromServer}</p>
          </details>
          <div className="button-row">
            <button type="button" className="primary-action" disabled={text === '' || sending} onClick={() => void send()}>
              {sending ? reportMessages.reportSending : reportMessages.reportSend}
            </button>
            <button type="button" onClick={onClose}>{reportMessages.reportCancel}</button>
          </div>
        </>}
      </div>
    </div>
  );
}
