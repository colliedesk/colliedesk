// shape writes a node as its elements, sorted attributes and text, so two renderings compare whatever their
// line breaks between blocks. Heading ids are the help center's own, and each surface loads a knowledge base
// image from its own address (/api/v1/kb/images/ID in the app, /images/ID on the help center), so an image
// is compared by its id.
export function shape(node: Node): string {
  node.normalize();
  return shapeOf(node).replace(/\s*(<\/?(?:p|h[1-6]|table|tbody|tr|th|td|figure|figcaption)\b[^>]*>)\s*/g, '$1');
}

const value = (attribute: Attr) =>
  attribute.name === 'src' ? attribute.value.replace(/^(?:\/api\/v1\/kb)?\/images\//, 'kb-image:') : attribute.value;

function shapeOf(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) return (node.nodeValue ?? '').replace(/\s+/g, ' ');
  if (node.nodeType !== Node.ELEMENT_NODE && node.nodeType !== Node.DOCUMENT_FRAGMENT_NODE) return '';
  const children = Array.from(node.childNodes).map(shapeOf).join('');
  if (node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) return children;
  const element = node as Element;
  const attributes = Array.from(element.attributes).filter((a) => a.name !== 'id').map((a) => ` ${a.name}="${value(a)}"`).sort().join('');
  return `<${element.localName}${attributes}>${children}</${element.localName}>`;
}
