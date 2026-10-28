const nonTextInputs = ['radio', 'checkbox', 'button', 'submit', 'reset'];

// isTyping says whether a key went to something that takes text, where a letter is text and never a
// shortcut. Radios and checkboxes take no text, so shortcuts still work after a click on a tab or a box.
export function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target instanceof HTMLInputElement) return !nonTextInputs.includes(target.type);
  // isContentEditable covers an editor's nested nodes; the attribute check covers runtimes without it.
  return target.isContentEditable || Boolean(target.closest('[contenteditable]:not([contenteditable="false"])'))
    || ['TEXTAREA', 'SELECT'].includes(target.tagName);
}

// An input method (Vietnamese Telex/VNI on macOS, CJK) still holds marked text while it composes;
// acting on that key would send or switch before the text is committed.
export const composing = (event: KeyboardEvent): boolean => event.isComposing || event.keyCode === 229;
