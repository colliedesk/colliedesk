// Strings of the custom domain part of the help center card; they live in the card's chunk.
type Status = 'none' | 'pending' | 'active';
type Purpose = 'route_and_verify' | 'route' | 'verify';

const purposes: Record<Purpose, string> = {
  route_and_verify: 'sends visitors here and proves the domain is yours',
  route: 'sends visitors here',
  verify: 'proves the domain is yours',
};

export const domainMessages = {
  heading: 'Custom domain',
  description: 'Serve the help center on an address of your own.',
  descriptionTip: 'For example help.example.com. Your other help center addresses then move there.',
  cutoverHeading: 'Moving from Freshdesk?',
  cutover: 'Verify by TXT first, then change the CNAME.',
  cutoverLabel: 'moving from Freshdesk',
  cutoverTip: 'Add only the TXT record and press Check now. Your visitors still reach Freshdesk. Change the CNAME afterwards, to the target below.',
  cutoverGuide: 'https://colliedesk.com/docs/knowledge-base#moving-from-freshdesk',
  learnMore: 'Learn more',
  trial: 'Set and verify now; it serves once you pay for a plan.',
  trialLabel: 'the trial',
  trialTip: 'Trial pages are hidden from search engines, and serving them on your domain would cost it its place in search results.',
  verifiedUnpaid: (hostname: string) => `${hostname} is verified. The help center serves there once you pay for a plan; until then it stays on its own address.`,
  state: (status: Status, hostname: string | null) => status === 'active'
    ? `The help center is served on ${hostname}.`
    : status === 'pending' ? `${hostname} is waiting for its DNS records. It starts serving as soon as a check finds them.` : 'No custom domain is set.',
  // The line under the state once a check opened the address: "HTTPS works: <link> opens this help center."
  httpsWorks: 'HTTPS works:',
  addressOpens: 'opens this help center.',
  recordsHelp: 'Add these at your DNS provider, then press Check now.',
  recordsHelpTip: 'One of the two proving records is enough. DNS changes can take a few minutes.',
  recordsLabel: 'the DNS records',
  recordsKeep: 'Keep these records at your DNS provider.',
  recordsKeepTip: 'The daily check warns here if they change, and the help center stops serving on the domain after 7 days without them.',
  failing: (since: string, until: string, hostname: string) =>
    `No DNS check has found these records since ${since}. The help center stops serving on ${hostname} if none finds them by ${until}.`,
  released: (reason: 'proof_lost' | 'taken_over', hostname: string) => reason === 'proof_lost'
    ? `${hostname} stopped serving the help center: no DNS check found its records for 7 days. Its other addresses serve it again until a check finds them.`
    : `${hostname} stopped serving the help center: another workspace's DNS records proved this host name while these were missing. Set them again and press Check now.`,
  record: (type: string, purpose: Purpose) => `${type} record: ${purposes[purpose]}`,
  recordName: 'Name',
  recordValue: 'Value',
  copyNameShort: 'Copy name',
  copyValueShort: 'Copy value',
  copyName: (type: string) => `Copy the name of the ${type} record`,
  copyValue: (type: string) => `Copy the value of the ${type} record`,
  copied: 'Copied',
  lastFinding: (finding: string) => `Last check: ${finding}`,
  lastChecked: (at: string) => `Last checked ${at}.`,
  verified: (at: string) => `Verified ${at}.`,
  hostname: 'Host name',
  save: 'Set domain',
  saving: 'Setting…',
  checkNow: 'Check now',
  checking: 'Checking…',
  remove: 'Remove domain',
  removeQuestion: 'Remove the custom domain?',
  removeConsequence: (hostname: string) => `The help center stops answering on ${hostname} at once, and its other addresses serve it again. Setting it again needs a new check.`,
  confirmRemove: 'Remove',
  adminsOnly: 'Only admins can set or remove the custom domain.',
  loadFailed: 'The custom domain could not be loaded.',
};
