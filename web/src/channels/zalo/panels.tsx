import { useEffect, useId, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ConflictError, InvalidRequestError } from '../../api/client';
import { channelsApi } from '../../api/channels';
import type { ChannelDetail, ChatSender, ChatThread, ZaloConfig } from '../../api/channels';
import { CopyButton } from '../../features/settings/copy-button';
import { saveState, useSaveFeedback } from '../../features/settings/save-feedback';
import { formatDate } from '../../lib/format-date';
import { Dialog, Field } from '../../ui/dialog';
import { LockIcon, PlusIcon } from '../../ui/icons';
import type { ChannelPanelProps, ConnectProps } from '../registry';
import { zaloMessages as m } from './zalo-messages';
import type { CommandAction, CommandMatch, SecretName } from './zalo-messages';
import './zalo.css';

const zaloID = /^[0-9]{1,40}$/;
const noticeMax = 1000;
const phraseMax = 60;

// fieldErrors reads the server's refusal of a field ({error: {fields: {name: words}}}).
function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof InvalidRequestError)) return {};
  const fields = error.detail?.fields;
  return fields && typeof fields === 'object' ? Object.fromEntries(Object.entries(fields).filter((entry): entry is [string, string] => typeof entry[1] === 'string')) : {};
}

const refusal = (error: unknown) => (error instanceof InvalidRequestError ? error.message : m.failed);

function useReload(channelID: string) {
  const client = useQueryClient();
  return () => Promise.all([
    client.invalidateQueries({ queryKey: ['channel', channelID] }),
    client.invalidateQueries({ queryKey: ['channels'] }),
  ]);
}

// ---------- Connect ----------

// What the connect form could not store on the new channel, read by its Connection tab.
interface ConnectFailure { name: 'app_secret' | 'oa_secret_key'; reason: string }
const connectFailedKey = (channelID: string) => ['zalo-connect-failed', channelID];

// Connect adds the OA with its ids and the two secrets that check what Zalo sends; the tokens come after,
// on the Connection tab (the channel takes deliveries without them and sends nothing).
function Connect({ canManage, create }: ConnectProps) {
  const [name, setName] = useState('');
  const [oaID, setOAID] = useState('');
  const [appID, setAppID] = useState('');
  const [appSecret, setAppSecret] = useState('');
  const [oaSecret, setOASecret] = useState('');
  const [checked, setChecked] = useState(false);
  const id = useId();
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: () => create({ ...(name.trim() ? { name: name.trim() } : {}), enabled: true, config: { oa_id: oaID.trim(), app_id: appID.trim() } },
      async (created) => {
        // Each secret on its own: one refused (a server without MAILBOX_TOKEN_KEYS) keeps neither the
        // channel nor the other secret from being stored, and Connection says what was not.
        const failed: ConnectFailure[] = [];
        for (const [secret, value] of [['app_secret', appSecret], ['oa_secret_key', oaSecret]] as const) {
          if (!value.trim()) continue;
          try {
            await channelsApi.setCredential(created.id, secret, value.trim());
          } catch (error) {
            failed.push({ name: secret, reason: refusal(error) });
          }
        }
        if (failed.length > 0) client.setQueryData(connectFailedKey(created.id), failed);
      }),
  });
  const feedback = useSaveFeedback(saveState(save));
  if (!canManage) return null;
  const server = fieldErrors(save.error);
  const taken = save.error instanceof ConflictError && save.error.detail?.code === 'channel_account_taken';
  const local = {
    oa_id: !oaID.trim() ? m.required : !zaloID.test(oaID.trim()) ? m.digitsOnly : undefined,
    app_id: !appID.trim() ? m.required : !zaloID.test(appID.trim()) ? m.digitsOnly : undefined,
  };
  const errors = {
    oa_id: (checked ? local.oa_id : undefined) ?? (taken && !feedback.pending ? m.accountTaken : server.oa_id),
    app_id: (checked ? local.app_id : undefined) ?? server.app_id,
  };
  return <form className="stacked-form settings-body" noValidate onSubmit={(event) => {
    event.preventDefault();
    setChecked(true);
    if (feedback.pending || local.oa_id || local.app_id) return;
    save.mutate();
  }}>
    <Field id={`${id}-name`} label={m.name} help={m.nameHint}>{(props) => <input {...props} value={name} maxLength={80} onChange={(event) => { feedback.touch(); setName(event.target.value); }} />}</Field>
    <Field id={`${id}-oa`} label={m.oaID} error={errors.oa_id} help={m.oaIDHint}>{(props) => <input {...props} className="zl-mono-input" inputMode="numeric" autoComplete="off" value={oaID}
      onChange={(event) => { feedback.touch(); setOAID(event.target.value); }} />}</Field>
    <Field id={`${id}-app`} label={m.appID} error={errors.app_id} help={m.appIDHint}>{(props) => <input {...props} className="zl-mono-input" inputMode="numeric" autoComplete="off" value={appID}
      onChange={(event) => { feedback.touch(); setAppID(event.target.value); }} />}</Field>
    <Field id={`${id}-as`} label={m.secretLabel.app_secret} help={m.secretHint}>{(props) => <input {...props} type="password" autoComplete="off" value={appSecret}
      onChange={(event) => { feedback.touch(); setAppSecret(event.target.value); }} />}</Field>
    <Field id={`${id}-os`} label={m.secretLabel.oa_secret_key} help={m.secretHint}>{(props) => <input {...props} type="password" autoComplete="off" value={oaSecret}
      onChange={(event) => { feedback.touch(); setOASecret(event.target.value); }} />}</Field>
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.connect)}</button>
      {!taken && Object.keys(server).length === 0 && feedback.status}</span>
  </form>;
}

