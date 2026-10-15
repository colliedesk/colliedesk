import { colliedesk, servers } from './cli';

export interface CreatedWorkspace { url: string; slug: string }

// createWorkspaceWithSlug runs create-workspace and reads the slug from the hint it prints for the
// self-hoster (`collie pair --workspace <slug> --email <email>`), the only place a new admin sees it.
export function createWorkspaceWithSlug(name: string, adminEmail: string, origin: string = servers.main): CreatedWorkspace {
  const result = colliedesk(['create-workspace', '--name', name, '--admin-email', adminEmail], { origin });
  if (result.status !== 0) throw new Error(`create-workspace failed: ${result.stderr}`);
  const slug = result.stderr.match(/collie pair --workspace (\S+)/)?.[1];
  if (!slug) throw new Error(`create-workspace printed no workspace slug: ${result.stderr}`);
  return { url: result.stdout.trim(), slug };
}
