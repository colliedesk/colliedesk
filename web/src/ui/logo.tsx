// The blue-merle collie head. Dark surfaces only; the link or heading that holds it carries the name.
export function LogoMark({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true" focusable="false">
      <path d="M9 17 L2.5 24.5 L7 29.5 L14 31 Z M23 17 L29.5 24.5 L25 29.5 L18 31 Z" fill="#eef2f7" />
      <path d="M6.6 16.6 L7.2 2.6 L13 3.6 L15.5 11 Z" fill="#5d7aa8" />
      <path d="M7.2 2.6 L13 3.6 L2.2 10.6 Z" fill="#3f5680" />
      <path d="M25.4 16.6 L24.8 2.6 L19 3.6 L16.5 11 Z" fill="#8aa6cf" />
      <path d="M24.8 2.6 L19 3.6 L29.8 10.6 Z" fill="#6a88b6" />
      <path d="M10.5 10 Q16 7.8 21.5 10 L27 18 L19.5 29 Q16 31.5 12.5 29 L5 18 Z" fill="#8aa6cf" />
      <path d="M15 8.6 L17 8.6 L20 28 Q16 31.5 12 28 Z" fill="#eef2f7" />
      <circle cx="11.6" cy="15.4" r="0.9" fill="#d4914a" /><circle cx="20.4" cy="15.4" r="0.9" fill="#d4914a" />
      <ellipse cx="11.9" cy="18.3" rx="1.45" ry="1.15" fill="#0b0f15" /><ellipse cx="20.1" cy="18.3" rx="1.45" ry="1.15" fill="#0b0f15" />
      <ellipse cx="16" cy="28.4" rx="2.6" ry="1.9" fill="#0b0f15" />
    </svg>
  );
}
