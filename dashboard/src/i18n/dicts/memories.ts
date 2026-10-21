// 命名空间 memories：记忆页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Memories',
  failedToLoad: 'Failed to load memories',
  deletedToast: 'Memory deleted',
  deleteFailedToast: 'Failed to delete memory',
  content: 'Content',
  user: 'User',
  agent: 'Agent',
  created: 'Created',
  id: 'ID',
  filterPlaceholder: 'Filter by User ID (optional)',
  upgradeMessage: '1,000+ memories stored. Categories can help organize them.',
  exploreCloud: 'Explore Cloud',
  emptyTitle: 'No memories yet',
  emptyDescription:
    'Create your first memory by sending a POST /memories request.',
  restApiReference: 'REST API reference',
  range: '{start}–{end} of {total}',
  previous: 'Previous',
  next: 'Next',
  detailTitle: 'Memory Detail',
  detailAria: 'View memory content and metadata',
  deleteMemory: 'Delete memory',
  deleteDescription:
    'This memory will be permanently removed. This cannot be undone.',
};

const zh: Shape<typeof en> = {
  title: '记忆',
  failedToLoad: '加载记忆失败',
  deletedToast: '记忆已删除',
  deleteFailedToast: '删除记忆失败',
  content: '内容',
  user: '用户',
  agent: '代理',
  created: '创建时间',
  id: 'ID',
  filterPlaceholder: '按用户 ID 筛选（可选）',
  upgradeMessage: '已存储超过 1,000 条记忆。分类功能可以帮助整理它们。',
  exploreCloud: '了解 Cloud',
  emptyTitle: '暂无记忆',
  emptyDescription: '发送 POST /memories 请求即可创建第一条记忆。',
  restApiReference: 'REST API 参考',
  range: '{start}–{end}，共 {total} 条',
  previous: '上一页',
  next: '下一页',
  detailTitle: '记忆详情',
  detailAria: '查看记忆内容与元数据',
  deleteMemory: '删除记忆',
  deleteDescription: '该记忆将被永久删除，且无法恢复。',
};

const ja: Shape<typeof en> = {
  title: 'メモリ',
  failedToLoad: 'メモリの読み込みに失敗しました',
  deletedToast: 'メモリを削除しました',
  deleteFailedToast: 'メモリの削除に失敗しました',
  content: '内容',
  user: 'ユーザー',
  agent: 'エージェント',
  created: '作成日時',
  id: 'ID',
  filterPlaceholder: 'ユーザー ID でフィルタ（任意）',
  upgradeMessage:
    '1,000 件を超えるメモリが保存されています。カテゴリ機能で整理できます。',
  exploreCloud: 'Cloud を探索',
  emptyTitle: 'メモリはまだありません',
  emptyDescription:
    'POST /memories リクエストを送信して、最初のメモリを作成してください。',
  restApiReference: 'REST API リファレンス',
  range: '{start}–{end} / 全 {total} 件',
  previous: '前へ',
  next: '次へ',
  detailTitle: 'メモリの詳細',
  detailAria: 'メモリの内容とメタデータを表示します',
  deleteMemory: 'メモリを削除',
  deleteDescription: 'このメモリは完全に削除され、元に戻せません。',
};

const ko: Shape<typeof en> = {
  title: '메모리',
  failedToLoad: '메모리 로드 실패',
  deletedToast: '메모리가 삭제되었습니다',
  deleteFailedToast: '메모리 삭제 실패',
  content: '내용',
  user: '사용자',
  agent: '에이전트',
  created: '생성일',
  id: 'ID',
  filterPlaceholder: '사용자 ID로 필터링(선택)',
  upgradeMessage:
    '1,000개 이상의 메모리가 저장되어 있습니다. 카테고리 기능으로 정리할 수 있습니다.',
  exploreCloud: 'Cloud 살펴보기',
  emptyTitle: '아직 메모리가 없습니다',
  emptyDescription:
    'POST /memories 요청을 보내 첫 번째 메모리를 만들어 보세요.',
  restApiReference: 'REST API 참조',
  range: '{start}–{end} / 총 {total}개',
  previous: '이전',
  next: '다음',
  detailTitle: '메모리 상세',
  detailAria: '메모리 내용과 메타데이터 보기',
  deleteMemory: '메모리 삭제',
  deleteDescription: '이 메모리는 영구적으로 삭제되며 되돌릴 수 없습니다.',
};

export const memories = { en, zh, ja, ko } as const;
