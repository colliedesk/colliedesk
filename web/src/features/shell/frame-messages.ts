// Words of the app frame's search field, search palette rows and Quick start. Apart from messages.ts, which the
// operator console shares: the console has none of these and its bundle has a budget.
export const frameMessages = {
  searchWorkspace: (name: string) => `Search ${name}`,
  openCount: (count: number) => `${count} open`,
  unreadCount: (count: number) => `${count} unread`,
  settingsDetail: 'Channels, members, plan',
  quickStart: 'Quick start',
  quickStartLeft: (count: number) => `Quick start, ${count} ${count === 1 ? 'step' : 'steps'} left`,
  getReady: (workspace: string) => `Get ${workspace} ready`,
};
