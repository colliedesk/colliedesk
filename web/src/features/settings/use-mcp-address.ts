import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { PublicConfig } from '../../api/schemas';

// serverRealm tells Colliedesk Cloud the way the server does (realm in problem-report-text.go): the realm product
// analytics name, else cloud sign-in with billing.
export function serverRealm(config: PublicConfig): 'cloud' | 'self-hosted' {
  return (config.product_analytics.realm ?? (config.cloud_sign_in.enabled && config.billing.enabled ? 'cloud' : null)) === 'cloud' ? 'cloud' : 'self-hosted';
}

// mcpAddressOf is where an AI app signs in: APP_ORIGIN's /mcp, one address for every workspace on Cloud, where the
// page itself runs on the workspace's own host. A server from before workspace hosts sends no APP_ORIGIN and runs
// everything on the page's origin.
export function mcpAddressOf(config: PublicConfig): string {
  return `${(config.app_origin || window.location.origin).replace(/\/+$/, '')}/mcp`;
}

// useMCPAddress is this server's MCP address and whether it is Colliedesk Cloud, or undefined while the public
// config is on its way or could not be read: no address is better than a wrong one.
export function useMCPAddress(): { address: string; cloud: boolean } | undefined {
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data;
  return config ? { address: mcpAddressOf(config), cloud: serverRealm(config) === 'cloud' } : undefined;
}