// ---------- Connection ----------

const configOf = (channel: ChannelDetail) => channel.config as ZaloConfig;

function Connection({ channel, canManage, timezone }: ChannelPanelProps) {
  const config = configOf(channel);
  const client = useQueryClient();
  // A secret the connect form could not store, until one is set.
  const failed = (client.getQueryData<ConnectFailure[]>(connectFailedKey(channel.id)) ?? [])
    .filter((item) => !config.credentials.find((credential) => credential.name === item.name)?.set_at);
  const url = `${window.location.origin}${config.webhook_path}`;
  const credential = (name: SecretName) => config.credentials.find((item) => item.name === name);
  const [editing, setEditing] = useState<'app_secret' | 'oa_secret_key' | 'tokens'>();
  const [done, setDone] = useState<string>();
  const doneNote = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (done) doneNote.current?.focus(); }, [done]);
  const when = (at: string | null | undefined) => (at ? formatDate(at, timezone) : undefined);
  const access = credential('access_token');
  const refresh = credential('refresh_token');
  const tokensSet = Boolean(access?.set_at) && Boolean(refresh?.set_at);
  const tokensAny = Boolean(access?.set_at) || Boolean(refresh?.set_at);
  const lastToken = [access?.set_at, refresh?.set_at].filter((at): at is string => Boolean(at)).sort().at(-1);
  const tokensState = tokensSet ? <span className="zl-state">{m.tokensSet(when(lastToken))}</span>
    : <span className="zl-state zl-state-warn">{tokensAny ? m.tokensHalf(access?.set_at ? m.secretLabel.refresh_token : m.secretLabel.access_token) : m.tokensMissing}</span>;
  const secretRow = (name: 'app_secret' | 'oa_secret_key') => {
    const item = credential(name);
    const set = Boolean(item?.set_at);
    return <div className="zl-row" key={name}><div><strong>{m.secretLabel[name]}</strong><small>{m.secretUse[name]}</small></div>
      <div><span className={`zl-state${set ? '' : ' zl-muted'}`}>{set ? m.setOn(item?.hint ?? null, when(item?.set_at)) : m.notSet}</span></div>
      <div className="zl-actions">{canManage && <button type="button" onClick={() => { setDone(undefined); setEditing(name); }}>{set ? m.replace : m.add}</button>}</div></div>;
  };
  return <div className="settings-body">
    {!tokensAny && <p className="notice notice-plain" role="status"><span>{m.firstSteps}</span></p>}
    <section className="zl-block" aria-labelledby={`${channel.id}-webhook`}><h3 id={`${channel.id}-webhook`}>{m.webhook}</h3>
      <p className="lead">{m.webhookLead}</p>
      <div className="zl-url"><code>{url}</code><CopyButton value={url} copiedText={m.urlCopied}>{m.copyURL}</CopyButton></div>
    </section>
    <section className="zl-block" aria-labelledby={`${channel.id}-oa`}><h3 id={`${channel.id}-oa`}>{m.account}</h3>
      <dl className="zl-dl"><dt>{m.oaID}</dt><dd className="mono">{config.oa_id}</dd><dt>{m.appID}</dt><dd className="mono">{config.app_id}</dd></dl>
      <p className="muted-copy">{m.anotherOA}</p>
    </section>
    <section className="zl-block" aria-labelledby={`${channel.id}-secrets`}><h3 id={`${channel.id}-secrets`}>{m.secrets}</h3>
      <p className="lead">{m.secretsLead}</p>
      {failed.length > 0 && <p className="notice" role="alert"><span>{failed.map((item) => m.connectFailed(channel.name, m.secretInline[item.name], item.reason)).join(' ')}</span></p>}
      <div className="zl-rows">
        {secretRow('oa_secret_key')}
        {secretRow('app_secret')}
        <div className="zl-row"><div><strong>{m.tokens}</strong><small>{m.tokensUse}</small></div><div>{tokensState}</div>
          <div className="zl-actions">{canManage && <button type="button" onClick={() => { setDone(undefined); setEditing('tokens'); }}>{tokensSet ? m.replaceTokens : m.addTokens}</button>}</div></div>
      </div>
      {done && <p className="notice notice-plain" role="status" tabIndex={-1} ref={doneNote}><span>{done}</span></p>}
    </section>
    <section className="zl-block" aria-labelledby={`${channel.id}-processing`}><h3 id={`${channel.id}-processing`}>{m.processing}</h3>
      <p className="zl-state">{channel.processing_paused_at ? m.pausedSince(formatDate(channel.processing_paused_at, timezone), channel.paused_count) : m.running}</p>
      <p className="muted-copy">{m.processingHelp}</p>
    </section>
    {editing === 'tokens' && <TokensDialog channel={channel} replace={tokensAny} onClose={() => setEditing(undefined)} onSaved={() => { setEditing(undefined); setDone(m.tokensSaved); }} />}
    {editing && editing !== 'tokens' && <SecretDialog channel={channel} name={editing} hint={credential(editing)?.hint ?? null} replace={Boolean(credential(editing)?.set_at)}
      onClose={() => setEditing(undefined)} onSaved={() => { setEditing(undefined); setDone(m.secretSaved(m.secretLabel[editing])); }} />}
  </div>;
}

