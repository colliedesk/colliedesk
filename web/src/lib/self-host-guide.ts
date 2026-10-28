// selfHostGuide is the address of a section of the self-hosting guide, for a screen that tells a person
// to ask whoever runs the server: it points them at the exact steps without naming server variables.
export const selfHostGuide = (page: string, section: string) =>
  `https://github.com/colliedesk/colliedesk/blob/main/docs/self-host/${page}#${section}`;
