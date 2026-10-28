// The page the browser lands on after a purchase. The plan changes only when the payment
// provider's webhook reaches the server; this page polls until it does.
export const checkoutSuccessPath = '/settings/billing?checkout=success';

// The embed covers the whole screen while it waits for the provider's page to say it loaded, and its
// close button lives inside that page. A page the browser refuses to frame (the provider's list of hosts
// allowed to embed it lacks this one: "refused to connect"), a blocker or the provider being down never
// says so, and the person would stare at an error. A loaded page answers in about a second, so after 3
// seconds the checkout opens in this tab instead, which works in every one of those cases.
export const embedLoadTimeoutMs = 3_000;

const fullPageNotice = 'The checkout could not open inside Colliedesk, so it opens in this tab instead.';

// Best effort: the full-screen frame and the scroll lock the embed adds before the page loads.
function removeEmbedLeftovers(url: string) {
  const origin = new URL(url).origin;
  document.querySelectorAll('iframe').forEach((frame) => {
    if (frame.src.startsWith(origin)) frame.remove();
  });
  document.querySelectorAll('.polar-loader-spinner').forEach((spinner) => spinner.parentElement?.remove());
  document.body.classList.remove('polar-no-scroll');
}

// Says why the page is about to change while the hosted checkout loads, on top of everything.
function sayFullPage() {
  const notice = document.createElement('p');
  notice.setAttribute('role', 'status');
  notice.className = 'checkout-full-page';
  notice.textContent = fullPageNotice;
  document.body.append(notice);
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
      // An overlay that loads after all, while the page is already leaving, is closed at once.
      loading.then((late) => late.close(), () => undefined);
      removeEmbedLeftovers(url);
      sayFullPage();
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
