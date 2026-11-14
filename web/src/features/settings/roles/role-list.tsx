import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, InvalidRequestError } from '../../../api/client';
import type { Role } from '../../../api/schemas';
import { HelpTip } from '../../../ui/help-tip';
import { can } from '../../../lib/can';
import { roleMessages as m } from '../roles-messages';
import { useCreateMutation } from '../../../api/use-create';
import '../permission-matrix.css';
import './roles.css';

// The roles of the workspace, a section of Settings → Members. A role is created, read, changed or deleted
// on a page of its own (roles-route.tsx), which comes back here.

// undo, when given, is the role just deleted (id and name), which Undo restores.
export type RolesStatus = { text: string; undo?: { id: string; name: string }; team?: boolean };
// carried is what a save says, kept for the list it opens: the form and the list are two routes, so the
// list is a new page that would otherwise forget it.
let carried: RolesStatus | undefined;
export function carryRoleStatus(status: RolesStatus) { carried = status; }

// RolesSection is the list of roles with the line that says what the last save or delete did.
export function RolesSection() {
  const [status, setStatus] = useState<RolesStatus>();
  const statusLine = useRef<HTMLParagraphElement>(null);
  const client = useQueryClient();
  // Once Undo worked the line says so and offers it no more; a failed Undo says that instead.
  const restore = useCreateMutation({ create: (id: string, key: string) => api.restoreRole(id, key) });
  const undo = async (role: { id: string; name: string }) => {
    try {
      const restored = await restore.mutateAsync(role.id);
      void client.invalidateQueries({ queryKey: ['roles'] });
      setStatus({ text: m.restored(restored.name) });
    } catch (error) {
      // A refusal (the role is gone, or its name was taken meanwhile) ends Undo; a failure on the way may be tried again.
      const final = error instanceof InvalidRequestError || (error instanceof Error && error.message === 'API 404');
      setStatus({ text: `${m.deleted(role.name)} ${final ? m.undoFailed : m.undoRetry}`, undo: final ? undefined : role });
    }
  };
  const [focusStatus, setFocusStatus] = useState(false);
  useEffect(() => {
    if (carried) {
      setStatus(carried);
      setFocusStatus(true);
      carried = undefined;
    }
  }, []);
  // After a save or a delete the list opens with what happened, and focus goes there rather than to the page.
  useEffect(() => {
    if (focusStatus && status) {
      statusLine.current?.focus();
      setFocusStatus(false);
    }
  }, [focusStatus, status]);
  return <>
    {status && <p ref={statusLine} tabIndex={-1} role="status" className="notice notice-plain roles-status"><span>{status.text}</span>
      {status.team && <a href="/settings/members#team-heading">{m.goToTeam}</a>}
      {status.undo && <button type="button" className="link-action" disabled={restore.isPending} onClick={() => void undo(status.undo!)}>{m.undo}</button>}</p>}
    <RoleList onStatus={setStatus} />
  </>;
}

// RoleList shows each role with what it holds and who holds it.
function RoleList({ onStatus }: { onStatus: (status: { text: string } | undefined) => void }) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.roles() });
  const deleted = useQuery({ queryKey: ['roles', 'deleted'], queryFn: () => api.roles(true) });
  const [showDeleted, setShowDeleted] = useState(false);
  const manage = can(me.data, 'team:manage_roles');
  const restore = useCreateMutation({ create: (id: string, key: string) => api.restoreRole(id, key), onSuccess: (role) => {
    void client.invalidateQueries({ queryKey: ['roles'] });
    onStatus({ text: m.restored(role.name) });
  } });
  return <section className="settings-card" aria-labelledby="roles-heading">
    <div className="section-heading roles-heading">
      <div><h2 id="roles-heading">{m.heading}</h2><p>{m.description}</p></div>
      {manage && <a className="button primary-action roles-new" href="/settings/roles/new">{m.newRole}</a>}
    </div>
    <div className="settings-body settings-rows">
      {roles.isPending ? <div className="roles-loading" aria-busy="true">{[0, 1, 2].map((row) => <span key={row} className="roles-skeleton" />)}</div>
        : roles.isError ? <div role="alert" className="notice"><span>{m.loadFailed}</span> <button type="button" onClick={() => void roles.refetch()}>{m.tryAgain}</button></div>
        : <>
          <div className="data-list roles-list">{roles.data.items.map((role) => <RoleRow key={role.id} role={role} manage={manage} />)}</div>
          {roles.data.items.length === 2 && manage && <p className="muted-copy">{m.onlySystem} <a className="roles-make" href="/settings/roles/new">{m.makeRole}</a> <HelpTip label={m.makeRole} text={m.onlySystemWhy} /></p>}
        </>}
      {(deleted.data?.items.length ?? 0) > 0 && <button type="button" className="link-action" aria-expanded={showDeleted} onClick={() => setShowDeleted((open) => !open)}>{m.deletedRoles(deleted.data!.items.length)}</button>}
      {showDeleted && <div className="data-list roles-deleted">
        {deleted.data?.items.length === 0 ? <p>{m.noDeletedRoles}</p> : deleted.data?.items.map((role) => <article key={role.id}>
          <div><strong>{role.name}</strong><span>{m.permissionCount(role.permissions.length)}</span></div>
          {manage && <button type="button" disabled={restore.isPending} onClick={() => restore.mutate(role.id)}>{m.restore}</button>}
        </article>)}
        {restore.isError && <p role="alert" className="error-copy">{restore.error.message}</p>}
      </div>}
    </div>
  </section>;
}

function RoleRow({ role, manage }: { role: Role; manage: boolean }) {
  const holders = role.members.map((member) => member.name);
  const count = role.system === 'admin' ? m.everyPermission : m.permissionCount(role.permissions.length);
  return <article className="role-row">
    <div className="role-row-main">
      <strong>{role.name}</strong>{role.system && <span className="muted-copy role-built-in">{m.builtIn}</span>}
      {role.description && <span className="muted-copy role-description">{role.description}</span>}
    </div>
    <div className="role-row-meta">
      <span className="role-meta-wide">{count}</span>
      <span className="role-meta-wide">{m.heldBy(holders)}{holders.length === 0 && manage && <> · <a href="/settings/members#team-heading">{m.giveToSomeone}</a></>}</span>
      <span className="role-meta-narrow">{m.meta(count, m.heldBy(holders))}</span>
    </div>
    <div className="role-row-actions button-row">
      <a className="button" href={`/settings/roles/${role.id}`}>{role.editable ? m.edit : m.view}</a>
      {manage && role.system !== 'admin' && role.assignable && <a className="button" href={`/settings/roles/new?from=${role.id}`}>{m.duplicate}</a>}
    </div>
  </article>;
}