function SecretDialog({ channel, name, hint, replace, onClose, onSaved }: { channel: ChannelDetail; name: 'app_secret' | 'oa_secret_key'; hint: string | null; replace: boolean; onClose: () => void; onSaved: () => void }) {
  const [value, setValue] = useState('');
  const [checked, setChecked] = useState(false);
  const reload = useReload(channel.id);
  const save = useMutation({ mutationFn: () => channelsApi.setCredential(channel.id, name, value.trim()), onSuccess: async () => { await reload(); onSaved(); } });
  const id = useId();
  const inline = m.secretInline[name];
  const error = checked && !value.trim() ? m.required : save.isError ? refusal(save.error) : undefined;
  return <Dialog title={m.secretTitle(replace, inline, channel.name)} help={replace ? m.secretHelp : undefined} closeLabel={m.close} onClose={onClose}
    onSubmit={() => { setChecked(true); if (value.trim() && !save.isPending) save.mutate(); }}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={save.isPending}>{save.isPending ? m.saving : replace ? m.replace : m.add}</button></>}>
    {replace && hint && <p className="zl-current"><LockIcon />{m.currentSecret} <span className="mono">••••{hint}</span></p>}
    <Field id={id} label={m.newSecret(inline)} error={error}>{(props) => <input {...props} type="password" autoComplete="off" autoFocus value={value} onChange={(event) => { save.reset(); setValue(event.target.value); }} />}</Field>
  </Dialog>;
}

// TokensDialog saves the pair, the refresh token first: it is the one that cannot be had again.
function TokensDialog({ channel, replace, onClose, onSaved }: { channel: ChannelDetail; replace: boolean; onClose: () => void; onSaved: () => void }) {
  const [access, setAccess] = useState('');
  const [refresh, setRefresh] = useState('');
  const [checked, setChecked] = useState(false);
  const [half, setHalf] = useState(false);
  const reload = useReload(channel.id);
  const save = useMutation({
    mutationFn: async () => {
      if (!half) await channelsApi.setCredential(channel.id, 'refresh_token', refresh.trim());
      try {
        await channelsApi.setCredential(channel.id, 'access_token', access.trim());
      } catch (error) {
        setHalf(true);
        throw error;
      }
    },
    onSuccess: async () => { await reload(); onSaved(); },
    onError: () => void reload(),
  });
  const id = useId();
  const missing = (value: string) => (checked && !value.trim() ? m.required : undefined);
  return <Dialog title={m.tokensTitle(replace, channel.name)} help={m.tokensHelp} closeLabel={m.close} onClose={onClose}
    onSubmit={() => { setChecked(true); if (access.trim() && (half || refresh.trim()) && !save.isPending) save.mutate(); }}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={save.isPending}>{save.isPending ? m.saving : m.saveTokens}</button></>}>
    <Field id={`${id}-a`} label={m.secretLabel.access_token} error={missing(access)}>{(props) => <input {...props} type="password" autoComplete="off" autoFocus value={access} onChange={(event) => setAccess(event.target.value)} />}</Field>
    {!half && <Field id={`${id}-r`} label={m.secretLabel.refresh_token} error={missing(refresh)}>{(props) => <input {...props} type="password" autoComplete="off" value={refresh} onChange={(event) => setRefresh(event.target.value)} />}</Field>}
    {save.isError && <p role="alert" className="error-copy">{half ? m.tokensHalfSaved : refusal(save.error)}</p>}
  </Dialog>;
}

// ---------- Groups ----------

const groupID = (thread: ChatThread) => thread.external_id.replace(/^group:/, '');

