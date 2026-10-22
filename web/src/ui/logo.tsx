// The collie in profile wearing a support headset, on its dark tile. The white ruff and headset need a
// dark ground, so the mark is never drawn bare on a light surface; the tile is part of the logo. The
// link or heading that holds it carries the name. Source and favicon: docs/internals/design.md#logo.
export function LogoMark({ size = 28 }: { size?: number }) {
  return (
    <svg className="logo-mark" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <rect width="64" height="64" rx="14" style={{ fill: 'var(--logo-tile)' }} />
      <path d="M8 62C6 48 9 36 15 28L19 8L27 12.5L25 14L29 18.5C34.5 18 38.5 20 42 23L57.5 31.5Q60.5 34 57.5 36.5L44 38.5C40.5 39.5 38.5 41.5 37.5 44L48 62Z" fill="#8f8f8f" />
      <path d="M19 8L27 12.5L25 14L21.5 13Z" fill="#525252" />
      <path d="M37.5 44C39 50 44 56 48 62H20C23 60 21 57 24.5 55.5C21.5 53 24 49.5 27.5 49C25.5 46 28.5 42.5 32 43.5C32.5 41 35.5 40.5 37.5 44Z" fill="#fafafa" />
      <path d="M44 38.5L57.5 36.5Q55 39 50 39.6Z" fill="#fafafa" />
      <path d="M34.5 25.6Q37 24.4 39 26Q37 27.4 34.5 25.6Z" fill="#0a0a0a" />
      <ellipse cx="57.6" cy="33.8" rx="2.5" ry="2.1" fill="#0a0a0a" />
      <path d="M10.5 36C7 19 21 6 34 15.5" fill="none" stroke="#fafafa" strokeWidth="3" strokeLinecap="round" />
      <rect x="6" y="30" width="9.5" height="13" rx="4.5" fill="#fafafa" />
      <path d="M13 42C20 49 31 47 39 43" fill="none" stroke="#fafafa" strokeWidth="2.4" strokeLinecap="round" />
      <circle cx="40" cy="42.6" r="2.7" fill="#fafafa" />
    </svg>
  );
}

// The wordmark as outlined letters, so it needs no font: "collie" bold, "desk" regular, the second "l"
// tipped like a collie ear. Only the always-dark sign-in brand panel shows it, so it is a static file
// with fixed colours (public/wordmark-on-dark.svg) and adds nothing to the JS bundle.
export function LogoWordmark({ height = 24 }: { height?: number }) {
  return <img className="wordmark" src="/wordmark-on-dark.svg" alt="Colliedesk" height={height} />;
}
