// The words of the Switch workspace dialog and of the workspace rows, in the chunks that show them. The menu
// item and the chooser's lede are in messages.ts, which the page loads anyway.
export const switchMessages = {
  title: 'Switch workspace',
  close: 'Close',
  loading: 'Loading your workspaces',
  current: 'Current',
  opening: 'Opening…',
  onlyOneTitle: 'This is your only workspace',
  onlyOneText: 'Workspaces you are invited to show up here. To run support for another team or client, create one.',
  failedTitle: 'Could not load your other workspaces',
  failedText: (appHost: string) => `Your sign-in on this address could not be checked. You can still pick a workspace on ${appHost}.`,
  tryAgain: 'Try again',
  chooseOn: (appHost: string) => `Choose on ${appHost}`,
  createWorkspace: 'Create a workspace',
  keyMove: 'Move',
  keyOpen: 'Open',
  keyClose: 'Close',
} as const;
