import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { browserTimeZone } from './format-date';

// A new workspace reads times in UTC until someone picks a zone, which shows a person in Hanoi or Denver a
// clock hours off theirs. So an agent who never chose a zone, in a workspace still on that UTC default,
// gets the browser's zone once at sign-in, saved as their own (Settings → Preferences changes it). It is
// the person's own clock, not a guess from a country. This browser remembers it did so, so an agent who
// later goes back to the workspace's zone keeps that choice.
const donePrefix = 'colliedesk.timezone-defaulted:';

interface MeZones { agent: { id: string; timezone_override: string | null }; workspace: { timezone: string } }

export async function defaultAgentTimezone(me: MeZones, browser: string, save: (zone: string) => Promise<unknown>): Promise<boolean> {
  if (me.agent.timezone_override !== null || me.workspace.timezone !== 'UTC' || !browser || browser === 'UTC' || browser === 'Etc/UTC') return false;
  try {
    if (localStorage.getItem(donePrefix + me.agent.id)) return false;
  } catch {
    return false;
  }
  try {
    await save(browser);
  } catch {
    // Tried again at the next sign-in.
    return false;
  }
  try {
    localStorage.setItem(donePrefix + me.agent.id, '1');
  } catch {
    // Storage off: the override is saved, and a set override is never replaced.
  }
  return true;
}

export function useBrowserTimezoneDefault(me: MeZones | undefined) {
  const client = useQueryClient();
  useEffect(() => {
    if (!me) return;
    void defaultAgentTimezone(me, browserTimeZone(), api.updateAgentTimezone).then((saved) => {
      if (saved) void client.invalidateQueries({ queryKey: ['me'] });
    });
  }, [me, client]);
}
