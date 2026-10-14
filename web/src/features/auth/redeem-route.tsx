import { useMutation } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { api } from '../../api/client';
import { RedeemPage } from './redeem-page';

export function RedeemRoute({ token }: { token: string }) {
  const navigate = useNavigate();
  const redeem = useMutation({ mutationFn: () => api.redeem(token, navigator.userAgent), onSuccess: () => navigate({ to: '/' }) });
  return <RedeemPage token={token} pending={redeem.isPending} failed={redeem.isError} onRedeem={() => redeem.mutate()} />;
}
