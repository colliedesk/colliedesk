import { useEffect, useRef, useState } from 'react';
import type { Me } from '../../api/schemas';
import { messages } from '../../messages';
import { Avatar } from '../../ui/avatar';

interface AccountMenuProps {
  me: Me;
  onSignOut?: () => void;
  signOutFailed?: boolean;
  signingOut?: boolean;
}

// AccountMenu is the avatar at the end of the rail: who is signed in, in which workspace, and the way out.
export function AccountMenu({ me, onSignOut, signOutFailed = false, signingOut = false }: AccountMenuProps) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const name = me.agent.display_name || me.agent.email;
  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false); };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [open]);
  const close = () => { setOpen(false); button.current?.focus(); };
  return (
    <div className="account-menu" ref={root} onKeyDown={(event) => { if (event.key === 'Escape' && open) close(); }}
      onBlur={(event) => { if (open && !root.current?.contains(event.relatedTarget as Node | null)) setOpen(false); }}>
      <button type="button" ref={button} className="rail-avatar" aria-label={messages.account} aria-expanded={open} onClick={() => setOpen(!open)}>
        <Avatar name={name} />
      </button>
      {open && <div className="account-popover" role="group" aria-label={messages.account}>
        <p><strong>{name}</strong>{me.agent.display_name && <span>{me.agent.email}</span>}<span>{me.workspace.name} · {messages.roles[me.agent.role]}</span></p>
        {signOutFailed && <p role="alert" className="error-copy">{messages.signOutFailed}</p>}
        <button type="button" disabled={signingOut} onClick={onSignOut}>{messages.signOut}</button>
      </div>}
    </div>
  );
}
