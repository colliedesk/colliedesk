// Strings of the invite page (Paper "05 Access", A2-a, A2-b, A2-c). inviter is the inviter's name as the
// server gives it; short is the first word of it, for the footnote's lighter "Ask Mai".
export const inviteMessages = {
  joinTitle: (workspace: string) => `Join ${workspace}`,
  invitedBy: (inviter: string) => `${inviter} invited you to the support team.`,
  factsLabel: 'Invite',
  yourEmail: 'Your email',
  role: (count: number) => (count === 1 ? 'Role' : 'Roles'),
  expires: 'Expires',
  join: (workspace: string) => `Join ${workspace}`,
  joining: 'Joining…',
  // Cloud sign-in sends a code to the invited address; a server without it signs in by links only.
  footnoteCloud: (email: string, short: string) => `No password. Next time, sign in with a code we email to ${email}. Not you? Ask ${short} for an invite to your own address.`,
  footnoteLinks: (short: string) => `No password. This browser stays signed in for 30 days; after that, ask ${short} for a new link. Not you? Ask ${short} for an invite to your own address.`,
  noSeat: (workspace: string, inviter: string, until: string) =>
    `${workspace} has used every seat on its plan, so you cannot join yet. Ask ${inviter} to free a seat or change the plan. Your link keeps working until ${until}.`,
  tryAgain: 'Try again',
  noLongerWorks: 'This invite no longer works',
  expired: (workspace: string, day: string, inviter: string) => `Your invite to ${workspace} expired on ${day}. Ask ${inviter} to send you a new one.`,
  // The server does not record who revoked a link, so the line names the inviter as the person to ask.
  revoked: (workspace: string, inviter: string) => `This invite to ${workspace} was cancelled. Ask ${inviter} if you still need access.`,
  usedCloud: (workspace: string) => `This invite was used to join ${workspace}. Sign in with your email instead.`,
  usedLinks: (workspace: string, inviter: string) => `This invite was used to join ${workspace}. Sign in from a browser that joined, or ask ${inviter} for a new link.`,
  alreadyJoined: 'Already joined? Sign in',
  signIn: 'Sign in',
};