function useThreads(channelID: string, kind: 'direct' | 'group') {
  return useInfiniteQuery({
    queryKey: ['zalo-threads', channelID, kind],
    queryFn: ({ pageParam }) => channelsApi.threads(channelID, kind, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => page.next_cursor ?? undefined,
  });
}

function useConfigChange(channel: ChannelDetail) {
  const reload = useReload(channel.id);
  return useMutation({
    mutationFn: (config: { staff_user_ids?: string[]; groups?: Record<string, string> }) => channelsApi.update(channel.id, { config }),
    onSuccess: () => reload(),
  });
}

function Groups({ channel, canManage, timezone }: ChannelPanelProps) {
  const config = configOf(channel);
  const threads = useThreads(channel.id, 'group');
  const change = useConfigChange(channel);
  const [linking, setLinking] = useState<string>();
  const [done, setDone] = useState<string>();
  const doneNote = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (done) doneNote.current?.focus(); }, [done]);
  // A group linked before it wrote (or by the API) is listed too.
  const seen = threads.data?.pages.flatMap((page) => page.items) ?? [];
  const ids = [...seen.map(groupID), ...Object.keys(config.groups).filter((id) => !seen.some((thread) => groupID(thread) === id))];
  const lastOf = (id: string) => seen.find((thread) => groupID(thread) === id)?.last_item_at ?? null;
  const unlink = (id: string) => {
    const organization = config.groups[id];
    const groups = { ...config.groups };
    delete groups[id];
    setDone(undefined);
    change.mutate({ groups }, { onSuccess: () => setDone(m.unlinked(m.group(id), organization)) });
  };
  return <>
    <p className="ch-ai-lead">{m.groupsLead}</p>
    {threads.isPending ? <div className="ch-loading" aria-busy="true"><span className="sr-only">{m.loading}</span></div>
      : threads.isError ? <p role="alert" className="error-copy">{m.groupsFailed} <button type="button" className="link-action" onClick={() => void threads.refetch()}>{m.tryAgain}</button></p>
      : ids.length === 0 ? <section className="panel"><p className="ch-empty-copy muted-copy">{m.noGroups}</p></section>
      : <div className="zl-rows" role="list" aria-label={m.groupsLabel}>
          {ids.map((id) => {
            const organization = config.groups[id];
            const last = lastOf(id);
            return <div className="zl-row" role="listitem" key={id}><div><strong>{m.group(id)}</strong>{last && <small>{m.lastMessage(formatDate(last, timezone))}</small>}</div>
              <div><span className={`zl-state${organization ? '' : ' zl-muted'}`}>{organization || m.notLinked}</span></div>
              <div className="zl-actions">{canManage && <>
                <button type="button" onClick={() => { setDone(undefined); setLinking(id); }}>{organization ? m.change : m.link}</button>
                {organization && <button type="button" aria-label={m.unlink(m.group(id))} disabled={change.isPending} onClick={() => unlink(id)}>{m.unlinkShort}</button>}
              </>}</div></div>;
          })}
        </div>}
    {threads.hasNextPage && <span className="button-row"><button type="button" disabled={threads.isFetchingNextPage} onClick={() => void threads.fetchNextPage()}>{m.showMore}</button></span>}
    {change.isError && !linking && <p role="alert" className="error-copy">{fieldErrors(change.error).groups ?? refusal(change.error)}</p>}
    {done && <p className="notice notice-plain" role="status" tabIndex={-1} ref={doneNote}><span>{done}</span></p>}
    {linking && <LinkDialog channel={channel} group={linking} onClose={() => setLinking(undefined)} onLinked={(organization) => { setLinking(undefined); setDone(m.linked(m.group(linking), organization)); }} />}
  </>;
}

function LinkDialog({ channel, group, onClose, onLinked }: { channel: ChannelDetail; group: string; onClose: () => void; onLinked: (organization: string) => void }) {
  const config = configOf(channel);
  const [organization, setOrganization] = useState(config.groups[group] ?? '');
  const [checked, setChecked] = useState(false);
  const change = useConfigChange(channel);
  const id = useId();
  const value = organization.trim();
  const error = checked && !value ? m.required : change.isError ? fieldErrors(change.error).groups ?? refusal(change.error) : undefined;
  return <Dialog title={m.linkTitle(m.group(group))} help={m.linkHelp} closeLabel={m.close} onClose={onClose}
    onSubmit={() => { setChecked(true); if (value && !change.isPending) change.mutate({ groups: { ...config.groups, [group]: value } }, { onSuccess: () => onLinked(value) }); }}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={change.isPending}>{change.isPending ? m.saving : m.linkSave}</button></>}>
    <Field id={id} label={m.organization} error={error} help={m.organizationHint}>{(props) => <input {...props} autoFocus maxLength={200} value={organization}
      onChange={(event) => { change.reset(); setOrganization(event.target.value); }} />}</Field>
  </Dialog>;
}

// ---------- Staff ----------

interface Person { id: string; name: string | null; at: string | null; place: string | null }

// people are the Zalo accounts that wrote in the channel's chats and groups, newest first.
function people(senders: ChatSender[] | undefined): Person[] {
  return (senders ?? []).filter((sender) => sender.kind === 'zalo_user' && zaloID.test(sender.value))
    .map((sender) => ({ id: sender.value, name: sender.name, at: sender.last_at, place: sender.thread_name }));
}

