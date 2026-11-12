import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api, ConflictError, InvalidRequestError } from '../../../api/client';
import type { Role, TokenPermission } from '../../../api/schemas';
import { can } from '../../../lib/can';
import { useBillingShown } from '../../billing/billing-shown';
import { everyPage, SettingsFrame } from '../settings-frame';
import { PermissionMatrix } from '../permission-matrix';
import { roleMessages as m } from '../roles-messages';
import { permissionLabel, outsidePermissions } from '../token-permissions';
import { useCreateMutation } from '../../../api/use-create';
import { carryRoleStatus } from './role-list';
import type { RolesStatus } from './role-list';
import '../permission-matrix.css';
import './roles.css';

// Settings → Members → a role: one role to create, read, change or delete. The list of roles is a section of
// Members (role-list.tsx), where a save or a delete goes back to.
// page: new (with an optional role to start from) or a role's id.
export type RolesPage = { kind: 'new'; from?: string } | { kind: 'edit'; id: string };

export function RolesRoute({ page }: { page: RolesPage }) {
  const navigate = useNavigate();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const billingShown = useBillingShown(can(me.data, 'billing:read'));
  return <SettingsFrame page="/settings/roles" title={m.heading} shown={{ ...everyPage, billing: billingShown }} className="roles-page">
    <RoleForm key={page.kind === 'edit' ? page.id : `new-${page.from ?? ''}`} page={page}
      onDone={(status) => { carryRoleStatus(status); void navigate({ to: '/settings/members', hash: 'roles-heading' }); }} />
  </SettingsFrame>;
}

const AGENT_START: TokenPermission[] = [];

