import { expect, test } from 'vitest';
import { paletteEntries } from './palette-entries';

type Me = Parameters<typeof paletteEntries>[0];
const meWith = (permissions: string[]) => ({ permissions } as unknown as Me);

// Invite teammates is about the invite form, which is on the Invites tab of Members: People has no email field.
test('Invite teammates opens the Invites tab of Members, for someone who may invite', () => {
  const invite = paletteEntries(meWith(['team:invite']), {}).actions.find((entry) => entry.key === 'invite');
  expect(invite && 'href' in invite ? invite.href : undefined).toBe('/settings/members?tab=invites');
  expect(paletteEntries(meWith([]), {}).actions.some((entry) => entry.key === 'invite')).toBe(false);
});