function Staff({ channel, canManage, timezone }: ChannelPanelProps) {
  const config = configOf(channel);
  const senders = useQuery({ queryKey: ['zalo-threads', channel.id, 'senders'], queryFn: () => channelsApi.senders(channel.id) });
  const known = people(senders.data?.items);
  const nameKnown = (id: string) => Boolean(known.find((person) => person.id === id)?.name);
  const nameOf = (id: string) => known.find((person) => person.id === id)?.name ?? m.uid(id);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<string>();
  const [done, setDone] = useState<string>();
  const doneNote = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (done) doneNote.current?.focus(); }, [done]);
  const change = useConfigChange(channel);
  const staff = config.staff_user_ids;
  const addButton = canManage && <button type="button" onClick={() => { setDone(undefined); setAdding(true); }}><PlusIcon />{m.addStaff}</button>;
  return <>
    <p className="ch-ai-lead">{m.staffLead}</p>
    {!canManage && <p className="notice notice-plain" role="note"><span>{m.staffAdminsOnly}</span></p>}
    {staff.length === 0
      ? <p className="notice notice-plain" role="status"><span>{m.noStaff}</span>{addButton && <span className="notice-actions">{addButton}</span>}</p>
      : <>
          <div className="zl-rows" role="list" aria-label={m.staffLabel}>
            {staff.map((id) => <div className="zl-row zl-row-two" role="listitem" key={id}><div>{nameKnown(id) ? <><strong>{nameOf(id)}</strong><small className="mono">{m.uid(id)}</small></> : <strong className="mono">{m.uid(id)}</strong>}</div>
              <div className="zl-actions">{canManage && <button type="button" aria-label={m.removeStaff(nameOf(id))} onClick={() => { setDone(undefined); setRemoving(id); }}>{m.removeShort}</button>}</div></div>)}
          </div>
          {addButton && <span className="button-row">{addButton}</span>}
        </>}
    {change.isError && <p role="alert" className="error-copy">{fieldErrors(change.error).staff_user_ids ?? refusal(change.error)}</p>}
    {done && <p className="notice notice-plain" role="status" tabIndex={-1} ref={doneNote}><span>{done}</span></p>}
    {adding && <StaffDialog channel={channel} people={known.filter((person) => !staff.includes(person.id))} timezone={timezone}
      onClose={() => setAdding(false)} onAdded={(id, name) => { setAdding(false); setDone(m.staffAdded(name ?? m.uid(id))); }} />}
    {removing && <Dialog alert title={m.removeStaffTitle(nameOf(removing))} closeLabel={m.close} onClose={() => setRemoving(undefined)}
      onSubmit={() => {
        const who = nameOf(removing);
        setRemoving(undefined);
        change.mutate({ staff_user_ids: staff.filter((id) => id !== removing) }, { onSuccess: () => setDone(m.staffRemoved(who)) });
      }}
      footer={<><button type="button" autoFocus onClick={() => setRemoving(undefined)}>{m.cancel}</button><button type="submit" className="danger-action">{m.removeShort}</button></>}>
      <p>{m.removeStaffBody}</p>
    </Dialog>}
  </>;
}

function StaffDialog({ channel, people: candidates, timezone, onClose, onAdded }: { channel: ChannelDetail; people: Person[]; timezone: string; onClose: () => void; onAdded: (id: string, name: string | null) => void }) {
  const config = configOf(channel);
  const [picked, setPicked] = useState<string>();
  const [pasted, setPasted] = useState('');
  const [checked, setChecked] = useState(false);
  const change = useConfigChange(channel);
  const id = useId();
  const chosen = pasted.trim() || picked;
  const local = !chosen ? m.pickOne : pasted.trim() && !zaloID.test(pasted.trim()) ? m.digitsOnly : config.staff_user_ids.includes(chosen) ? m.alreadyStaff : undefined;
  const error = checked ? local ?? (change.isError ? fieldErrors(change.error).staff_user_ids ?? refusal(change.error) : undefined) : undefined;
  return <Dialog title={m.addStaff} help={m.addStaffHelp} closeLabel={m.close} onClose={onClose}
    onSubmit={() => {
      setChecked(true);
      if (local || !chosen || change.isPending) return;
      change.mutate({ staff_user_ids: [...config.staff_user_ids, chosen] }, { onSuccess: () => onAdded(chosen, candidates.find((person) => person.id === chosen)?.name ?? null) });
    }}
    footer={<><button type="button" onClick={onClose}>{m.cancel}</button><button type="submit" className="primary-action" disabled={change.isPending}>{change.isPending ? m.saving : m.addSave}</button></>}>
    {candidates.length > 0 && <fieldset className="field zl-pick"><legend className="field-label">{m.recent}</legend>
      {candidates.map((person) => <label className="check-row" key={person.id}>
        <input type="radio" name={`${id}-uid`} checked={!pasted.trim() && picked === person.id} onChange={() => { change.reset(); setPasted(''); setPicked(person.id); }} />
        <span>{m.wrote(person.name, person.id, person.at ? formatDate(person.at, timezone) : m.none, person.place)}</span></label>)}
    </fieldset>}
    <Field id={id} label={m.orPaste} error={error} help={m.pasteHint}>{(props) => <input {...props} className="zl-mono-input" inputMode="numeric" autoComplete="off" value={pasted}
      onChange={(event) => { change.reset(); setPasted(event.target.value); }} />}</Field>
  </Dialog>;
}

// ---------- Messages ----------

interface Command { action?: string; match?: string; phrases?: string[]; tags?: string[]; priority?: string; ask_description?: string }
interface Draft { key: string; phrases: string[]; match: CommandMatch; askDescription: string }
const actions: CommandAction[] = ['open', 'cancel', 'reopen', 'confirm'];
const matches: CommandMatch[] = ['fuzzy', 'words', 'exact'];

const record = (value: unknown): Record<string, unknown> => (value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {});
const text = (value: unknown) => (typeof value === 'string' ? value : '');

