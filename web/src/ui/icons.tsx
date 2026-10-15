import type { ReactNode, SVGProps } from 'react';

// Decorative 16px stroke icons; the control that holds one carries the accessible name.
function Icon({ children, ...props }: SVGProps<SVGSVGElement> & { children: ReactNode }) {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
      strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...props}>{children}</svg>
  );
}

export const InboxIcon = () => <Icon><path d="M22 12h-6l-2 3h-4l-2-3H2" /><path d="M5.5 5.1 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.5-6.9A2 2 0 0 0 16.8 4H7.2a2 2 0 0 0-1.7 1.1Z" /></Icon>;
export const SettingsIcon = () => <Icon><path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6" /></Icon>;
export const SearchIcon = () => <Icon><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></Icon>;
export const ChevronLeftIcon = () => <Icon><path d="m15 18-6-6 6-6" /></Icon>;
export const ChevronRightIcon = () => <Icon><path d="m9 18 6-6-6-6" /></Icon>;
export const PaperclipIcon = () => <Icon><path d="m21.4 11-9.2 9.2a6 6 0 0 1-8.5-8.5l9.2-9.2a4 4 0 0 1 5.7 5.7l-9.2 9.2a2 2 0 0 1-2.8-2.8l8.5-8.5" /></Icon>;
export const LockIcon = () => <Icon><rect x="4" y="11" width="16" height="10" rx="2" /><path d="M8 11V7a4 4 0 0 1 8 0v4" /></Icon>;
export const ClockIcon = () => <Icon><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></Icon>;
export const ArrowUpIcon = () => <Icon><path d="M12 19V5M5 12l7-7 7 7" /></Icon>;
export const ReplyIcon = () => <Icon><path d="M9 14 4 9l5-5" /><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11" /></Icon>;
export const FilterIcon = () => <Icon><path d="M3 6h18M7 12h10M10 18h4" /></Icon>;
