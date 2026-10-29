// Strings of the cloud sign-in and sign-up pages; they load with those pages, not with the first screen.
export const signupMessages = {
  cloudSigningIn: 'Signing in…',
  retryCloudSignIn: 'Try cloud sign-in again',
  signingOut: 'Signing out…',
  signupHeading: 'Create your workspace',
  signupExplanation: 'No workspace uses this email yet. Name your team\'s workspace to start a 14-day free trial. No card needed.',
  startTrial: 'Start free trial',
  // A visitor who came from a website "buy" link pays right after naming the workspace, so no trial wording.
  signupBuyingExplanation: 'No workspace uses this email yet. Name your team\'s workspace, then pay on the next screen.',
  createWorkspaceAndPay: 'Create workspace and continue to payment',
  creatingWorkspace: 'Creating workspace…',
  signInToWorkspace: 'Sign in to your workspace',
  useAnotherAccount: 'Use another account',
  notAnAgentHere: 'This email is not an agent of this workspace. Use another account, or find the workspace your email belongs to.',
  findYourWorkspace: 'Find your workspace',
  noWorkspaceForEmail: 'This email is not part of a workspace yet. Ask your workspace admin for an invite link, or use another account.',
  // The answers order the first steps of the empty inbox and fit our emails to the team; fixed lists, no free text.
  signupQuestions: {
    goal: {
      legend: 'What do you want Colliedesk to do for you first?',
      options: {
        leave_helpdesk: 'Move off Freshdesk or another helpdesk', shared_inbox: 'Answer support email together as a team',
        lower_cost: 'Pay less per agent', ai_agents: 'Let AI agents draft or send replies', other: 'Something else',
      },
    },
    role: {
      legend: 'What is your role?',
      options: { founder: 'Founder or owner', support_lead: 'Support lead or manager', support_agent: 'Support agent', developer: 'Developer or IT', other: 'Other' },
    },
    source: {
      legend: 'Where did you hear about Colliedesk?',
      options: {
        x: 'X (Twitter)', email: 'An email from us', search: 'Search engine',
        ai_chatgpt: 'ChatGPT', ai_claude: 'Claude', ai_gemini: 'Gemini', ai_perplexity: 'Perplexity', ai_other: 'Another AI assistant',
        tiktok: 'TikTok', youtube: 'YouTube', reddit: 'Reddit', github: 'GitHub',
        referral: 'A friend or colleague', other: 'Other',
      },
    },
  },
  signupAnswerEach: 'Answer each question to continue.',
  signupChooseOne: 'Choose one',
  signupErrors: {
    trial_already_used: 'This email already started a Colliedesk trial. Sign in to that workspace instead, or contact us if you need help.',
    rate_limited: 'Too many workspaces were created from this network today. Try again tomorrow, or contact us.',
    name_not_allowed: 'This name is not available. Choose another name, or contact us if it is your own brand.',
    slug_unavailable: 'No address was free for this name. Try a different name.',
    invalid: 'The workspace name must be 2 to 60 characters.',
    signup_unavailable: 'Sign-up is temporarily unavailable. Please try again later.',
    unavailable: 'The workspace could not be created. Try again.',
  },
} as const;
