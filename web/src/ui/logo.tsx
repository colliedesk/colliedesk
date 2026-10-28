// The collie head in profile as one solid white shape on its orange tile, with the eye and nose cut
// out in the tile colour. The same mark is the favicon and sits in the site wordmark. The link or
// heading that holds it carries the name. Source: docs/internals/design.md#logo.
export function LogoMark({ size = 28 }: { size?: number }) {
  return (
    <svg className="logo-mark" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <rect width="64" height="64" rx="14" style={{ fill: 'var(--logo-tile)' }} />
      <path d="M8 62C6 48 9 36 15 28L19 8L27 12.5L25 14L29 18.5C34.5 18 38.5 20 42 23L57.5 31.5Q60.5 34 57.5 36.5L44 38.5C40.5 39.5 38.5 41.5 37.5 44L48 62Z" fill="#fafafa" />
      <path d="M33.5 25.6Q37 23.6 40 26Q37 28.2 33.5 25.6Z" style={{ fill: 'var(--logo-tile)' }} />
      <ellipse cx="57.6" cy="33.8" rx="3" ry="2.6" style={{ fill: 'var(--logo-tile)' }} />
    </svg>
  );
}

// The wordmark as outlined letters, so it needs no font: "collie" bold, "desk" regular, the second "l"
// tipped like a collie ear. Only the always-dark sign-in brand panel shows it, so it is a static file
// with fixed colours (public/wordmark-on-dark.svg) and adds nothing to the JS bundle.
export function LogoWordmark({ height = 24 }: { height?: number }) {
  return <img className="wordmark" src="/wordmark-on-dark.svg" alt="Colliedesk" height={height} />;
}
