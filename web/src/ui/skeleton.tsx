import './skeleton.css';

// A grey bar in place of one line of text. Its parent's class sets the line height, so the page
// keeps the height it will have once the words arrive.
export function SkeletonLine({ width = '100%' }: { width?: string }) {
  return <i className="skeleton-line" style={{ width }} />;
}

// A grey circle in place of a status dot or an avatar.
export function SkeletonDot({ size }: { size: number }) {
  return <i className="skeleton-dot" style={{ width: size, height: size }} />;
}
