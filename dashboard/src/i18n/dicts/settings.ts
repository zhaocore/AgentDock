// 命名空间 settings：设置页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Settings',
  profile: 'Profile',
  saveProfile: 'Save profile',
  profileUpdatedToast: 'Profile updated',
  updateProfileFailedToast: 'Failed to update profile',
  currentPassword: 'Current password',
  newPassword: 'New password',
  confirmNewPassword: 'Confirm new password',
  minPasswordLength: 'Min 8 characters',
  updatePassword: 'Update password',
  passwordUpdatedToast: 'Password updated',
  updatePasswordFailedToast: 'Failed to update password',
  passwordsDontMatch: "Passwords don't match",
  appearance: 'Appearance',
  theme: 'Theme',
};

const zh: Shape<typeof en> = {
  title: '设置',
  profile: '个人资料',
  saveProfile: '保存资料',
  profileUpdatedToast: '资料已更新',
  updateProfileFailedToast: '更新资料失败',
  currentPassword: '当前密码',
  newPassword: '新密码',
  confirmNewPassword: '确认新密码',
  minPasswordLength: '最少 8 个字符',
  updatePassword: '更新密码',
  passwordUpdatedToast: '密码已更新',
  updatePasswordFailedToast: '更新密码失败',
  passwordsDontMatch: '两次输入的密码不一致',
  appearance: '外观',
  theme: '主题',
};

const ja: Shape<typeof en> = {
  title: '設定',
  profile: 'プロフィール',
  saveProfile: 'プロフィールを保存',
  profileUpdatedToast: 'プロフィールを更新しました',
  updateProfileFailedToast: 'プロフィールの更新に失敗しました',
  currentPassword: '現在のパスワード',
  newPassword: '新しいパスワード',
  confirmNewPassword: '新しいパスワードの確認',
  minPasswordLength: '8文字以上',
  updatePassword: 'パスワードを更新',
  passwordUpdatedToast: 'パスワードを更新しました',
  updatePasswordFailedToast: 'パスワードの更新に失敗しました',
  passwordsDontMatch: 'パスワードが一致しません',
  appearance: '外観',
  theme: 'テーマ',
};

const ko: Shape<typeof en> = {
  title: '설정',
  profile: '프로필',
  saveProfile: '프로필 저장',
  profileUpdatedToast: '프로필이 업데이트되었습니다',
  updateProfileFailedToast: '프로필 업데이트에 실패했습니다',
  currentPassword: '현재 비밀번호',
  newPassword: '새 비밀번호',
  confirmNewPassword: '새 비밀번호 확인',
  minPasswordLength: '최소 8자',
  updatePassword: '비밀번호 업데이트',
  passwordUpdatedToast: '비밀번호가 업데이트되었습니다',
  updatePasswordFailedToast: '비밀번호 업데이트에 실패했습니다',
  passwordsDontMatch: '비밀번호가 일치하지 않습니다',
  appearance: '모양',
  theme: '테마',
};

export const settings = { en, zh, ja, ko } as const;
