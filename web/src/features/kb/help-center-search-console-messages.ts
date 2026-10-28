// Strings of the Search Console part of the help center card; they live in the card's chunk.
export const searchConsoleMessages = {
  heading: 'Search Console',
  description: 'Verify your help center address in Google Search Console and Bing Webmaster Tools yourself. Each tool offers a verification by HTML tag: paste only the content value of its meta tag here. The help center then puts the tag in the head of every page of this help center, and nowhere else.',
  loadFailed: 'The Search Console tokens could not be loaded.',
  googleLabel: 'Google Search Console token',
  googleHelp: 'The content of <meta name="google-site-verification" content="…">.',
  bingLabel: 'Bing Webmaster Tools token',
  bingHelp: 'The content of <meta name="msvalidate.01" content="…">.',
  save: 'Save tokens',
  saving: 'Saving…',
  saved: 'Saved. The tags are on every page of the help center.',
  set: (tool: string) => `${tool}: set.`,
  notSet: (tool: string) => `${tool}: not set.`,
  google: 'Google Search Console',
  bing: 'Bing Webmaster Tools',
  adminsOnly: 'Only an admin can change the tokens.',
};
