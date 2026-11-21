import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { Me } from '../../api/schemas';
import { kbMessages } from './kb-messages';

export type EditorKind = 'visual' | 'markdown';

// useEditorKind is the editor this member writes articles in: the one they last chose, on any device, and the
// visual editor until they choose. A choice shows at once; it is kept for them on the server.
export function useEditorKind(): [EditorKind, (next: EditorKind) => void] {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const [chosen, setChosen] = useState<EditorKind>();
  const kind = chosen ?? me.data?.kb_editor ?? 'visual';
  const choose = (next: EditorKind) => {
    setChosen(next);
    client.setQueryData<Me>(['me'], (current) => (current ? { ...current, kb_editor: next } : current));
    // A choice the server did not keep still holds on this page; the next one tries again.
    api.setKBEditor(next).catch(() => {});
  };
  return [kind, choose];
}

// EditorKindSwitch switches between the visual editor and the Markdown source. It waits while an image uploads,
// as the upload's place is in the editor that started it.
export function EditorKindSwitch({ kind, disabled, onChoose }: { kind: EditorKind; disabled: boolean; onChoose: (next: EditorKind) => void }) {
  const kinds: EditorKind[] = ['visual', 'markdown'];
  return (
    <div className="kb-tabs kb-editor-kind" role="group" aria-label={kbMessages.editorKind}>
      {kinds.map((name) => (
        <button key={name} type="button" aria-pressed={kind === name} disabled={disabled && kind !== name}
          onClick={() => { if (kind !== name) onChoose(name); }}>
          {name === 'visual' ? kbMessages.visual : kbMessages.markdown}
        </button>
      ))}
    </div>
  );
}
