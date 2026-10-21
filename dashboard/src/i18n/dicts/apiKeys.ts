// 命名空间 apiKeys：API 密钥页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'API Keys',
  failedToLoad: 'Failed to load API keys',
  createFailedToast: 'Failed to create key',
  revokedToast: 'API key revoked',
  revokeFailedToast: 'Failed to revoke key',
  label: 'Label',
  key: 'Key',
  created: 'Created',
  lastUsed: 'Last Used',
  createKey: 'Create Key',
  createTitle: 'Create API Key',
  labelPlaceholder: 'e.g. Production',
  create: 'Create',
  yourApiKey: 'Your API Key',
  saveKeyWarning: "Save this key -- you won't see it again.",
  done: 'Done',
  cloudMessage: 'Managing multiple apps? Cloud offers project-based isolation.',
  exploreCloud: 'Explore Cloud',
  emptyTitle: 'No API keys yet',
  emptyDescription: 'Create your first API key to start using the MemGo API.',
  revokeTitle: 'Revoke API key',
  revokeDescription:
    'Applications using this key will immediately stop working. This cannot be undone.',
  revoke: 'Revoke',
};

const zh: Shape<typeof en> = {
  title: 'API 密钥',
  failedToLoad: '加载 API 密钥失败',
  createFailedToast: '创建密钥失败',
  revokedToast: 'API 密钥已撤销',
  revokeFailedToast: '撤销密钥失败',
  label: '标签',
  key: '密钥',
  created: '创建时间',
  lastUsed: '最近使用',
  createKey: '创建密钥',
  createTitle: '创建 API 密钥',
  labelPlaceholder: '例如 Production',
  create: '创建',
  yourApiKey: '你的 API 密钥',
  saveKeyWarning: '请保存此密钥——之后不会再显示。',
  done: '完成',
  cloudMessage: '需要管理多个应用？Cloud 提供基于项目的隔离。',
  exploreCloud: '了解 Cloud',
  emptyTitle: '暂无 API 密钥',
  emptyDescription: '创建你的第一个 API 密钥以开始使用 MemGo API。',
  revokeTitle: '撤销 API 密钥',
  revokeDescription: '使用此密钥的应用将立即停止工作，且此操作无法撤销。',
  revoke: '撤销',
};

const ja: Shape<typeof en> = {
  title: 'API キー',
  failedToLoad: 'API キーの読み込みに失敗しました',
  createFailedToast: 'キーの作成に失敗しました',
  revokedToast: 'API キーを失効させました',
  revokeFailedToast: 'キーの失効に失敗しました',
  label: 'ラベル',
  key: 'キー',
  created: '作成日時',
  lastUsed: '最終使用',
  createKey: 'キーを作成',
  createTitle: 'API キーを作成',
  labelPlaceholder: '例：Production',
  create: '作成',
  yourApiKey: 'あなたの API キー',
  saveKeyWarning:
    'このキーを保存してください。再度表示されることはありません。',
  done: '完了',
  cloudMessage:
    '複数のアプリを管理していますか？Cloud ではプロジェクト単位の分離を提供しています。',
  exploreCloud: 'Cloud を詳しく見る',
  emptyTitle: 'API キーはまだありません',
  emptyDescription:
    '最初の API キーを作成して MemGo API の利用を開始してください。',
  revokeTitle: 'API キーを失効',
  revokeDescription:
    'このキーを使用しているアプリはすぐに動作しなくなります。この操作は元に戻せません。',
  revoke: '失効',
};

const ko: Shape<typeof en> = {
  title: 'API 키',
  failedToLoad: 'API 키를 불러오지 못했습니다',
  createFailedToast: '키 생성 실패',
  revokedToast: 'API 키가 해지되었습니다',
  revokeFailedToast: '키 해지 실패',
  label: '라벨',
  key: '키',
  created: '생성일',
  lastUsed: '마지막 사용',
  createKey: '키 생성',
  createTitle: 'API 키 생성',
  labelPlaceholder: '예: Production',
  create: '생성',
  yourApiKey: '내 API 키',
  saveKeyWarning: '이 키를 저장하세요. 다시 볼 수 없습니다.',
  done: '완료',
  cloudMessage:
    '여러 앱을 관리하고 있나요? Cloud는 프로젝트 기반 격리를 제공합니다.',
  exploreCloud: 'Cloud 살펴보기',
  emptyTitle: '아직 API 키가 없습니다',
  emptyDescription: '첫 번째 API 키를 만들어 MemGo API 사용을 시작하세요.',
  revokeTitle: 'API 키 해지',
  revokeDescription:
    '이 키를 사용하는 앱은 즉시 작동이 중지됩니다. 되돌릴 수 없습니다.',
  revoke: '해지',
};

export const apiKeys = { en, zh, ja, ko } as const;
