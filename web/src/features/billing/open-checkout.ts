// The page the browser lands on after a purchase. The plan changes only when the payment
// provider's webhook reaches the server; this page polls until it does.
export const checkoutSuccessPath = '/settings/billing?checkout=success';

// openCheckout shows the provider's checkout as an overlay on top of the app. The embed script is
// loaded on demand so it never weighs on the inbox. If it cannot load or start (blocked script,
// network), the browser goes to the hosted checkout instead, so a purchase is never lost.
export async function openCheckout(url: string): Promise<void> {
  try {
    const { PolarEmbedCheckout } = await import('@polar-sh/checkout/embed');
    const checkout = await PolarEmbedCheckout.create(url, { theme: 'light' });
    checkout.addEventListener('success', (event) => {
      event.preventDefault();
      window.location.assign(checkoutSuccessPath);
    });
  } catch {
    window.location.assign(url);
  }
}
