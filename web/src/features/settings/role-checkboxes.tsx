import type { ReactNode } from 'react';
import type { Role } from '../../api/schemas';
import { LockIcon } from '../../ui/icons';
import { roleMessages as text } from './roles-messages';
import './role-pickers.css';

interface RoleCheckboxesProps {
  roles: Role[];
  chosen: readonly string[];
  onChange: (chosen: string[]) => void;
  idPrefix: string;
  // locked: a role that may not be ticked or unticked here (for example Admin of the last admin), with why.
  locked?: ReadonlyMap<string, string>;
  // note: a line under a role, for example the tokens that stop when it is taken.
  note?: (role: Role) => ReactNode;
}

// RoleCheckboxes picks roles. A role the person may not give (it holds a permission they lack) stays
// listed, locked, and says why.
export function RoleCheckboxes({ roles, chosen, onChange, idPrefix, locked, note }: RoleCheckboxesProps) {
  const lockedNote = `${idPrefix}-cant-give`;
  return (
    <div className="role-checkboxes">
      <p id={lockedNote} className="sr-only">{text.cantGiveLong}</p>
      {roles.map((role) => {
        const why = locked?.get(role.id);
        const off = !role.assignable || Boolean(why);
        const id = `${idPrefix}-${role.id}`;
        const describedBy = why ? `${id}-why` : !role.assignable ? lockedNote : undefined;
        return (
          <div key={role.id} className="role-checkbox" data-locked={off || undefined}>
            <label htmlFor={id}>
              <input id={id} type="checkbox" checked={chosen.includes(role.id)} aria-disabled={off || undefined} aria-describedby={describedBy}
                onChange={() => { if (!off) onChange(chosen.includes(role.id) ? chosen.filter((item) => item !== role.id) : [...chosen, role.id]); }} />
              {off && <span aria-hidden="true" className="role-lock"><LockIcon /></span>}
              <span className="role-checkbox-name">{role.name}</span>
              <span className="muted-copy">{role.system === 'admin' ? text.everyPermission : text.permissionCount(role.permissions.length)}</span>
              {!role.assignable && !why && <span className="muted-copy">· {text.cantGive}</span>}
            </label>
            {why && <p id={`${id}-why`} className="muted-copy">{why}</p>}
            {note?.(role)}
          </div>
        );
      })}
    </div>
  );
}
