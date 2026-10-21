// 命名空间 categories：分类页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Custom Categories',
  failedToLoadConfig: 'Failed to load server configuration',
  introDescription:
    'Memories added with infer are tagged with the closest matching category from this catalog. Changing the list only affects new memories — existing memories keep their tag.',
  activeCategories: 'Active Categories',
  loading: 'Loading categories...',
  emptyDescription:
    'No categories configured — memory tagging is off. New memories are stored without a category.',
  descriptionPlaceholder: 'What this category covers (helps the LLM match)',
  removeCategory: 'Remove category {name}',
  unnamed: 'unnamed',
  addCategory: 'Add category',
  emptyListNote:
    'An empty list disables tagging entirely. Tags are assigned by the LLM at ingestion time.',
  perCallPrefix: 'Per-call override: pass',
  perCallOn: 'on',
  perCallSuffix:
    'to fully replace this catalog for that call (lists are not merged).',
  everyCategoryNeedsName: 'Every category needs a name',
  savedToast: 'Categories saved',
  saveFailedToast: 'Failed to save categories',
  adminRoleRequired: 'Admin role required to edit the category catalog.',
  saveCategories: 'Save Categories',
};

const zh: Shape<typeof en> = {
  title: '自定义分类',
  failedToLoadConfig: '加载服务器配置失败',
  introDescription:
    '通过 infer 添加的记忆会匹配此目录中最接近的分类并打标。修改列表只影响新记忆，已有记忆保留其标签。',
  activeCategories: '当前分类',
  loading: '正在加载分类...',
  emptyDescription: '未配置分类，记忆打标已关闭。新记忆将不带分类存储。',
  descriptionPlaceholder: '此分类涵盖的内容（帮助 LLM 匹配）',
  removeCategory: '移除分类 {name}',
  unnamed: '未命名',
  addCategory: '添加分类',
  emptyListNote: '空列表会完全关闭打标。标签由 LLM 在写入时分配。',
  perCallPrefix: '单次调用覆盖：传入',
  perCallOn: '到',
  perCallSuffix: '即可完全替换该调用的目录（列表不会合并）。',
  everyCategoryNeedsName: '每个分类都需要名称',
  savedToast: '分类已保存',
  saveFailedToast: '保存分类失败',
  adminRoleRequired: '需要管理员角色才能编辑分类目录。',
  saveCategories: '保存分类',
};

const ja: Shape<typeof en> = {
  title: 'カスタムカテゴリ',
  failedToLoadConfig: 'サーバー設定の読み込みに失敗しました',
  introDescription:
    'infer で追加されたメモリは、このカタログで最も近いカテゴリにタグ付けされます。リストの変更は新しいメモリにのみ影響し、既存のメモリのタグは維持されます。',
  activeCategories: '現在のカテゴリ',
  loading: 'カテゴリを読み込み中...',
  emptyDescription:
    'カテゴリが設定されていないため、メモリのタグ付けは無効です。新しいメモリはカテゴリなしで保存されます。',
  descriptionPlaceholder:
    'このカテゴリがカバーする内容（LLM のマッチングに役立ちます）',
  removeCategory: 'カテゴリ {name} を削除',
  unnamed: '名前なし',
  addCategory: 'カテゴリを追加',
  emptyListNote:
    '空のリストではタグ付けが完全に無効になります。タグは取り込み時に LLM によって割り当てられます。',
  perCallPrefix: '呼び出しごとの上書き：',
  perCallOn: 'を',
  perCallSuffix:
    'に渡すと、その呼び出しのカタログが完全に置き換えられます（リストはマージされません）。',
  everyCategoryNeedsName: 'すべてのカテゴリに名前が必要です',
  savedToast: 'カテゴリを保存しました',
  saveFailedToast: 'カテゴリの保存に失敗しました',
  adminRoleRequired: 'カテゴリカタログを編集するには管理者ロールが必要です。',
  saveCategories: 'カテゴリを保存',
};

const ko: Shape<typeof en> = {
  title: '사용자 정의 카테고리',
  failedToLoadConfig: '서버 설정을 불러오지 못했습니다',
  introDescription:
    'infer로 추가된 메모리는 이 카탈로그에서 가장 가까운 카테고리로 태그가 지정됩니다. 목록을 변경하면 새 메모리에만 영향을 주며 기존 메모리는 태그가 유지됩니다.',
  activeCategories: '현재 카테고리',
  loading: '카테고리를 불러오는 중...',
  emptyDescription:
    '카테고리가 설정되지 않아 메모리 태그 기능이 꺼져 있습니다. 새 메모리는 카테고리 없이 저장됩니다.',
  descriptionPlaceholder: '이 카테고리가 다루는 내용(LLM 매칭에 도움이 됩니다)',
  removeCategory: '카테고리 {name} 제거',
  unnamed: '이름 없음',
  addCategory: '카테고리 추가',
  emptyListNote:
    '빈 목록은 태그 기능을 완전히 비활성화합니다. 태그는 수집 시 LLM이 할당합니다.',
  perCallPrefix: '호출별 재정의: ',
  perCallOn: '를',
  perCallSuffix:
    '에 전달하면 해당 호출의 카탈로그가 완전히 대체됩니다(목록은 병합되지 않습니다).',
  everyCategoryNeedsName: '모든 카테고리에는 이름이 필요합니다',
  savedToast: '카테고리가 저장되었습니다',
  saveFailedToast: '카테고리 저장 실패',
  adminRoleRequired: '카테고리 카탈로그를 편집하려면 관리자 역할이 필요합니다.',
  saveCategories: '카테고리 저장',
};

export const categories = { en, zh, ja, ko } as const;
