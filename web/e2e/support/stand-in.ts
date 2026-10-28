import { spawn, spawnSync } from 'node:child_process';
import type { ChildProcess } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { scratchDir } from './checkout';
import { goEnv } from './cli';

// startStandIn builds server/cmd/<name> and runs it with args, resolving once GET <ready> answers.
export async function startStandIn(name: string, args: string[], ready: string): Promise<ChildProcess> {
  const bin = join(scratchDir, 'bin', name);
  mkdirSync(join(scratchDir, 'bin'), { recursive: true });
  const built = spawnSync('go', ['build', '-o', bin, `./cmd/${name}`], { cwd: '../server', encoding: 'utf8', env: { ...process.env, ...goEnv } });
  if (built.status !== 0) throw new Error(`go build ${name} failed: ${built.stderr}`);
  const child = spawn(bin, args, { stdio: ['ignore', 'inherit', 'inherit'] });
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) throw new Error(`${name} exited with ${child.exitCode}`);
    try {
      if ((await fetch(ready)).ok) return child;
    } catch { /* not listening yet */ }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  child.kill('SIGTERM');
  throw new Error(`${name} did not start`);
}