// RoleForm creates a role, or shows and changes one. A change of permissions to a role someone holds or
// a token follows is shown first, with who it reaches, and saved only on Save and apply.
function RoleForm({ page, onDone }: { page: RolesPage; onDone: (status: RolesStatus) => void }) {
  const client = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: api.me });
  const roles = useQuery({ queryKey: ['roles'], queryFn: () => api.roles() });
  const editing = page.kind === 'edit' ? page.id : undefined;
  const role = useQuery({ queryKey: ['roles', 'one', editing], queryFn: () => api.role(editing!), enabled: Boolean(editing), retry: false });
  const from = page.kind === 'new' && page.from ? roles.data?.items.find((item) => item.id === page.from) : undefined;
  const original = role.data;
  const held = useMemo(() => new Set(me.data?.permissions ?? []), [me.data]);
  const [name, setName] = useState<string>();
  const [description, setDescription] = useState<string>();
  const [picked, setPicked] = useState<TokenPermission[]>();
  const [startFrom, setStartFrom] = useState<string>();
  const [confirming, setConfirming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [problem, setProblem] = useState<{ text: string; field?: boolean; changedBy?: boolean }>();
  const [changed, setChanged] = useState<Set<string>>();
  const nameField = useRef<HTMLInputElement>(null);
  const saveButton = useRef<HTMLButtonElement>(null);
  const base = original ?? from;
  const currentName = name ?? (original?.name ?? (from ? m.copyOf(from.name) : ''));
  const currentDescription = description ?? (original?.description ?? from?.description ?? '');
  const startList = (startFrom !== undefined ? roles.data?.items.find((item) => item.id === startFrom)?.permissions : base?.permissions) ?? AGENT_START;
  const currentPicked = picked ?? (startList.filter((item) => item !== undefined) as TokenPermission[]);
  const isAdminRole = original?.system === 'admin';
  // Who to ask when the role may only be read: the active holders of a role that may change roles.
  const canChangeRoles = [...new Set((roles.data?.items ?? []).filter((item) => item.system === 'admin' || item.permissions.includes('team:manage_roles'))
    .flatMap((item) => item.members.filter((member) => member.active).map((member) => member.name)))];
  const readOnly = Boolean(original && !original.editable);
  const before = new Set(original?.permissions ?? []);
  const removed = original ? original.permissions.filter((item) => !currentPicked.includes(item)) : [];
  const added = original ? currentPicked.filter((item) => !before.has(item)) : [];
  const inUse = Boolean(original && (original.members.length > 0 || original.tokens.length > 0));
  const needsConfirm = inUse && (removed.length > 0 || added.length > 0);
  const losesManage = original && held.has('team:manage_roles') && removed.includes('team:manage_roles')
    && !(me.data?.agent.roles ?? []).some((mine) => mine.id !== original.id && (mine.system === 'admin' || roles.data?.items.find((item) => item.id === mine.id)?.permissions.includes('team:manage_roles')));
  useEffect(() => { if (page.kind === 'new') nameField.current?.focus(); }, [page.kind]);
  const toggle = (permission: TokenPermission) => setPicked(currentPicked.includes(permission) ? currentPicked.filter((item) => item !== permission) : [...currentPicked, permission]);
  const setMany = (permissions: TokenPermission[], on: boolean) => setPicked(on ? [...new Set([...currentPicked, ...permissions])] : currentPicked.filter((item) => !permissions.includes(item)));
  const fail = (error: unknown) => {
    if (error instanceof ConflictError && error.detail?.code === 'role_name_taken') {
      setProblem({ text: error.message, field: true });
      nameField.current?.focus();
    }
    else if (error instanceof ConflictError && error.detail?.code === 'role_changed') setProblem({ text: m.changedBy(typeof error.detail.updated_by === 'string' ? error.detail.updated_by : null), changedBy: true });
    else setProblem({ text: error instanceof InvalidRequestError && error.message ? error.message : m.saveFailed });
  };
  const create = useCreateMutation({ create: (value: { name: string; description: string; permissions: TokenPermission[] }, key: string) => api.createRole(value, key),
    onSuccess: (created) => { void client.invalidateQueries({ queryKey: ['roles'] }); onDone({ text: m.created(created.name), team: true }); }, onError: fail });
  const update = useMutation({ mutationFn: () => api.updateRole(original!.id, {
    name: currentName.trim(), description: currentDescription, ...(isAdminRole ? {} : { permissions: currentPicked }), expected_updated_at: original!.updated_at,
  }), onSuccess: (saved) => {
    void client.invalidateQueries({ queryKey: ['roles'] });
    void client.invalidateQueries({ queryKey: ['me'] });
    onDone({ text: m.saved(saved.name, saved.members.length, saved.tokens.length) });
  }, onError: fail });
  // Reset to default fills the matrix with the built-in list; Save role then shows what changes, like any edit.
  const resetToDefault = () => { setPicked([...(original?.default_permissions ?? [])]); setConfirming(false); setProblem(undefined); };
  const remove = useMutation({ mutationFn: () => api.deleteRole(original!.id), onSuccess: () => {
    // The deleted role's own page is not asked for again (it is gone, and the list opens next): only the
    // lists reload. Removing its entry would make the page, still shown, fetch it once more and get 404.
    void client.invalidateQueries({ queryKey: ['roles'], predicate: (query) => query.queryKey[1] !== 'one' });
    const id = original!.id;
    onDone({ text: m.deleted(original!.name), undo: { id, name: original!.name } });
  }, onError: fail });
  const busy = create.isPending || update.isPending || remove.isPending;
  if (editing && role.isPending) return <section className="settings-card" aria-busy="true"><div className="roles-loading">{[0, 1, 2, 3].map((row) => <span key={row} className="roles-skeleton" />)}</div></section>;
  if (editing && role.isError) {
    return <section className="settings-card"><div className="settings-body" role="alert"><p>{m.deletedWhileOpen}</p>
      <span className="button-row"><a className="button" href="/settings/members#roles-heading">{m.backToRoles}</a></span></div></section>;
  }
  const save = () => {
    setProblem(undefined);
    if (!currentName.trim()) {
      setProblem({ text: m.nameMissing, field: true });
      nameField.current?.focus();
      return;
    }
    if (!original) { create.mutate({ name: currentName.trim(), description: currentDescription, permissions: currentPicked }); return; }
    if (needsConfirm && !confirming) { setConfirming(true); return; }
    update.mutate();
  };
  const reload = async () => {
    const fresh = await role.refetch();
    if (fresh.data) {
      setChanged(new Set(fresh.data.permissions.filter((item) => !currentPicked.includes(item)).concat(currentPicked.filter((item) => !fresh.data!.permissions.includes(item)))));
      setProblem(undefined);
    }
  };
  const heading = !original ? m.newHeading : readOnly ? m.viewHeading(original.name) : m.editHeading(original.name);
  const nameError = problem?.field ? 'role-name-error' : undefined;
  const label = (list: readonly string[]) => list.map((item) => `${permissionLabel(item)}${outsidePermissions.has(item as TokenPermission) ? ' (reaches outside the workspace)' : ''}`);
  return <section className="settings-card role-form" aria-labelledby="role-form-heading">
    <div className="section-heading"><h2 id="role-form-heading">{heading}</h2></div>
    <form className="settings-body" noValidate onSubmit={(event) => { event.preventDefault(); save(); }}>
      <label className="field"><span>{m.name}</span>
        <input ref={nameField} value={currentName} maxLength={60} readOnly={readOnly || isAdminRole} aria-invalid={problem?.field || undefined} aria-describedby={nameError}
          onChange={(event) => { setName(event.target.value); setConfirming(false); }} /></label>
      {problem?.field && <p id="role-name-error" className="error-copy">{problem.text}</p>}
      <label className="field"><span>{m.descriptionLabel} <small className="muted-copy">{m.descriptionOptional}</small></span>
        <textarea value={currentDescription} maxLength={300} rows={2} readOnly={readOnly || isAdminRole} onChange={(event) => setDescription(event.target.value)} /></label>
      {!original && <label className="field"><span>{m.startFrom}</span>
        <select value={startFrom ?? from?.id ?? ''} onChange={(event) => { setStartFrom(event.target.value); setPicked(undefined); }}>
          <option value="">{m.startNothing}</option>
          {roles.data?.items.filter((item) => item.system !== 'admin').map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select></label>}
      {original && inUse && <p className="muted-copy">
        {original.members.length > 0 && <>{m.heldByLabel} <LinkedNames names={original.members.map((item) => item.name)} href="/settings/members#team-heading" />. </>}
        {original.tokens.length > 0 && <>{m.followedByLabel(original.tokens.length)} <LinkedNames names={original.tokens.map((item) => `${item.label} (${item.owner})`)} href="/settings/ai#agent-tokens-heading" />.</>}
      </p>}
      {original?.system === 'agent' && <p className="notice notice-plain"><span>{m.agentBuiltIn(original.members.length)}</span></p>}
      <fieldset className="role-permissions"><legend>{m.permissions}</legend>
        {isAdminRole ? <p>{m.adminAlways}</p> : <>
          {!readOnly && <a className="skip-link" href="#role-save">{m.skipToSave}</a>}
          <PermissionMatrix idPrefix="role" picked={currentPicked} allowed={held} onToggle={(item) => { toggle(item); setConfirming(false); }}
            onSetMany={(items, on) => { setMany(items, on); setConfirming(false); }} readOnly={readOnly} changed={changed} />
        </>}
      </fieldset>
      {readOnly && original && !isAdminRole && <p className="muted-copy">{m.askSomeone(canChangeRoles)}</p>}
      {!readOnly && !isAdminRole && currentPicked.length === 0 && <p className="warning-note">{m.noPermissions}</p>}
      {problem && !problem.field && <p role="alert" className="error-copy">{problem.text}{problem.changedBy && <> <button type="button" className="link-action" onClick={() => void reload()}>{m.reload}</button></>}</p>}
      {!readOnly && !isAdminRole && <div className="role-save-bar" id="role-save">
  {confirming && <div className="role-confirm" id="role-confirm" role="group" aria-labelledby="role-confirm-heading">
        <p id="role-confirm-heading">{removed.length > 0 && m.removing(label(removed))} {added.length > 0 && m.adding(label(added))}</p>
        <p>{m.whoChanges(original!.members.map((item) => item.name), original!.tokens.map((item) => `${item.label} (${item.owner})`))}</p>
        {losesManage && <p className="warning-note">{m.loseManage}</p>}
      </div>}
        <span className="button-row">
          <button ref={saveButton} type="submit" className="primary-action" disabled={busy} aria-describedby={confirming ? 'role-confirm' : undefined}>{busy ? m.saving : confirming ? m.saveAndApply : m.save}</button>
          {confirming && <button type="button" onClick={() => setConfirming(false)}>{m.backToEditing}</button>}
          {original?.system === 'agent' && <button type="button" disabled={busy} onClick={resetToDefault}>{m.resetDefault}</button>}
          <a className="button" href="/settings/members#roles-heading">{m.cancel}</a>
        </span>
      </div>}
      {original && original.system === null && !readOnly && <div className="role-delete">
        {original.deletable
          ? deleting
            ? <div role="group" aria-label={m.deleteRole} className="team-confirm"><p>{m.confirmDelete(original.name)}</p>
              <span className="button-row"><button type="button" autoFocus onClick={() => setDeleting(false)}>{m.cancel}</button>
                <button type="button" className="danger-action" disabled={busy} onClick={() => remove.mutate()}>{m.confirm}</button></span></div>
            : <button type="button" className="danger-action" onClick={() => setDeleting(true)}>{m.deleteRole}</button>
          : <p className="muted-copy">{m.inUse(original.members.length, original.invites, original.tokens.length)}{' '}
              <a href="/settings/members#team-heading">Team</a> · <a href="/settings/members#invites-heading">Invites</a> · <a href="/settings/ai#agent-tokens-heading">Tokens</a></p>}
      </div>}
    </form>
  </section>;
}

// LinkedNames lists names as links to where they are managed, three at most and then how many more.
function LinkedNames({ names, href, max = 3 }: { names: readonly string[]; href: string; max?: number }) {
  const shown = names.slice(0, max);
  return <>{shown.map((name, index) => <span key={`${name}-${index}`}>{index > 0 && ', '}<a href={href}>{name}</a></span>)}{names.length > max && m.andMore(names.length - max)}</>;
}
