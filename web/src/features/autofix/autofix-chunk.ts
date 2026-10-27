// Every Autofix screen, in one chunk of its own: the first bundle carries only autofix-slot.tsx (autofix-part.ts loads this).
import './autofix.css';

export { AutofixConversation, AutofixStrip } from './autofix-conversation';
export { FixesRoute } from './fixes-route';
export { AutofixSettingsRoute } from './autofix-settings';
export { AutofixRequestDetail } from './autofix-request';
