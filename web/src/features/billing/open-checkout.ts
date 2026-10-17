// The page the browser lands on after a purchase. The plan changes only when the payment
// provider's webhook reaches the server; this page polls until it does.
export const checkoutSuccessPath = '/settings/billing?checkout=success';

// The embed covers the whole screen while it waits for the provider's page, and its close button
// lives inside that page. A page that never answers (host not allowed to embed it, a blocker, the
// provider down) would trap the user, so the wait is bounded.
export const embedLoadTimeoutMs = 15_000;

// Best effort: the full-screen frame and the scroll lock the embed adds before the page loads.
function removeEmbedLeftovers(url: string) {
  const origin = new URL(url).origin;
  document.querySelectorAll('iframe').forEach((frame) => {
    if (frame.src.startsWith(origin)) frame.remove();
  });
  document.body.classList.remove('polar-no-scroll');
}

// openCheckout shows the provider's checkout as an overlay on top of the app. The embed script is
// loaded on demand so it never weighs on the inbox. If it cannot load, does not answer in time or
// fails to start, the browser goes to the hosted checkout instead, so a purchase is never lost.
export async function openCheckout(url: string): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<'timeout'>((resolve) => { timer = setTimeout(() => resolve('timeout'), embedLoadTimeoutMs); });
  try {
    const loading = (async () => {
      const { PolarEmbedCheckout } = await import('@polar-sh/checkout/embed');
      return PolarEmbedCheckout.create(url, { theme: 'light' });
    })();
    loading.catch(() => undefined);
    const checkout = await Promise.race([loading, timedOut]);
    if (checkout === 'timeout') {
      removeEmbedLeftovers(url);
      window.location.assign(url);
      return;
    }
    checkout.addEventListener('success', (event) => {
      event.preventDefault();
      window.location.assign(checkoutSuccessPath);
    });
  } catch {
    window.location.assign(url);
  } finally {
    clearTimeout(timer);
  }
}
