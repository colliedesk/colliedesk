// On a slow connection the sign-in form (Clerk's script, about 370 KB) must not share the line with
// anything the page can do without. signInFormReady resolves once the form says it is ready, or after
// fallbackMs, so whatever waits for it (product analytics) still starts if the form never loads.
let ready: Promise<void> | null = null;
let resolveReady: (() => void) | null = null;
let markedEarly = false;

export function signInFormReady(fallbackMs = 15_000): Promise<void> {
  if (markedEarly) return Promise.resolve();
  ready ??= new Promise<void>((resolve) => {
    resolveReady = resolve;
    window.setTimeout(resolve, fallbackMs);
  });
  return ready;
}

// markSignInFormReady is called by the sign-in form once it can be used, or by a page that has none.
export function markSignInFormReady(): void {
  markedEarly = true;
  resolveReady?.();
}

export function resetSignInFormReadyForTest(): void {
  ready = null;
  resolveReady = null;
  markedEarly = false;
}
