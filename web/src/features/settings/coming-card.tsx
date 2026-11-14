import { settingsMessages } from './settings-messages';

// ComingCard says what a reserved place will hold, with where to follow it. The issue is public, and its number
// stays in the address: the words never name it.
export function ComingCard({ title, body, issue }: { title: string; body: string; issue: number }) {
  return <section className="settings-card coming-card" aria-label={title}>
    <div className="settings-body">
      <strong>{title}</strong>
      <p>{body}</p>
      <p><a className="link-action" href={`https://github.com/colliedesk/colliedesk/issues/${issue}`} target="_blank" rel="noreferrer">{settingsMessages.followProgress}</a></p>
    </div>
  </section>;
}
