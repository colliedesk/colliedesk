// A frame is never taller than this, whatever the mail asks for.
const maxHeight = 20_000;

// fitFrame sizes the email frame to its content so the conversation scrolls as one page and the frame
// never scrolls inside. Content wider than the frame (a fixed 650 px newsletter in a narrow column) is
// scaled down instead of clipped. It is safe to call often: it measures, then writes.
export function fitFrame(frame: HTMLIFrameElement): void {
  const doc = frame.contentDocument;
  if (!doc?.body) return;
  const { body, documentElement: root } = doc;
  // Measure with the frame collapsed: a block sized by the viewport (height:100%) then takes its natural
  // size instead of the frame's, so the answer does not depend on the last answer. The box keeps the old
  // height meanwhile, so the page around it does not shrink, clamp its scroll position and jump.
  const box = frame.parentElement;
  if (box) box.style.minHeight = `${parseFloat(frame.style.height) || frame.offsetHeight}px`;
  frame.style.height = '0px';
  try {
    measure(frame, body, root);
  } finally {
    if (box) box.style.minHeight = '';
  }
}

function measure(frame: HTMLIFrameElement, body: HTMLElement, root: HTMLElement): void {
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
    frame.style.height = `${Math.min(Math.ceil(Math.max(root.scrollHeight, body.scrollHeight) * scale), maxHeight)}px`;
    return;
  }
  frame.style.height = `${Math.min(Math.max(root.scrollHeight, body.scrollHeight), maxHeight)}px`;
}
