import type { MenuItem } from '../kb-ui';
import { kbMessages } from '../kb-messages';
import { calloutTypes } from '../callouts';
import type { IconName } from './icons';
import type { Active, Block, LineKind, ToolTarget } from './editor-types';

// What the toolbar's two menus hold, for the editor that shows the text. compact: a phone's toolbar, or a column
// too narrow for every tool, where More also holds the headings, lists, table, search and undo.
export interface MenuContext {
  compact: boolean;
  editing: boolean;
  full: boolean;
  active: Active | undefined;
  search: boolean;
  act(run: (target: ToolTarget) => unknown): void;
  openCoding(): void;
  openLinking(): void;
  openPicking(): void;
  openDevice(): void;
  openVideo(): void;
  toggleFull(): void;
}

export function moreMenu(c: MenuContext): MenuItem[] {
  const t = kbMessages.tools;
  const item = (label: string, icon: IconName, run: (target: ToolTarget) => unknown, disabled = false): MenuItem => ({ label, icon, disabled: disabled || !c.editing, onSelect: () => c.act(run) });
  const heading = (block: Block, label: string, icon: IconName): MenuItem => ({ label, icon, disabled: !c.editing || c.active?.block === block, onSelect: () => c.act((target) => target.block(block)) });
  const line = (kind: LineKind) => (target: ToolTarget) => target.lines(kind);
  return [
    ...(c.compact ? [heading('paragraph', t.paragraph, 'text'), heading('h2', t.h2, 'h2'), heading('h3', t.h3, 'h3'),
      { ...item(t.ordered, 'ordered', line('ordered')), separated: true }, item(t.bullet, 'bullet', line('bullet')), item(t.quote, 'quote', line('quote')),
      item(t.table, 'table', (target) => target.table())] : []),
    { ...item(t.strike, 'strike', (target) => target.mark('strike')), separated: c.compact }, item(t.code, 'code', (target) => target.mark('code')), item(t.task, 'task', line('task')),
    { label: t.codeBlock, icon: 'codeBlock', disabled: !c.editing, onSelect: c.openCoding }, item(t.hr, 'hr', (target) => target.rule()),
    ...(c.compact ? [{ label: t.articleLink, icon: 'article' as const, disabled: !c.editing, onSelect: c.openLinking, separated: true }] : []),
    ...(c.compact && c.search ? [item(t.search, 'search', (target) => target.search?.())] : []),
    ...(c.compact ? [item(t.undo, 'undo', (target) => target.undo(), !c.active?.canUndo)] : []),
    item(t.redo, 'redo', (target) => target.redo(), !c.active?.canRedo),
    ...(c.compact ? [{ label: c.full ? t.exitFullscreen : t.fullscreen, icon: 'fullscreen' as const, onSelect: c.toggleFull, separated: true }] : []),
  ];
}

export function insertMenu(c: MenuContext): MenuItem[] {
  const t = kbMessages.tools;
  return [
    { label: t.fromLibrary, hint: t.fromLibraryHint, icon: 'library', disabled: !c.editing, onSelect: c.openPicking },
    { label: t.fromDevice, hint: t.fromDeviceHint, icon: 'upload', disabled: !c.editing, onSelect: c.openDevice },
    { label: t.videoLink, hint: t.videoLinkHint, icon: 'video', disabled: !c.editing, onSelect: c.openVideo },
    ...calloutTypes.map((type, at) => ({ label: t.callouts[type], icon: 'info' as const, disabled: !c.editing, onSelect: () => c.act((target) => target.callout(type)), separated: at === 0 })),
  ];
}
