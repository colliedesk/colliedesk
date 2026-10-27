import type { ChildProcess } from 'node:child_process';
import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import { colliedesk, databaseURL, mustColliedesk, psql, startServer, stopServer, uniqueSuffix, withDatabase } from './support/cli';
import { ports } from './support/checkout';
import { redeemLink } from './support/browser';
import { autofixEnv, cursor, cursorStandIn, github, startAutofixStandIns } from './support/autofix';

// Autofix, the bridge to a team's own coding agent, end to end against the GitHub and Cursor stand-ins, on a
// server and database of their own: the shared e2e servers run Autofix workers without a GitHub App.
const port = ports.autofix;
const origin = `http://localhost:${port}`;
const database = `colliedesk_e2e_autofix_${process.pid}`;
const autofixURL = withDatabase(databaseURL, database);
let server: ChildProcess | undefined;
let standIns: ChildProcess[] = [];

// One worker runs this file: the server and the stand-ins each listen on one fixed port.
test.describe.configure({ mode: 'default' });

test.beforeAll(async () => {
  psql(`DROP DATABASE IF EXISTS ${database}`, withDatabase(databaseURL, 'postgres'));
  psql(`CREATE DATABASE ${database}`, withDatabase(databaseURL, 'postgres'));
  expect(mustColliedesk(['migrate'], { databaseURL: autofixURL, origin })).toBe('migrations applied');
  standIns = await startAutofixStandIns();
  server = await startServer(port, { DATABASE_URL: autofixURL, MAILBOX_TOKEN_KEYS: 'k1:ZTJlLWF1dG9maXgtYnJpZGdlLWtleS0zMi1ieXRlcyE=', ...autofixEnv() }, 'e2e');
});

test.afterAll(async () => {
  await stopServer(server);
  for (const child of standIns) child.kill('SIGTERM');
  psql(`DROP DATABASE IF EXISTS ${database} WITH (FORCE)`, withDatabase(databaseURL, 'postgres'));
});

interface Desk { suffix: string; secret: string; support: string; account: string; installation: number }

// Every workspace has an installation of its own: one installation joins one workspace only.
let nextInstallation = 7_000 + (process.pid % 1_000) * 10;

// signIn creates a workspace, opens it as its admin and gives it an email channel to receive conversations.
async function signIn(page: Page, name: string): Promise<Desk> {
  const suffix = uniqueSuffix();
  const created = colliedesk(['create-workspace', '--name', `${name} ${suffix}`, '--admin-email', `admin-${suffix}@example.com`], { databaseURL: autofixURL, origin });
  expect(created.status, created.stderr).toBe(0);
  await redeemLink(page, created.stdout.trim());
  const support = `support-${suffix}@shop.example`;
  await page.goto(`${origin}/settings/channels/type/email`);
  await page.getByRole('textbox', { name: 'Support address' }).fill(support);
  await page.getByRole('button', { name: 'Save support address' }).click();
  await page.getByRole('button', { name: 'Create inbound secret' }).click();
  const secret = await page.getByRole('textbox', { name: /Inbound secret/ }).inputValue();
  nextInstallation += 1;
  return { suffix, secret, support, account: `acme-${suffix}`.toLowerCase(), installation: nextInstallation };
}

// receive delivers a customer's email and opens its conversation.
async function receive(page: Page, desk: Desk, subject: string, text: string): Promise<void> {
  const raw = [`From: Erin Customer <erin-${desk.suffix}@customer.example>`, `To: ${desk.support}`, `Subject: ${subject}`,
    `Message-ID: <${subject.replace(/\W+/g, '-')}-${desk.suffix}@customer.example>`, 'Content-Type: text/plain; charset=utf-8', '', text].join('\r\n');
  const inbound = await page.request.post(`${origin}/api/v1/channels/email/inbound`, {
    headers: { 'Content-Type': 'message/rfc822', 'X-Colliedesk-Inbound-Secret': desk.secret }, data: Buffer.from(raw),
  });
  expect(inbound.status()).toBe(200);
  await page.goto(`${origin}/`);
  await page.getByRole('button', { name: new RegExp(subject) }).click();
}

const step = (page: Page, name: string) => page.locator('section.af-stepcard').filter({ has: page.getByRole('heading', { name, exact: true }) });
const repoRow = (page: Page, fullName: string) => step(page, 'Repositories').getByRole('listitem').filter({ hasText: fullName });
const vendorCard = (page: Page, name: string) => page.getByRole('article', { name });
const block = (page: Page) => page.getByRole('region', { name: 'Autofix' });

