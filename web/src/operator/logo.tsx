// The collie mark of ui/logo.tsx, coloured by class instead of a style attribute: the console's CSP allows no
// inline style. Source: docs/internals/design.md#logo.
export function ConsoleLogo() {
  return (
    <svg className="logo-mark" width={24} height={24} viewBox="0 0 64 64" aria-hidden="true" focusable="false">
      <rect className="logo-tile" width="64" height="64" rx="14" />
      <path d="M8 62C6 48 9 36 15 28L19 8L27 12.5L25 14L29 18.5C34.5 18 38.5 20 42 23L57.5 31.5Q60.5 34 57.5 36.5L44 38.5C40.5 39.5 38.5 41.5 37.5 44L48 62Z" fill="#fafafa" />
      <path className="logo-tile" d="M33.5 25.6Q37 23.6 40 26Q37 28.2 33.5 25.6Z" />
      <ellipse className="logo-tile" cx="57.6" cy="33.8" rx="3" ry="2.6" />
    </svg>
  );
}
