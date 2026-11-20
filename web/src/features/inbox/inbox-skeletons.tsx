import { SkeletonDot, SkeletonLine } from '../../ui/skeleton';
import './inbox-skeletons.css';

// The list and the timeline while they load, drawn with the classes of the real rows and messages,
// so nothing moves when the conversations arrive. No buttons or boxes: there is nothing to press yet.
const rowWidths = [['42%', '70%'], ['30%', '55%'], ['50%', '80%'], ['36%', '62%'], ['46%', '74%'], ['28%', '58%'], ['40%', '66%'], ['34%', '72%']];

// grouped matches the list's own: only a list split under Today and Earlier starts with a day label.
export function ListSkeleton({ grouped }: { grouped: boolean }) {
  return <>
    {grouped && <div className="group-label"><SkeletonLine width="4rem" /></div>}
    {rowWidths.map(([customer, subject], index) => <div className="conversation-item" key={index}>
      <div className="conversation-row">
        <span className="row-top"><SkeletonDot size={8} /><span className="row-customer"><SkeletonLine width={customer} /></span><span className="row-due"><SkeletonLine width="2.5rem" /></span></span>
        <span className="row-subject"><SkeletonLine width={subject} /></span>
        <span className="row-excerpt"><SkeletonLine width="90%" /></span>
      </div>
    </div>)}
  </>;
}

const messageLines = [['96%', '88%', '52%'], ['92%', '40%'], ['85%', '94%', '70%', '30%']];

export function TimelineSkeleton() {
  return <div className="timeline-stack">
    {messageLines.map((lines, index) => <div className="message message-incoming" key={index}>
      <div className="message-meta"><SkeletonDot size={24} /><SkeletonLine width="9rem" /><SkeletonLine width="3rem" /></div>
      {lines.map((width, line) => <SkeletonLine width={width} key={line} />)}
    </div>)}
  </div>;
}
