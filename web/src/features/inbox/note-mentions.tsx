import './mentions.css';
import type { NoteMention } from '../../api/schemas';
import { formatDate } from '../../lib/format-date';
import { AtIcon } from '../../ui/icons';
import { mentionMessages } from './mention-messages';

const escaped = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// boldMentions writes each @Name a note asks in bold in its Markdown, longest names first so "@Long Bui" wins over
// "@Long"; a name already in bold or in code stays as written.
export function boldMentions(text: string, mentions: NoteMention[] = []): string {
  const names = [...new Set(mentions.map((mention) => mention.name))].sort((a, b) => b.length - a.length);
  if (names.length === 0) return text;
  const pattern = new RegExp(`(^|[^*\`\\w])@(${names.map(escaped).join('|')})(?![\\w*])`, 'gu');
  return text.replace(pattern, (_, before: string, name: string) => `${before}**@${name}**`);
}

interface NoteMentionsLineProps {
  mentions: NoteMention[];
  // Who wrote the note, and who reads it now: the person asked reads who asked them instead.
  writer?: string | null;
  currentAgentId?: string;
  timezone: string;
}

// NoteMentionsLine is the quiet line under a note that asks teammates: each person was told (in Mentions, and by
// email unless they turned it off), then saw it at a time. The person asked reads that the note asked them.
export function NoteMentionsLine({ mentions, writer, currentAgentId, timezone }: NoteMentionsLineProps) {
  if (mentions.length === 0) return null;
  const mine = mentions.some((mention) => mention.agent_id === currentAgentId);
  const lines = mine ? [mentionMessages.askedYou(writer || mentionMessages.someone)]
    : mentions.map((mention) => (mention.read_at ? mentionMessages.saw(mention.name, formatDate(mention.read_at, timezone))
      : mentionMessages.told(mention.name, mention.by_email)));
  return <div className="note-mentions">{lines.map((line) => <p key={line}><AtIcon /><span>{line}</span></p>)}</div>;
}
