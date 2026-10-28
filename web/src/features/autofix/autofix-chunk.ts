// Every Autofix screen, in one chunk of its own: the first bundle carries only autofix-slot.tsx (autofix-part.ts loads this).
import './autofix.css';

export { AutofixConversation, AutofixStrip } from './autofix-conversation';
export { FixesRoute, FixesScreen } from './fixes-route';
export { FixesRailLink } from './fixes-rail-link';
export { AutofixSettingsRoute } from './autofix-settings';
export { AutofixRequestDetail } from './autofix-request';
