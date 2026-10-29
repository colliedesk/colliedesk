import { Icon } from '../ui/icons';

// The console's own icons, 16 px stroke like ui/icons.tsx (whose Icon frame they reuse).
export const HomeIcon = () => <Icon><path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1Z" /></Icon>;
export const ShieldIcon = () => <Icon><path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6Z" /></Icon>;
export const GaugeIcon = () => <Icon><path d="M12 14l4-4" /><path d="M3.5 18a9 9 0 1 1 17 0" /></Icon>;
export const HistoryIcon = () => <Icon><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5" /><path d="M12 7v5l3 2" /></Icon>;
export const ListIcon = () => <Icon><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" /></Icon>;
export const UsersIcon = () => <Icon><circle cx="9" cy="8" r="4" /><path d="M2 21a7 7 0 0 1 14 0" /><path d="M16 4a4 4 0 0 1 0 8M22 21a7 7 0 0 0-4-6.3" /></Icon>;
export const KeyIcon = () => <Icon><circle cx="8" cy="15" r="4" /><path d="m10.8 12.2 9.2-9.2M17 6l3 3" /></Icon>;
export const AlertIcon = () => <Icon><path d="M12 3 2 20h20Z" /><path d="M12 10v4M12 17h.01" /></Icon>;
export const TerminalIcon = () => <Icon><path d="m4 17 6-5-6-5M12 19h8" /></Icon>;
