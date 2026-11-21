import type { MenuItem } from '../kb-ui';
import { kbMessages } from '../kb-messages';
import { rules } from './format-tags';
import type { Align, Palette } from './format-tags';
import type { IconName } from './icons';
import type { TableAction, ToolTarget } from './editor-types';
import type { MenuContext } from './toolbar-menus';

// The toolbar's menus for the formatting the server keeps beyond Markdown: text colour, alignment and indent, and
// the table tools. Each colour shows as a swatch in the app's own tokens (formatting.css has the same values).

const palette: readonly Palette[] = rules.colors;
const textSwatch: Record<string, string> = { accent: 'var(--accent-text)', success: 'var(--ok)', warning: 'var(--warn)', danger: 'var(--danger)', muted: 'var(--text-3)' };
const cellSwatch: Record<string, string> = { accent: 'color-mix(in srgb, var(--accent) 14%, transparent)', success: 'var(--ok-soft)', warning: 'var(--warn-soft)', danger: 'var(--danger-soft)', muted: 'var(--surface-2)' };

export function colorMenu(c: MenuContext): MenuItem[] {
  const t = kbMessages.tools;
  const current = c.active?.color ?? '';
  const pick = (color: Palette | null): MenuItem => ({
    label: color ? t.palette[color] ?? color : t.defaultColor, swatch: color ? textSwatch[color] : 'var(--text-1)', separated: palette[0] === color,
    disabled: !c.editing || current === (color ?? ''), onSelect: () => c.act((target) => target.color(color)),
  });
  return [pick(null), ...palette.map(pick)];
}

export function alignMenu(c: MenuContext): MenuItem[] {
  const t = kbMessages.tools;
  const off = !c.editing || c.active?.formatAt === 'none';
  const align = (value: Align | null, label: string, icon: IconName): MenuItem => ({
    label, icon, disabled: off || (c.active?.align ?? 'left') === (value ?? 'left'), onSelect: () => c.act((target) => target.align(value)),
  });
  const indent = c.active?.indent ?? 0;
  const block = c.active?.formatAt === 'block';
  return [
    align(null, t.alignLeft, 'alignLeft'), align('center', t.alignCenter, 'alignCenter'), align('right', t.alignRight, 'alignRight'),
    { label: t.indent, icon: 'indent', separated: true, disabled: off || !block || indent >= rules.indent, onSelect: () => c.act((target) => target.indent(1)) },
    { label: t.outdent, icon: 'outdent', disabled: off || !block || indent === 0, onSelect: () => c.act((target) => target.indent(-1)) },
  ];
}

export function tableMenu(c: MenuContext): MenuItem[] {
  const t = kbMessages.tools;
  const edit = (label: string, action: TableAction, more: Partial<MenuItem> = {}): MenuItem => ({
    label, disabled: !c.editing, onSelect: () => c.act((target: ToolTarget) => target.tableEdit?.(action)), ...more,
  });
  return [
    edit(c.active?.headerRow ? t.headerRowOff : t.headerRowOn, 'headerRow'),
    edit(t.rowBefore, 'rowBefore', { separated: true }), edit(t.rowAfter, 'rowAfter'),
    edit(t.columnBefore, 'columnBefore'), edit(t.columnAfter, 'columnAfter'),
    edit(t.mergeCells, 'merge', { icon: 'merge', separated: true, disabled: !c.editing || !c.active?.canMerge }),
    edit(t.splitCell, 'split', { icon: 'split', disabled: !c.editing || !c.active?.canSplit }),
    edit(t.noBackground, 'bg-none', { swatch: 'var(--surface-0)', separated: true }),
    ...palette.map((color) => edit(t.background(t.palette[color] ?? color), `bg-${color}`, { swatch: cellSwatch[color] })),
    edit(t.deleteRow, 'deleteRow', { danger: true, separated: true }), edit(t.deleteColumn, 'deleteColumn', { danger: true }),
    edit(t.deleteTable, 'deleteTable', { icon: 'trash', danger: true }),
  ];
}