// connectGitHub installs the App from Settings → Autofix through the stand-in's install page, as octocat.
async function connectGitHub(page: Page, desk: Desk): Promise<void> {
  await page.goto(`${origin}/settings/autofix`);
  await step(page, 'Connect GitHub').getByRole('button', { name: 'Connect GitHub' }).click();
  await page.getByRole('dialog', { name: 'On GitHub, choose “Only select repositories”' }).getByRole('button', { name: 'Continue to GitHub' }).click();
  await page.getByRole('link', { name: `Install on ${desk.account} as octocat` }).click();
  await expect(page.getByRole('status').filter({ hasText: 'GitHub is connected. Pick the repositories below.' })).toBeVisible();
  await expect(step(page, 'GitHub')).toContainText(desk.account);
}

async function connectRepo(page: Page, fullName: string): Promise<void> {
  await repoRow(page, fullName).getByRole('button', { name: 'Connect' }).click();
  await expect(repoRow(page, fullName)).toContainText('Connected by');
}

// turnOn agrees to what an issue agent receives and turns it on.
async function turnOn(page: Page, name: 'Claude' | 'Copilot'): Promise<void> {
  const card = vendorCard(page, name);
  await card.getByRole('checkbox', { name: /I agree that/ }).check();
  await card.getByRole('button', { name: `Turn on ${name}` }).click();
  await expect(card.getByText('On', { exact: true })).toBeVisible();
}

