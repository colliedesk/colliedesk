const hues = [24, 200, 150, 280, 340, 95];

export function initials(name: string): string {
  const words = name.trim().split(/[\s@._-]+/).filter(Boolean);
  if (words.length === 0) return '?';
  return (words[0][0] + (words.length > 1 ? words[words.length - 1][0] : '')).toUpperCase();
}

// Same name, same colour: a stable hash picks one of a few muted hues.
function hue(name: string): number {
  let sum = 0;
  for (const char of name) sum = (sum * 31 + char.charCodeAt(0)) >>> 0;
  return hues[sum % hues.length];
}

export function Avatar({ name, size = 'md' }: { name: string; size?: 'sm' | 'md' }) {
  return <span className={`avatar avatar-${size}`} aria-hidden="true" style={{ '--avatar-hue': hue(name) } as React.CSSProperties}>{initials(name)}</span>;
}