// drafts are the commands in force, one per action: the first of each (by name) is the one the page edits.
function drafts(effective: Record<string, unknown>): Record<CommandAction, Draft> {
  const commands = record(effective.commands) as Record<string, Command>;
  const names = Object.keys(commands).sort();
  return Object.fromEntries(actions.map((action) => {
    const key = names.find((name) => (commands[name].action ?? 'open') === action) ?? action;
    const command = commands[key];
    // A new Confirm is exact: "ok" is too short to forgive a letter.
    const match = matches.find((value) => value === command?.match) ?? (command || action !== 'confirm' ? 'fuzzy' : 'exact');
    return [action, { key, phrases: command?.phrases ?? [], match, askDescription: command?.ask_description ?? '' }];
  })) as Record<CommandAction, Draft>;
}

// nextSettings is the whole of what the channel stores (a change replaces it): its stored rules, with the
// commands, notices and one-to-one answer of the page over them. A scope or command is written whole,
// from the one in force, since a stored one replaces its default entirely.
interface Confirm { on: boolean; quiet: string; wait: string }
const confirmLimit = 3600;
const expireLimit = 1440;
const seconds = (value: string) => (/^\d+$/.test(value.trim()) ? Number(value.trim()) : Number.NaN);

function nextSettings(channel: ChannelDetail, edits: { commands: Record<CommandAction, Draft>; received: string; unmatched: string; askConfirm: string; cancelled: string; expired: string; answer?: boolean; confirm?: Confirm; expireSeconds?: number }) {
  const stored = record(channel.settings);
  const effective = record(channel.effective_settings);
  const commands = { ...(record(effective.commands) as Record<string, Command>) };
  for (const action of actions) {
    const draft = edits.commands[action];
    if (draft.phrases.length === 0) delete commands[draft.key];
    else {
      const { ask_description: _old, ...rest } = commands[draft.key] ?? {};
      commands[draft.key] = { ...rest, action, match: draft.match, phrases: draft.phrases, ...(draft.askDescription.trim() ? { ask_description: draft.askDescription.trim() } : {}) };
    }
  }
  const next: Record<string, unknown> = { ...stored, commands, notices: { ...record(effective.notices), received: edits.received.trim(), unmatched: edits.unmatched.trim(),
    ask_confirm: edits.askConfirm.trim(), cancelled: edits.cancelled.trim(), expired: edits.expired.trim() } };
  if (edits.answer !== undefined || edits.confirm) {
    const direct = { ...record(record(effective.scopes).direct) };
    if (edits.answer !== undefined) direct.unmatched = edits.answer ? 'notice' : 'join_open';
    if (edits.confirm) {
      const gather = record(direct.gather);
      direct.gather = edits.confirm.on
        ? { ...gather, quiet_seconds: seconds(edits.confirm.quiet), confirm_seconds: seconds(edits.confirm.wait) }
        : { ...gather, confirm_seconds: 0 };
    }
    next.scopes = { ...record(stored.scopes), direct };
  }
  // How long a request waits for its description is one wait for every scope the rules have (a command
  // asks for it in a chat and in a group alike); each scope and its gathering are copied whole, since a
  // stored one replaces its default, and a scope the rules do not have is not made.
  if (edits.expireSeconds !== undefined) {
    const scopes: Record<string, unknown> = { ...record(stored.scopes), ...record(next.scopes) };
    for (const [key, value] of Object.entries(record(effective.scopes))) {
      const scope = { ...record(value), ...record(scopes[key]) };
      scopes[key] = { ...scope, gather: { ...record(scope.gather), expire_seconds: edits.expireSeconds } };
    }
    next.scopes = scopes;
  }
  return next;
}

