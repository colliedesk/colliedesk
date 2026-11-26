import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { PublicConfig } from '../../api/schemas';

// serverRealm tells Colliedesk Cloud the way the server does (realm in problem-report-text.go): the realm product
// analytics name, else cloud sign-in with billing.
export function serverRealm(config: PublicConfig): 'cloud' | 'self-hosted' {
  return (config.product_analytics.realm ?? (config.cloud_sign_in.enabled && config.billing.enabled ? 'cloud' : null)) === 'cloud' ? 'cloud' : 'self-hosted';
}

// mcpAddressOf is where an AI app signs in: APP_ORIGIN's /mcp, one address for every workspace on Cloud, where the
// page itself runs on the workspace's own host, so the page's origin is never a stand-in for it.
export function mcpAddressOf(config: PublicConfig): string | undefined {
  const origin = (config.app_origin || '').replace(/\/+$/, '');
  return origin ? `${origin}/mcp` : undefined;
}

// useMCPAddress is this server's MCP address and whether it is Colliedesk Cloud, or undefined while the public
// config is on its way, could not be read or names no APP_ORIGIN: no address is better than a wrong one.
export function useMCPAddress(): { address: string; cloud: boolean } | undefined {
  const config = useQuery({ queryKey: ['public-config'], queryFn: api.config }).data;
  const address = config && mcpAddressOf(config);
  return config && address ? { address, cloud: serverRealm(config) === 'cloud' } : undefined;
}
