import { spawn, spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { ports, scratchDir } from './checkout';
import type { ChildProcess } from 'node:child_process';

// Ports and URLs shared with playwright.config.ts.
export const servers = {
  main: `http://localhost:${ports.main}`,
  cloud: `http://localhost:${ports.cloud}`,
  mail: `http://localhost:${ports.mail}`,
} as const;

export const databaseURL = process.env.TEST_DATABASE_URL
  ?? 'postgres://colliedesk:colliedesk@127.0.0.1:55441/colliedesk_test?sslmode=disable';

export const goEnv = {
  GOPATH: '/tmp/colliedesk-gopath', GOMODCACHE: '/tmp/colliedesk-gopath/pkg/mod', GOCACHE: '/tmp/colliedesk-go-cache',
};

export const attachmentsDir = join(scratchDir, 'attachments');

export interface CommandResult { status: number | null; stdout: string; stderr: string }

interface CommandOptions { origin?: string; databaseURL?: string; env?: Record<string, string>; tags?: string }

const binDir = join(scratchDir, 'bin');
const built = new Set<string>();

// binary builds the real CLI from source once per test worker; tags 'e2e' adds only the
// fake-Freshdesk network seam (cmd/colliedesk/e2e_freshdesk.go).
export function binary(tags = ''): string {
  const path = `${binDir}/colliedesk${tags ? `-${tags}` : ''}`;
  if (!built.has(path)) {
    mkdirSync(binDir, { recursive: true });
    const result = spawnSync('go', ['build', ...(tags ? ['-tags', tags] : []), '-o', path, './cmd/colliedesk'], {
      cwd: '../server', encoding: 'utf8', env: { ...process.env, ...goEnv },
    });
    if (result.status !== 0) throw new Error(`go build failed: ${result.stderr}`);
    built.add(path);
  }
  return path;
}

export function colliedesk(args: string[], options: CommandOptions = {}): CommandResult {
  const result = spawnSync(binary(options.tags), args, {
    encoding: 'utf8', timeout: 300_000,
    env: {
      ...process.env, ...goEnv, ATTACHMENTS_DIR: attachmentsDir,
      APP_ORIGIN: options.origin ?? servers.main, DATABASE_URL: options.databaseURL ?? databaseURL, ...options.env,
    },
  });
  return { status: result.status, stdout: result.stdout, stderr: result.stderr };
}

// colliedeskAsync keeps the event loop free, for commands that talk to a fake served by this process.
export function colliedeskAsync(args: string[], options: CommandOptions = {}): Promise<CommandResult> {
  return new Promise((resolve, reject) => {
    const child = spawn(binary(options.tags), args, {
      env: {
        ...process.env, ...goEnv, ATTACHMENTS_DIR: attachmentsDir,
        APP_ORIGIN: options.origin ?? servers.main, DATABASE_URL: options.databaseURL ?? databaseURL, ...options.env,
      },
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk: Buffer) => { stdout += chunk.toString(); });
    child.stderr.on('data', (chunk: Buffer) => { stderr += chunk.toString(); });
    child.on('error', reject);
    child.on('close', (status) => resolve({ status, stdout, stderr }));
  });
}

export function mustColliedesk(args: string[], options: CommandOptions = {}): string {
  const result = colliedesk(args, options);
  if (result.status !== 0) throw new Error(`colliedesk ${args.join(' ')} exited ${result.status}: ${result.stderr}`);
  return result.stdout.trim();
}

export function uniqueSuffix(): string {
  return `${Date.now()}-${process.pid}-${Math.floor(Math.random() * 1e6)}`;
}

export function createWorkspace(name: string, adminEmail: string, origin: string = servers.main, database?: string): string {
  return mustColliedesk(['create-workspace', '--name', name, '--admin-email', adminEmail], { origin, databaseURL: database });
}

export function psql(sql: string, url: string = databaseURL): string {
  const result = spawnSync('psql', [url, '-v', 'ON_ERROR_STOP=1', '-Atc', sql], { encoding: 'utf8' });
  if (result.status !== 0) throw new Error(`psql failed: ${result.stderr}`);
  return result.stdout.trim();
}

export function withDatabase(url: string, database: string): string {
  const parsed = new URL(url);
  parsed.pathname = `/${database}`;
  return parsed.toString();
}

// startServer runs `colliedesk serve` on its own port and resolves once it answers.
export async function startServer(port: number, env: Record<string, string>): Promise<ChildProcess> {
  const child = spawn(binary(), ['serve', '--listen', `127.0.0.1:${port}`], {
    stdio: ['ignore', 'inherit', 'inherit'],
    env: { ...process.env, ...goEnv, ATTACHMENTS_DIR: attachmentsDir, ...env },
  });
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`colliedesk serve exited with ${child.exitCode}`);
    try {
      const response = await fetch(`http://localhost:${port}/`);
      if (response.ok) return child;
    } catch { /* not listening yet */ }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  child.kill('SIGTERM');
  throw new Error(`colliedesk serve on ${port} did not start`);
}

export async function stopServer(child: ChildProcess | undefined): Promise<void> {
  if (!child || child.exitCode !== null) return;
  const exited = new Promise((resolve) => child.once('exit', resolve));
  child.kill('SIGTERM');
  await exited;
}
