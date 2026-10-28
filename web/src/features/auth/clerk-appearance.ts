// The media query under which every control is sized for a finger, as the app's own CSS does.
export const narrowViewport = '@media (max-width: 30rem)';

// Clerk's controls on a narrow window: 44 px tall, like the rest of the app.
const tapSized = { [narrowViewport]: { minHeight: '44px' } };

// Clerk draws its own form; it reads the brand tokens of the active theme so it matches the app.
export function clerkAppearance() {
  const token = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || undefined;
  return {
    variables: {
      colorBackground: token('--surface-1'), colorText: token('--text-1'), colorTextSecondary: token('--text-2'),
      colorPrimary: token('--accent'), colorTextOnPrimaryBackground: token('--on-accent'), colorInputBackground: token('--surface-0'),
      colorInputText: token('--text-1'), colorNeutral: token('--text-1'), colorDanger: token('--danger'),
      fontFamily: token('--font-sans'), borderRadius: token('--radius'),
    },
    elements: {
      // The page's own h1 already titles the first step, so Clerk's "Continue to Colliedesk" header goes.
      // Later steps keep theirs: it says "Check your email" and shows the address with a way to change it.
      card: { '&.cl-signIn-start .cl-header': { display: 'none' } },
      // Clerk's card has a fixed width; it fills the panel instead, so a phone never scrolls sideways.
      rootBox: { width: '100%' },
      cardBox: { width: '100%', maxWidth: '100%' },
      formButtonPrimary: tapSized,
      socialButtonsBlockButton: tapSized,
      formFieldInput: tapSized,
      otpCodeFieldInput: tapSized,
    },
  };
}
