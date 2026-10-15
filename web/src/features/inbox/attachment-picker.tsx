import { useRef } from 'react';
import { formatBytes } from '../../lib/format-bytes';
import { messages } from '../../messages';
import { PaperclipIcon, XIcon } from '../../ui/icons';

export interface AttachmentLimits {
  bytes: number;
  files: number;
}

// attachmentProblem says why the chosen files cannot be sent, so the composer warns before upload.
// Without limits (the config has not loaded) the server's 413 is the check.
export function attachmentProblem(files: File[], limits?: AttachmentLimits): string | undefined {
  if (!limits) return undefined;
  if (files.length > limits.files) return messages.attachmentsTooMany(limits.files);
  const total = files.reduce((sum, file) => sum + file.size, 0);
  return total > limits.bytes ? messages.attachmentsTooLarge(formatBytes(total), formatBytes(limits.bytes)) : undefined;
}

interface AttachmentPickerProps {
  files: File[];
  onChange: (files: File[]) => void;
  limits?: AttachmentLimits;
  disabled?: boolean;
}

export function AttachmentPicker({ files, onChange, limits, disabled }: AttachmentPickerProps) {
  const input = useRef<HTMLInputElement>(null);
  const problem = attachmentProblem(files, limits);
  return (
    <div className="composer-attachments">
      <input ref={input} type="file" multiple hidden tabIndex={-1} aria-label={messages.attachFiles} disabled={disabled} onChange={(event) => {
        onChange([...files, ...Array.from(event.target.files ?? [])]);
        // The same file can be chosen again after it was removed.
        event.target.value = '';
      }} />
      <button type="button" className="attach-button" disabled={disabled} title={messages.addAttachment} aria-label={messages.addAttachment} onClick={() => input.current?.click()}>
        <PaperclipIcon />
      </button>
      {files.length > 0 && (
        <ul className="composer-files" aria-label={messages.filesToSend}>
          {files.map((file, position) => (
            <li key={`${file.name}-${position}`}>
              <PaperclipIcon /><span>{file.name}</span><span className="attachment-size">{formatBytes(file.size)}</span>
              <button type="button" aria-label={messages.removeFile(file.name)} title={messages.removeFile(file.name)} onClick={() => onChange(files.filter((_, index) => index !== position))}><XIcon /></button>
            </li>
          ))}
        </ul>
      )}
      {problem && <p role="alert" className="composer-attachments-problem">{problem}</p>}
    </div>
  );
}
