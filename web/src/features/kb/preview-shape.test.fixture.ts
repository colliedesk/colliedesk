// shape writes a node as its elements, sorted attributes and text, so two renderings compare whatever their
// line breaks between blocks. Heading ids are the help center's own.
export function shape(node: Node): string {
  node.normalize();
  return shapeOf(node).replace(/\s*(<\/?(?:p|h[1-6]|table|tbody|tr|th|td)\b[^>]*>)\s*/g, '$1');
}

function shapeOf(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return (node.nodeValue ?? '').replace(/\s+/g, ' ');
  if (node.nodeType !== Node.ELEMENT_NODE && node.nodeType !== Node.DOCUMENT_FRAGMENT_NODE) return '';
  const children = Array.from(node.childNodes).map(shapeOf).join('');
  if (node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) return children;
  const element = node as Element;
  const attributes = Array.from(element.attributes).filter((a) => a.name !== 'id').map((a) => ` ${a.name}="${a.value}"`).sort().join('');
  return `<${element.localName}${attributes}>${children}</${element.localName}>`;
}
