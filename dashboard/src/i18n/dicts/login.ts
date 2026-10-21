// 命名空间 login：登录页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  signInTo: 'Sign in to MemGo',
  signIn: 'Sign in',
  signingIn: 'Signing in...',
  email: 'Email',
  password: 'Password',
  forgotPassword: 'Forgot password?',
  resetTitle: 'Reset your admin password',
  resetDescription:
    'Run this command on the server host. It overwrites the existing password; anyone already signed in stays signed in until their session expires.',
  invalidEmail: 'Enter a valid email address.',
  loginFailed: 'Login failed',
  testimonial:
    '"MemGo allowed us to unlock true personalized tutoring for every student, and it took us just a weekend to integrate."',
  testimonialAuthor: 'Michael Tong',
  testimonialRole: 'CTO, RevisionDojo',
  trustedBy: 'Trusted by 100k+ Developers',
};

const zh: Shape<typeof en> = {
  signInTo: '登录 MemGo',
  signIn: '登录',
  signingIn: '登录中...',
  email: '邮箱',
  password: '密码',
  forgotPassword: '忘记密码？',
  resetTitle: '重置管理员密码',
  resetDescription:
    '在服务器主机上执行此命令。它会覆盖现有密码；已登录用户在会话过期前仍保持登录状态。',
  invalidEmail: '请输入有效的邮箱地址。',
  loginFailed: '登录失败',
  testimonial:
    '“MemGo 让我们只用一个周末就完成了集成，为每位学生解锁了真正的个性化辅导。”',
  testimonialAuthor: 'Michael Tong',
  testimonialRole: 'CTO, RevisionDojo',
  trustedBy: '深受 100,000+ 开发者信赖',
};

const ja: Shape<typeof en> = {
  signInTo: 'MemGo にサインイン',
  signIn: 'サインイン',
  signingIn: 'サインイン中...',
  email: 'メールアドレス',
  password: 'パスワード',
  forgotPassword: 'パスワードをお忘れですか？',
  resetTitle: '管理者パスワードのリセット',
  resetDescription:
    'サーバーホスト上でこのコマンドを実行してください。既存のパスワードが上書きされます。すでにサインインしているユーザーは、セッションが期限切れになるまでサインイン状態を維持します。',
  invalidEmail: '有効なメールアドレスを入力してください。',
  loginFailed: 'ログインに失敗しました',
  testimonial:
    '「MemGo を導入したのはわずか一週間末。すべての生徒に本当のパーソナライズされた学習を提供できるようになりました。」',
  testimonialAuthor: 'Michael Tong',
  testimonialRole: 'CTO, RevisionDojo',
  trustedBy: '100,000 人以上の開発者から信頼',
};

const ko: Shape<typeof en> = {
  signInTo: 'MemGo 로그인',
  signIn: '로그인',
  signingIn: '로그인 중...',
  email: '이메일',
  password: '비밀번호',
  forgotPassword: '비밀번호를 잊으셨나요?',
  resetTitle: '관리자 비밀번호 재설정',
  resetDescription:
    '서버 호스트에서 이 명령어를 실행하세요. 기존 비밀번호를 덮어씁니다. 이미 로그인한 사용자는 세션이 만료될 때까지 로그인 상태를 유지합니다.',
  invalidEmail: '유효한 이메일 주소를 입력하세요.',
  loginFailed: '로그인 실패',
  testimonial:
    '“MemGo 덕분에 단 한 주말 만에 통합을 완료하고, 모든 학생에게 진정한 개인 맞춤 학습을 제공할 수 있게 되었습니다.”',
  testimonialAuthor: 'Michael Tong',
  testimonialRole: 'CTO, RevisionDojo',
  trustedBy: '100,000명 이상의 개발자가 신뢰',
};

export const login = { en, zh, ja, ko } as const;
