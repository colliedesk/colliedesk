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

// clerkFrontendHost reads the host of Clerk's Frontend API from a publishable key (pk_live_ or pk_test_
// followed by base64 of "<host>$"), so the page can open the connection before Clerk's script asks.
export function clerkFrontendHost(publishableKey: string): string | undefined {
  const encoded = /^pk_(?:live|test)_([A-Za-z0-9+/=]+)$/.exec(publishableKey)?.[1];
  if (!encoded) return undefined;
  try {
    const host = atob(encoded).replace(/\$$/, '');
    return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(host) ? host : undefined;
  } catch {
    return undefined;
  }
}

// preconnectClerk opens the TLS connection to Clerk's host once; on a slow line it saves the handshake
// round trips before Clerk's script is requested.
export function preconnectClerk(publishableKey: string): void {
  const host = clerkFrontendHost(publishableKey);
  if (!host || document.querySelector(`link[rel="preconnect"][href="https://${host}"]`)) return;
  const link = document.createElement('link');
  link.rel = 'preconnect';
  link.href = `https://${host}`;
  link.crossOrigin = 'anonymous';
  document.head.append(link);
}
