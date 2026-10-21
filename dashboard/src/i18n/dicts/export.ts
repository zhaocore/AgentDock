// 命名空间 export：导出页词条。en 为键结构真源。export 是保留字，导出名用 exportDict。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Export',
  subtitle: 'Export your memories in JSON, CSV, or Pydantic schema format.',
  format: 'Format',
  scope: 'Scope (optional)',
  emptyScopeHint: 'Leave all scopes empty to export every memory (admin only).',
  sampleOutput: 'Sample output',
  exportMemories: 'Export Memories',
  exporting: 'Exporting...',
  toastDownloaded: 'Export downloaded',
  failedToExport: 'Failed to export memories',
};

const zh: Shape<typeof en> = {
  title: '导出',
  subtitle: '以 JSON、CSV 或 Pydantic schema 格式导出记忆。',
  format: '格式',
  scope: '范围（可选）',
  emptyScopeHint: '留空所有范围即可导出全部记忆（仅限管理员）。',
  sampleOutput: '示例输出',
  exportMemories: '导出记忆',
  exporting: '导出中...',
  toastDownloaded: '导出已下载',
  failedToExport: '导出记忆失败',
};

const ja: Shape<typeof en> = {
  title: 'エクスポート',
  subtitle:
    'メモリを JSON、CSV、または Pydantic schema 形式でエクスポートします。',
  format: '形式',
  scope: 'スコープ（任意）',
  emptyScopeHint:
    'すべてのスコープを空にすると、すべてのメモリがエクスポートされます（管理者のみ）。',
  sampleOutput: 'サンプル出力',
  exportMemories: 'メモリをエクスポート',
  exporting: 'エクスポート中...',
  toastDownloaded: 'エクスポートしました',
  failedToExport: 'メモリのエクスポートに失敗しました',
};

const ko: Shape<typeof en> = {
  title: '내보내기',
  subtitle: '메모리를 JSON, CSV 또는 Pydantic schema 형식으로 내보냅니다.',
  format: '형식',
  scope: '범위(선택)',
  emptyScopeHint: '모든 범위를 비우면 모든 메모리를 내보냅니다(관리자만).',
  sampleOutput: '샘플 출력',
  exportMemories: '메모리 내보내기',
  exporting: '내보내는 중...',
  toastDownloaded: '내보내기가 완료되었습니다',
  failedToExport: '메모리 내보내기 실패',
};

export const exportDict = { en, zh, ja, ko } as const;
