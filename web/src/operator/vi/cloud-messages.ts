import type { cloudMessages as en } from '../cloud-messages';
import type { Translation } from '../lang';

// Vietnamese for cloud-messages.ts (the console's cloud sign-in).
export const cloudMessages: Translation<typeof en> = {
  signingOut: 'Đang đăng xuất…',
  signOutFailed: 'Đăng xuất không thành công. Hãy thử lại.',
  retry: 'Thử đăng nhập lại',
  refused: 'Tài khoản này không đăng nhập được vào bảng điều khiển vận hành.',
  refusedHelp: 'Nếu bạn vận hành máy chủ này, một người vận hành thêm bạn trên máy chủ bằng:',
  useAnotherAccount: 'Dùng tài khoản khác',
};
