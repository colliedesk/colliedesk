import { Icon } from '../../ui/icons';

// Icons only the Autofix screens use; they load with its chunk. WrenchIcon is in ui/icons.tsx for the rail.
export const ExternalIcon = () => <Icon width={13} height={13}><path d="M15 3h6v6" /><path d="M10 14 21 3" /><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" /></Icon>;
export const KeyIcon = () => <Icon width={14} height={14}><circle cx="7.5" cy="15.5" r="5.5" /><path d="m21 2-9.6 9.6" /><path d="m15.5 7.5 3 3L22 7l-3-3" /></Icon>;
export const ShieldIcon = () => <Icon width={12} height={12}><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" /></Icon>;
export const RetryIcon = () => <Icon width={14} height={14}><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5" /></Icon>;
export const ReleaseIcon = () => <Icon width={14} height={14}><circle cx="12" cy="12" r="3" /><path d="M12 2v4" /><path d="M12 18v4" /><path d="M2 12h4" /><path d="M18 12h4" /></Icon>;