function Messages({ channel, canManage }: ChannelPanelProps) {
  const effective = record(channel.effective_settings);
  const notices = record(effective.notices);
  const direct = record(record(effective.scopes).direct);
  const onCommands = direct.mode === 'commands';
  const [commands, setCommands] = useState(() => drafts(effective));
  const [received, setReceived] = useState(text(notices.received));
  const [unmatched, setUnmatched] = useState(text(notices.unmatched));
  const [answer, setAnswer] = useState(direct.unmatched === 'notice');
  const [askConfirm, setAskConfirm] = useState(text(notices.ask_confirm));
  const [cancelled, setCancelled] = useState(text(notices.cancelled));
  const gather = record(direct.gather);
  const [confirm, setConfirm] = useState<Confirm>(() => ({ on: Number(gather.confirm_seconds) > 0, quiet: String(Number(gather.quiet_seconds) || 0), wait: String(Number(gather.confirm_seconds) || 0) }));
  // The description's wait: the one-to-one scope's, in minutes; written only once the field is changed.
  const directExpire = Number(gather.expire_seconds) || 0;
  const otherExpires = Object.entries(record(effective.scopes)).filter(([key]) => key !== 'direct').map(([, value]) => Number(record(record(value).gather).expire_seconds) || 0);
  const groupExpire = otherExpires.find((value) => value !== directExpire);
  const [expire, setExpire] = useState(String(Math.round(directExpire / 60)));
  const [expireTouched, setExpireTouched] = useState(false);
  const [expired, setExpired] = useState(text(notices.expired));
  const [confirmTouched, setConfirmTouched] = useState(false);
  const [checked, setChecked] = useState(false);
  const reload = useReload(channel.id);
  const save = useMutation({
    mutationFn: () => channelsApi.update(channel.id, { settings: nextSettings(channel, { commands, received, unmatched, askConfirm, cancelled, expired,
      answer: onCommands && answer !== (direct.unmatched === 'notice') ? answer : undefined, confirm: confirmTouched ? confirm : undefined,
      expireSeconds: expireTouched ? seconds(expire) * 60 : undefined }) }),
    onSuccess: () => reload(),
  });
  const feedback = useSaveFeedback(saveState(save));
  const id = useId();
  const server = fieldErrors(save.error);
  const quietBad = confirm.on && !(seconds(confirm.quiet) >= 0);
  const waitBad = confirm.on && !(seconds(confirm.wait) >= 1 && seconds(confirm.wait) <= confirmLimit);
  const expireBad = expireTouched && !(seconds(expire) >= 0 && seconds(expire) <= expireLimit);
  // The server asks nothing without the question's words.
  const questionBad = confirm.on && !askConfirm.trim();
  const changeConfirm = (change: Partial<Confirm>) => {
    feedback.touch();
    setConfirmTouched(true);
    setConfirm((now) => {
      const next = { ...now, ...change };
      // Turned on with no wait yet: ten seconds of quiet, then a minute for OK, as most teams start.
      if (change.on && !now.on) return { ...next, quiet: seconds(now.quiet) > 0 ? now.quiet : '10', wait: seconds(now.wait) > 0 ? now.wait : '60' };
      return next;
    });
  };
  const commandError = (draft: Draft) => Object.entries(server).find(([field]) => field.startsWith(`settings.commands.${draft.key}`))?.[1];
  if (!canManage) {
    return <div className="settings-body">
      <p className="notice notice-plain" role="note"><span>{m.messagesAdminsOnly}</span></p>
      <dl className="zl-dl">
        {actions.map((action) => <ReadOnly key={action} label={m.action[action]} value={commands[action].phrases.length ? `${commands[action].phrases.join(' · ')} (${m.match[commands[action].match]})` : m.noPhrase} />)}
        <ReadOnly label={m.received} value={received || m.none} />
        <ReadOnly label={m.unmatched} value={unmatched || m.none} />
        <ReadOnly label={m.confirmHeading} value={confirm.on ? m.confirmSentence(seconds(confirm.quiet), seconds(confirm.wait)) : m.confirmOff} />
        <ReadOnly label={m.question} value={askConfirm || m.none} />
        <ReadOnly label={m.cancelled} value={cancelled || m.none} />
        <ReadOnly label={m.expiredNotice} value={expired || m.none} />
        <ReadOnly label={m.askDescription} value={commands.open.askDescription || m.none} />
      </dl>
    </div>;
  }
  const edit = (action: CommandAction, change: Partial<Draft>) => { feedback.touch(); setCommands((all) => ({ ...all, [action]: { ...all[action], ...change } })); };
  return <form className="settings-body" noValidate onSubmit={(event) => { event.preventDefault(); setChecked(true); if (!feedback.pending && !quietBad && !waitBad && !expireBad && !questionBad) save.mutate(); }}>
    <section className="zl-block" aria-labelledby={`${id}-cmd`}><h3 id={`${id}-cmd`}>{m.commands}</h3>
      <p className="lead">{m.commandsLead}</p>
      <div className="zl-rows">
        {actions.map((action) => <CommandRow key={action} action={action} draft={commands[action]} error={commandError(commands[action])} onChange={(change) => edit(action, change)} />)}
      </div>
      <p className="muted-copy">{m.matchHelp}</p>
      <Field id={`${id}-desc`} label={m.askDescription} error={commandError(commands.open)} help={`${m.askDescriptionHint} ${m.count(commands.open.askDescription.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={commands.open.askDescription} onChange={(event) => edit('open', { askDescription: event.target.value })} />}</Field>
      <Field id={`${id}-x`} label={m.expiresAfter} error={(checked && expireBad ? m.expireRange : undefined) ?? Object.entries(server).find(([field]) => field.endsWith('.gather.expire_seconds'))?.[1]}
        help={[m.minutesUnit, !expireTouched && directExpire % 60 !== 0 ? m.secondsNow(directExpire) : '', !expireTouched && groupExpire !== undefined ? m.groupsUse(groupExpire) : ''].filter(Boolean).join(' ')}>
        {(props) => <input {...props} type="number" min={0} max={expireLimit} step={1} inputMode="numeric" value={expire} onChange={(event) => { feedback.touch(); setExpireTouched(true); setExpire(event.target.value); }} />}</Field>
      <Field id={`${id}-exp`} label={m.expiredNotice} error={server['settings.notices.expired']} help={`${m.unmatchedHint} ${m.count(expired.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={expired} onChange={(event) => { feedback.touch(); setExpired(event.target.value); }} />}</Field>
    </section>
    <section className="zl-block" aria-labelledby={`${id}-ntc`}><h3 id={`${id}-ntc`}>{m.notices}</h3>
      <p className="lead">{m.noticesLead}</p>
      <Field id={`${id}-rcv`} label={m.received} error={server['settings.notices.received']} help={`${m.unmatchedHint} ${m.count(received.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={received} onChange={(event) => { feedback.touch(); setReceived(event.target.value); }} />}</Field>
      <Field id={`${id}-unm`} label={m.unmatched} error={server['settings.notices.unmatched'] ?? server['settings.scopes.direct.unmatched']}
        help={`${onCommands ? m.unmatchedHint : m.unmatchedAllOpen} ${m.count(unmatched.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={unmatched} onChange={(event) => { feedback.touch(); setUnmatched(event.target.value); }} />}</Field>
      {onCommands && <label className="field-inline"><input type="checkbox" checked={answer} onChange={(event) => { feedback.touch(); setAnswer(event.target.checked); }} /><span>{m.unmatchedOn}</span></label>}
      <Field id={`${id}-cnl`} label={m.cancelled} error={server['settings.notices.cancelled']} help={`${m.unmatchedHint} ${m.count(cancelled.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={cancelled} onChange={(event) => { feedback.touch(); setCancelled(event.target.value); }} />}</Field>
    </section>
    <section className="zl-block" aria-labelledby={`${id}-ok`}><h3 id={`${id}-ok`}>{m.confirmHeading}</h3>
      <label className="field-inline"><span className="switch"><input type="checkbox" role="switch" checked={confirm.on} aria-describedby={`${id}-ok-d`}
        onChange={(event) => changeConfirm({ on: event.target.checked })} /></span><span>{m.confirmOn}</span></label>
      <p className="lead" id={`${id}-ok-d`}>{confirm.on && !quietBad && !waitBad ? m.confirmSentence(seconds(confirm.quiet), seconds(confirm.wait)) : m.confirmOff}{onCommands ? '' : ` ${m.confirmNeedsCommand}`}</p>
      {confirm.on && <div className="zl-numbers">
        <Field id={`${id}-q`} label={m.askAfter} error={checked && quietBad ? m.wholeSeconds : undefined} help={m.secondsOfSilence}>
          {(props) => <input {...props} type="number" min={0} step={1} inputMode="numeric" value={confirm.quiet} onChange={(event) => changeConfirm({ quiet: event.target.value })} />}</Field>
        <Field id={`${id}-w`} label={m.openAnyway} error={(checked && waitBad ? m.waitRange : undefined) ?? server[`settings.scopes.direct.gather.confirm_seconds`]} help={m.secondsUnit}>
          {(props) => <input {...props} type="number" min={1} max={confirmLimit} step={1} inputMode="numeric" value={confirm.wait} onChange={(event) => changeConfirm({ wait: event.target.value })} />}</Field>
      </div>}
      <Field id={`${id}-ask`} label={m.question} error={(checked && questionBad ? m.questionRequired : undefined) ?? server['settings.notices.ask_confirm']} help={`${m.questionHint} ${m.count(askConfirm.length, noticeMax)}`}>
        {(props) => <textarea {...props} rows={2} maxLength={noticeMax} value={askConfirm} onChange={(event) => { feedback.touch(); setAskConfirm(event.target.value); }} />}</Field>
    </section>
    <span className="button-row"><button className="primary-action" type="submit" disabled={feedback.pending}>{feedback.saveLabel(m.saveMessages)}</button>{feedback.status}</span>
  </form>;
}

function ReadOnly({ label, value }: { label: string; value: ReactNode }) {
  return <><dt>{label}</dt><dd>{value}</dd></>;
}

function CommandRow({ action, draft, error, onChange }: { action: CommandAction; draft: Draft; error?: string; onChange: (change: Partial<Draft>) => void }) {
  const [adding, setAdding] = useState(false);
  const [phrase, setPhrase] = useState('');
  const id = useId();
  const label = m.action[action];
  const add = () => {
    const value = phrase.trim();
    if (value && !draft.phrases.some((item) => item.toLowerCase() === value.toLowerCase())) onChange({ phrases: [...draft.phrases, value] });
    setPhrase('');
    setAdding(false);
  };
  return <div className="zl-row"><div><strong>{label}</strong><small>{m.actionUse[action]}</small>{error && <small className="error-copy" role="alert">{error}</small>}</div>
    <div className="zl-phrases" role="group" aria-label={m.phrases(label)}>
      {draft.phrases.length === 0 && !adding && <span className="zl-muted">{m.noPhrase}</span>}
      {draft.phrases.map((item) => <span className="zl-phrase" key={item}>{item}
        <button type="button" aria-label={m.removePhrase(item)} title={m.removePhrase(item)} onClick={() => onChange({ phrases: draft.phrases.filter((other) => other !== item) })}>×</button></span>)}
      {adding
        ? <span className="input-with-actions zl-phrase-new"><input aria-label={m.newPhrase(label)} autoFocus maxLength={phraseMax} value={phrase} onChange={(event) => setPhrase(event.target.value)}
            onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); add(); } if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setPhrase(''); setAdding(false); } }} onBlur={add} /></span>
        : <button type="button" className="link-action" onClick={() => setAdding(true)}>{m.addPhrase}</button>}
    </div>
    <div className="zl-actions"><label className="sr-only" htmlFor={`${id}-match`}>{m.matchFor(label)}</label>
      <select id={`${id}-match`} value={draft.match} onChange={(event) => onChange({ match: event.target.value as CommandMatch })}>
        {matches.map((value) => <option key={value} value={value}>{m.match[value]}</option>)}
      </select></div>
  </div>;
}

export const Tabs = { connection: Connection, groups: Groups, staff: Staff, messages: Messages };
export { Connect };
