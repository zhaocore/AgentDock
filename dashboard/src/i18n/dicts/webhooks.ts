// 命名空间 webhooks：Webhook 页词条。en 为键结构真源，
// zh/ja/ko 用 Shape<typeof en> 声明，缺键/多键在编译期报错。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Webhooks',
  failedToLoad: 'Failed to load webhooks',
  description:
    'Real-time event notifications for memory operations. Registered endpoints receive an HTTP POST for each subscribed event.',
  create: 'Create Webhook',
  endpointUrlLabel: 'Endpoint URL',
  namePlaceholder: 'Memory Logger',
  eventsLabel: 'Events',
  registeredTitle: 'Registered Webhooks',
  loading: 'Loading webhooks...',
  empty: 'No webhooks registered — event notifications are off.',
  unnamed: 'Unnamed webhook',
  editAria: 'Edit webhook {name}',
  deleteAria: 'Delete webhook {name}',
  endpointRequired: 'Endpoint URL is required',
  createdToast: 'Webhook created',
  createFailedToast: 'Failed to create webhook',
  updatedToast: 'Webhook updated',
  updateFailedToast: 'Failed to update webhook',
  deletedToast: 'Webhook deleted',
  deleteFailedToast: 'Failed to delete webhook',
  adminRequired: 'Admin role required to manage webhooks.',
  eventInfo:
    'Memory events carry the memory ID, content and event type (ADD / UPDATE / DELETE). Categorization events carry the memory ID and the assigned category.',
  deliveryInfo:
    'Deliveries are best-effort: a failing endpoint is logged and skipped, it never blocks memory operations.',
};

const zh: Shape<typeof en> = {
  title: 'Webhook',
  failedToLoad: '加载 Webhook 失败',
  description:
    '记忆操作的实时事件通知。已注册的端点会对每个订阅事件收到一个 HTTP POST。',
  create: '创建 Webhook',
  endpointUrlLabel: '端点 URL',
  namePlaceholder: '记忆日志',
  eventsLabel: '事件',
  registeredTitle: '已注册的 Webhook',
  loading: '正在加载 Webhook...',
  empty: '尚未注册 Webhook —— 事件通知已关闭。',
  unnamed: '未命名的 Webhook',
  editAria: '编辑 Webhook {name}',
  deleteAria: '删除 Webhook {name}',
  endpointRequired: '端点 URL 为必填项',
  createdToast: 'Webhook 已创建',
  createFailedToast: '创建 Webhook 失败',
  updatedToast: 'Webhook 已更新',
  updateFailedToast: '更新 Webhook 失败',
  deletedToast: 'Webhook 已删除',
  deleteFailedToast: '删除 Webhook 失败',
  adminRequired: '管理 Webhook 需要管理员角色',
  eventInfo:
    '记忆事件携带记忆 ID、内容与事件类型（ADD / UPDATE / DELETE）。分类事件携带记忆 ID 与分配的分类。',
  deliveryInfo:
    '投递采用尽力而为：失败的端点会被记录并跳过，绝不会阻塞记忆操作。',
};

const ja: Shape<typeof en> = {
  title: 'Webhook',
  failedToLoad: 'Webhook の読み込みに失敗しました',
  description:
    'メモリ操作のリアルタイムイベント通知です。登録したエンドポイントは、購読中の各イベントに対して HTTP POST を受け取ります。',
  create: 'Webhook を作成',
  endpointUrlLabel: 'エンドポイント URL',
  namePlaceholder: 'メモリロガー',
  eventsLabel: 'イベント',
  registeredTitle: '登録済みの Webhook',
  loading: 'Webhook を読み込み中...',
  empty: 'Webhook が登録されていません — イベント通知はオフです。',
  unnamed: '名前のない Webhook',
  editAria: 'Webhook {name} を編集',
  deleteAria: 'Webhook {name} を削除',
  endpointRequired: 'エンドポイント URL は必須です',
  createdToast: 'Webhook を作成しました',
  createFailedToast: 'Webhook の作成に失敗しました',
  updatedToast: 'Webhook を更新しました',
  updateFailedToast: 'Webhook の更新に失敗しました',
  deletedToast: 'Webhook を削除しました',
  deleteFailedToast: 'Webhook の削除に失敗しました',
  adminRequired: 'Webhook の管理には管理者ロールが必要です',
  eventInfo:
    'メモリイベントには、メモリ ID、コンテンツ、イベントタイプ（ADD / UPDATE / DELETE）が含まれます。分類イベントには、メモリ ID と割り当てられたカテゴリが含まれます。',
  deliveryInfo:
    '配信はベストエフォートです。失敗したエンドポイントは記録してスキップされ、メモリ操作をブロックすることはありません。',
};

const ko: Shape<typeof en> = {
  title: 'Webhook',
  failedToLoad: 'Webhook 로드 실패',
  description:
    '메모리 작업에 대한 실시간 이벤트 알림입니다. 등록된 엔드포인트는 구독한 각 이벤트에 대해 HTTP POST를 받습니다.',
  create: 'Webhook 생성',
  endpointUrlLabel: '엔드포인트 URL',
  namePlaceholder: '메모리 로거',
  eventsLabel: '이벤트',
  registeredTitle: '등록된 Webhook',
  loading: 'Webhook 불러오는 중...',
  empty: '등록된 Webhook이 없습니다 — 이벤트 알림이 꺼져 있습니다.',
  unnamed: '이름 없는 Webhook',
  editAria: 'Webhook {name} 편집',
  deleteAria: 'Webhook {name} 삭제',
  endpointRequired: '엔드포인트 URL은 필수입니다',
  createdToast: 'Webhook이 생성되었습니다',
  createFailedToast: 'Webhook 생성 실패',
  updatedToast: 'Webhook이 업데이트되었습니다',
  updateFailedToast: 'Webhook 업데이트 실패',
  deletedToast: 'Webhook이 삭제되었습니다',
  deleteFailedToast: 'Webhook 삭제 실패',
  adminRequired: 'Webhook 관리를 위해서는 관리자 역할이 필요합니다',
  eventInfo:
    '메모리 이벤트에는 메모리 ID, 콘텐츠, 이벤트 유형(ADD / UPDATE / DELETE)이 포함됩니다. 분류 이벤트에는 메모리 ID와 할당된 카테고리가 포함됩니다.',
  deliveryInfo:
    '전달은 best-effort 방식입니다. 실패한 엔드포인트는 기록 후 건너뛰며 메모리 작업을 차단하지 않습니다.',
};

export const webhooks = { en, zh, ja, ko } as const;
