import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { scratchDir } from './checkout';

// The DNS the main server's domain checks read instead of the network (cmd/collie/e2e_dns.go): a journey
// writes the records a domain owner would set, and the server reads the file on every question.
export const dnsFile = join(scratchDir, 'dns.json');

export interface StandInRecords { cname?: Record<string, string>; txt?: Record<string, string[]> }

export function setDNS(records: StandInRecords) {
  mkdirSync(scratchDir, { recursive: true });
  writeFileSync(dnsFile, JSON.stringify(records));
}
