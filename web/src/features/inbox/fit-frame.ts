// fitFrame sizes the email frame to its content so the conversation scrolls as one page and the frame
// never scrolls inside. Content wider than the frame (a fixed 650 px newsletter in a narrow column) is
// scaled down instead of clipped. It is safe to call often: it measures, then writes.
export function fitFrame(frame: HTMLIFrameElement): void {
  const doc = frame.contentDocument;
  if (!doc?.body) return;
  const { body, documentElement: root } = doc;
  body.style.transform = '';
  body.style.transformOrigin = '';
  body.style.width = '';
  const available = frame.clientWidth;
  const contentWidth = Math.max(root.scrollWidth, body.scrollWidth);
  if (available > 0 && contentWidth > available + 1) {
    const scale = Math.floor((available / contentWidth) * 1000) / 1000;
    body.style.width = `${contentWidth}px`;
    body.style.transformOrigin = '0 0';
    body.style.transform = `scale(${scale})`;
    frame.style.height = `${Math.ceil(Math.max(root.scrollHeight, body.scrollHeight) * scale)}px`;
    return;
  }
  frame.style.height = `${Math.max(root.scrollHeight, body.scrollHeight)}px`;
}
