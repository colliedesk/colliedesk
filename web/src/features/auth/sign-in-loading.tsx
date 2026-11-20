import { messages } from '../../messages';
import { LoadingStatus } from '../../ui/loading-status';
import { SkeletonLine } from '../../ui/skeleton';
import './sign-in-loading.css';

// Clerk's sign-in form in grey, at the size Clerk draws it (one social button, the "or" line, the email
// field and Continue, then the footer), so the panel does not grow when the real form arrives.
function SignInFormSkeleton() {
  return <div className="signin-skeleton">
    <div className="signin-skeleton-card">
      <i className="signin-skeleton-control" />
      <div className="signin-skeleton-divider"><SkeletonLine width="1.5rem" /></div>
      <SkeletonLine width="6rem" />
      <i className="signin-skeleton-control" />
      <i className="signin-skeleton-control" />
    </div>
    <div className="signin-skeleton-footer"><SkeletonLine width="55%" /></div>
  </div>;
}

// SignInLoading stands where the sign-in form will be, from the moment its code is asked for until
// Clerk has drawn the form: the words are for screen readers, and say so out loud when the wait is slow.
export function SignInLoading() {
  return <LoadingStatus text={messages.loadingCloudSignIn} shape={<SignInFormSkeleton />} />;
}