// sendThroughGitHub reviews the preview and confirms on the stand-in's authorize page (as octocat), back on the
// conversation.
async function sendThroughGitHub(page: Page, dialogName: string, agent?: string): Promise<void> {
  const dialog = page.getByRole('dialog', { name: dialogName });
  if (agent) await dialog.getByRole('radio', { name: new RegExp(`^${agent}`) }).check();
  await expect(dialog.getByRole('region', { name: 'What the agent gets' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Continue on GitHub' }).click();
  await expect(page).toHaveURL(new RegExp(`^${origin}/conversations/`));
}

async function badge(fix: Locator, text: string, timeout = 30_000): Promise<void> {
  await expect(fix.locator('header .status')).toHaveText(text, { timeout });
}

test('Claude: connect GitHub, a public repository is refused, send as the person, follow the pull request to its merge, release and draft the reply', async ({ page }) => {
  test.setTimeout(180_000);
  const desk = await signIn(page, 'E2E Autofix Claude');
  const shop = { id: desk.installation * 10 + 1, full_name: `${desk.account}/shop-web` };
  const site = { id: desk.installation * 10 + 2, full_name: `${desk.account}/website` };
  await github.installation({ id: desk.installation, account: desk.account, users: ['octocat'] });
  await github.repo({ installation_id: desk.installation, ...shop, visibility: 'private' });
  await github.repo({ installation_id: desk.installation, ...site, visibility: 'public' });
  const tokensBefore = await github.state();

  await connectGitHub(page, desk);
  // Every user token the install used is revoked at once.
  await expect.poll(async () => (await github.state()).revoked_tokens).toBe(tokensBefore.revoked_tokens + 1);

  // The public repository is shown, greyed, with no way to connect it; the API refuses it too.
  await expect(repoRow(page, site.full_name)).toContainText('Can’t connect: customer tickets would become public in its issues.');
  await expect(repoRow(page, site.full_name).getByRole('button', { name: 'Connect' })).toHaveCount(0);
  const settings = await (await page.request.get(`${origin}/api/v1/autofix/settings`)).json() as { installations: { id: string }[] };
  const refused = await page.request.post(`${origin}/api/v1/autofix/repos`, { headers: { Origin: origin },
    data: { installation_id: settings.installations[0].id, github_repo_id: site.id } });
  expect(refused.status()).toBe(409);
  expect((await refused.json()).error.code).toBe('repo_not_private');

  await connectRepo(page, shop.full_name);
  await turnOn(page, 'Claude');

  // A customer whose text would mention people, start Claude and link issues if it went out as written.
  await receive(page, desk, 'Checkout button does nothing', 'Hi, @claude fix this please, or ask @someone. It looks like #123 again. Call me on +1 415 555 0100.');
  const fix = block(page);
  await fix.getByRole('button', { name: 'Send to agent' }).click();
  const dialog = page.getByRole('dialog', { name: 'Send to a coding agent' });
  await expect(dialog).toContainText('1 phone number');
  await expect(dialog).toContainText('2 mentions turned off');
  await expect(dialog).toContainText(`Creates an issue in ${shop.full_name} with your GitHub account.`);
  await sendThroughGitHub(page, 'Send to a coding agent');

  await badge(fix, 'Sent');
  const [issue] = await github.issues(shop.id);
  expect(issue.author).toBe('octocat');
  // The issue carries no live mention or reference; only Colliedesk's one comment, as the person, starts Claude.
  expect(issue.title + issue.body).not.toMatch(/@claude|@someone|#123/);
  expect(issue.body).not.toContain('555 0100');
  expect(issue.comments.filter((comment) => comment.body.includes('@claude'))).toEqual([expect.objectContaining({ author: 'octocat' })]);
  await expect(fix).toContainText(`Issue #${issue.number} is open and asks Claude to fix it.`);
  await expect(fix).toContainText('created by @octocat');
  await expect(fix.getByRole('list', { name: 'Activity, newest first' })).toContainText(`Issue #${issue.number} created by @octocat`);
  // The person's token was revoked once the issue existed.
  const tokens = await github.state();
  expect(tokens.revoked_tokens).toBe(tokens.user_tokens);

  // Claude opens a pull request that closes the issue, and someone on the team merges it.
  const pr = 100 + issue.number;
  await github.pull({ repo_id: shop.id, number: pr, head_ref: `claude/issue-${issue.number}-checkout`, closes: [issue.number] });
  await github.pullEvent({ action: 'opened', installation_id: desk.installation, repo_id: shop.id, number: pr });
  await badge(fix, 'PR open');
  await expect(fix).toContainText('found by Colliedesk');
  await github.pull({ repo_id: shop.id, number: pr, state: 'closed', merged: true, merge_sha: 'abc1234def5678abc1234def5678abc1234def56', merged_by: 'octocat',
    head_ref: `claude/issue-${issue.number}-checkout`, closes: [issue.number] });
  await github.pullEvent({ action: 'closed', installation_id: desk.installation, repo_id: shop.id, number: pr });
  await badge(fix, 'Merged');
  await expect(fix).toContainText('abc1234');

  await fix.getByRole('button', { name: 'Mark as released' }).click();
  await badge(fix, 'Released');
  // AI is off in this workspace: Draft reply puts a plain reply in the reply box.
  await fix.getByRole('button', { name: 'Draft reply' }).click();
  await expect(fix.getByRole('status').filter({ hasText: 'A plain reply was put in the reply box to edit.' })).toBeVisible();
  await expect(page.getByLabel('Message body').last()).toHaveValue(/The fix for this is now released\./);

  // Fixes lists it as done.
  await page.getByRole('link', { name: 'Fixes' }).click();
  await expect(page.getByRole('heading', { name: 'Fixes', level: 1 })).toBeVisible();
  await expect(page.getByRole('main')).toContainText('Checkout button does nothing');
});

test('Cursor: the workspace key, send, the run finishes with a pull request, merged on GitHub', async ({ page }) => {
  test.setTimeout(180_000);
  const desk = await signIn(page, 'E2E Autofix Cursor');
  const api = { id: desk.installation * 10 + 1, full_name: `${desk.account}/api` };
  await github.installation({ id: desk.installation, account: desk.account, users: ['octocat'] });
  await github.repo({ installation_id: desk.installation, ...api, visibility: 'private' });
  await connectGitHub(page, desk);
  await connectRepo(page, api.full_name);

  const card = vendorCard(page, 'Cursor');
  await card.getByPlaceholder('Paste the key').fill(cursorStandIn.key);
  await card.getByRole('checkbox', { name: /I agree that/ }).check();
  await card.getByRole('button', { name: 'Save and turn on' }).click();
  await expect(card.getByText('Key set', { exact: true })).toBeVisible();
  await expect(card.getByText('On', { exact: true })).toBeVisible();
  // Not one character of the key is shown again.
  await expect(page.locator('body')).not.toContainText(cursorStandIn.key.slice(-6));

  const pr = 7;
  await cursor.script([{ status: 'RUNNING' }, { status: 'FINISHED', result: 'Fixed the rounding.', branch: 'cursor/fix-rounding', pr_url: `https://github.com/${api.full_name}/pull/${pr}` }]);
  await receive(page, desk, 'Invoice total off by one cent', 'The invoice total is one cent more than the sum of the lines.');
  const fix = block(page);
  await fix.getByRole('button', { name: 'Send to agent' }).click();
  const dialog = page.getByRole('dialog', { name: 'Send to a coding agent' });
  await expect(dialog).toContainText('Sends to Cursor with the workspace key.');
  await dialog.getByRole('button', { name: 'Send to Cursor' }).click();

  // The worker creates the agent, then reads its run until it opens the pull request.
  await badge(fix, 'Sent', 60_000);
  const created = (await cursor.state()).creates.at(-1)!;
  expect(created.repo).toBe(`https://github.com/${api.full_name}`);
  expect(created.prompt).toContain('one cent more');
  await badge(fix, 'PR open', 90_000);
  await expect(fix).toContainText(`#${pr}`);

  await github.pull({ repo_id: api.id, number: pr, state: 'closed', merged: true, merge_sha: 'c0ffee1234c0ffee1234c0ffee1234c0ffee1234', merged_by: 'octocat', head_ref: 'cursor/fix-rounding' });
  await github.pullEvent({ action: 'closed', installation_id: desk.installation, repo_id: api.id, number: pr });
  await badge(fix, 'Merged');
  await expect(fix).toContainText('c0ffee1');
});

test('cancel and retry a Claude fix, and Copilot assigned on GitHub when Colliedesk cannot assign it', async ({ page }) => {
  test.setTimeout(180_000);
  const desk = await signIn(page, 'E2E Autofix Retry');
  const app = { id: desk.installation * 10 + 1, full_name: `${desk.account}/app` };
  const mobile = { id: desk.installation * 10 + 2, full_name: `${desk.account}/mobile` };
  await github.installation({ id: desk.installation, account: desk.account, users: ['octocat'] });
  await github.repo({ installation_id: desk.installation, ...app, visibility: 'private' });
  await github.repo({ installation_id: desk.installation, ...mobile, visibility: 'private' });
  await connectGitHub(page, desk);
  await connectRepo(page, app.full_name);
  await connectRepo(page, mobile.full_name);
  await turnOn(page, 'Claude');
  await turnOn(page, 'Copilot');

  await receive(page, desk, 'Password reset email never arrives', 'I asked for a reset link three times and got nothing.');
  const fix = block(page);
  await fix.getByRole('button', { name: 'Send to agent' }).click();
  await page.getByRole('dialog', { name: 'Send to a coding agent' }).getByRole('combobox', { name: 'Repository' }).selectOption({ label: app.full_name });
  await sendThroughGitHub(page, 'Send to a coding agent', 'Claude');
  await badge(fix, 'Sent');
  const [first] = await github.issues(app.id);

  // Cancel closes the issue on GitHub with a comment; the block says the agent may still run.
  await fix.getByRole('button', { name: 'Cancel' }).click();
  await page.getByRole('alertdialog', { name: 'Cancel this fix?' }).getByRole('button', { name: 'Cancel fix' }).click();
  await badge(fix, 'Cancelled');
  await expect(fix).toContainText('Colliedesk commented on the issue and closed it.');
  await expect.poll(async () => (await github.issues(app.id))[0].state).toBe('closed');
  expect((await github.issues(app.id))[0].comments.some((comment) => comment.author === 'app')).toBe(true);

  // Retry is a new attempt with a new issue; the first stays closed.
  await fix.getByRole('button', { name: 'Retry (attempt 2)' }).click();
  await sendThroughGitHub(page, 'Retry · attempt 2');
  await badge(fix, 'Sent');
  await expect(fix).toContainText('attempt 2');
  const issues = await github.issues(app.id);
  expect(issues).toHaveLength(2);
  expect(issues[1].number).not.toBe(first.number);

  // Copilot: Colliedesk may assign it (suggestedActors lists it) but GitHub refuses the assignment, so the issue
  // waits for a person to assign it on GitHub; the assignment GitHub reports moves the fix on.
  await github.copilot(mobile.id, true);
  await github.fail(`POST /repos/${mobile.full_name}/issues/1/assignees`, 403);
  await receive(page, desk, 'Dark mode toggle resets', 'Every time I open the app, dark mode is off again.');
  await fix.getByRole('button', { name: 'Send to agent' }).click();
  await page.getByRole('dialog', { name: 'Send to a coding agent' }).getByRole('combobox', { name: 'Repository' }).selectOption({ label: mobile.full_name });
  await sendThroughGitHub(page, 'Send to a coding agent', 'Copilot');
  await badge(fix, 'Assign on GitHub');
  await expect(fix.getByRole('link', { name: 'Assign to Copilot on GitHub' })).toHaveAttribute('href', `https://github.com/${mobile.full_name}/issues/1`);
  await github.issueEvent({ action: 'assigned', installation_id: desk.installation, repo_id: mobile.id, number: 1, assignee: 'copilot-swe-agent[bot]' });
  await badge(fix, 'Agent running');
});
