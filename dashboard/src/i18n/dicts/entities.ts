// 命名空间 entities：实体页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Entities',
  failedToLoad: 'Failed to load entities',
  deletedToast: 'Entity deleted',
  deleteFailedToast: 'Failed to delete entity',
  type: 'Type',
  id: 'ID',
  memories: 'Memories',
  lastActive: 'Last Active',
  emptyTitle: 'No entities yet',
  emptyDescription:
    'Entities appear once memories are stored with a user_id, agent_id, or run_id.',
  deleteTitle: 'Delete entity',
  deleteDescription:
    'All memories associated with this entity will be permanently removed. This cannot be undone.',
};

const zh: Shape<typeof en> = {
  title: '实体',
  failedToLoad: '加载实体失败',
  deletedToast: '实体已删除',
  deleteFailedToast: '删除实体失败',
  type: '类型',
  id: 'ID',
  memories: '记忆数',
  lastActive: '最近活跃',
  emptyTitle: '暂无实体',
  emptyDescription:
    '当记忆以 user_id、agent_id 或 run_id 存储后，实体会显示在这里。',
  deleteTitle: '删除实体',
  deleteDescription: '与此实体关联的所有记忆将被永久删除，且无法恢复。',
};

const ja: Shape<typeof en> = {
  title: 'エンティティ',
  failedToLoad: 'エンティティの読み込みに失敗しました',
  deletedToast: 'エンティティを削除しました',
  deleteFailedToast: 'エンティティの削除に失敗しました',
  type: 'タイプ',
  id: 'ID',
  memories: 'メモリ数',
  lastActive: '最終アクティブ',
  emptyTitle: 'エンティティはまだありません',
  emptyDescription:
    'user_id、agent_id、run_id と共に保存されたメモリがあると、エンティティが表示されます。',
  deleteTitle: 'エンティティを削除',
  deleteDescription:
    'このエンティティに関連付けられたすべてのメモリが完全に削除されます。この操作は元に戻せません。',
};

const ko: Shape<typeof en> = {
  title: '엔티티',
  failedToLoad: '엔티티 로드 실패',
  deletedToast: '엔티티가 삭제되었습니다',
  deleteFailedToast: '엔티티 삭제 실패',
  type: '유형',
  id: 'ID',
  memories: '메모리 수',
  lastActive: '최근 활동',
  emptyTitle: '아직 엔티티가 없습니다',
  emptyDescription:
    'user_id, agent_id, run_id와 함께 메모리가 저장되면 엔티티가 표시됩니다.',
  deleteTitle: '엔티티 삭제',
  deleteDescription:
    '이 엔티티와 연결된 모든 메모리가 영구적으로 삭제됩니다. 되돌릴 수 없습니다.',
};

export const entities = { en, zh, ja, ko } as const;
