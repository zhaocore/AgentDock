// 命名空间 analytics：分析页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Analytics',
  failedToLoad: 'Failed to load analytics',
  failedToLoadUserStats: 'Failed to load user analytics',
  nonAdminDescription:
    'Track memory operations, latency, and usage patterns over time.',
  adminRequired: 'Admin role required to view analytics.',
  description:
    'Memory operations, latency, and success rate over the last {days} days.',
  loading: 'Loading analytics...',
  totalOperations: 'Total Operations',
  avgLatency: 'Avg Latency',
  successRate: 'Success Rate',
  operationsOverTime: 'Operations over time',
  bucketTooltip: '{date}: {count}',
  usageByUser: 'Usage by user',
  noUserData: 'No per-user data yet.',
  user: 'User',
  totalOps: 'Total Ops',
};

const zh: Shape<typeof en> = {
  title: '分析',
  failedToLoad: '加载分析数据失败',
  failedToLoadUserStats: '加载用户分析数据失败',
  nonAdminDescription: '追踪记忆操作、延迟与使用模式随时间的变化。',
  adminRequired: '查看分析需要管理员权限。',
  description: '过去 {days} 天的记忆操作、延迟与成功率。',
  loading: '正在加载分析数据...',
  totalOperations: '操作总数',
  avgLatency: '平均延迟',
  successRate: '成功率',
  operationsOverTime: '随时间变化的操作',
  bucketTooltip: '{date}：{count}',
  usageByUser: '按用户使用情况',
  noUserData: '暂无按用户统计数据。',
  user: '用户',
  totalOps: '操作数',
};

const ja: Shape<typeof en> = {
  title: '分析',
  failedToLoad: '分析データの読み込みに失敗しました',
  failedToLoadUserStats: 'ユーザー分析データの読み込みに失敗しました',
  nonAdminDescription:
    'メモリ操作、レイテンシ、利用パターンの推移を追跡します。',
  adminRequired: '分析を表示するには管理者権限が必要です。',
  description: '直近 {days} 日間のメモリ操作、レイテンシ、成功率。',
  loading: '分析データを読み込み中...',
  totalOperations: '操作合計',
  avgLatency: '平均レイテンシ',
  successRate: '成功率',
  operationsOverTime: '操作の推移',
  bucketTooltip: '{date}：{count}',
  usageByUser: 'ユーザー別の利用状況',
  noUserData: 'ユーザー別のデータはまだありません。',
  user: 'ユーザー',
  totalOps: '操作数',
};

const ko: Shape<typeof en> = {
  title: '분석',
  failedToLoad: '분석 데이터를 불러오지 못했습니다',
  failedToLoadUserStats: '사용자 분석 데이터를 불러오지 못했습니다',
  nonAdminDescription:
    '시간에 따른 메모리 작업, 지연 시간, 사용 패턴을 추적합니다.',
  adminRequired: '분석을 보려면 관리자 권한이 필요합니다.',
  description: '지난 {days}일간의 메모리 작업, 지연 시간, 성공률입니다.',
  loading: '분석 데이터를 불러오는 중...',
  totalOperations: '총 작업 수',
  avgLatency: '평균 지연 시간',
  successRate: '성공률',
  operationsOverTime: '시간별 작업량',
  bucketTooltip: '{date}: {count}',
  usageByUser: '사용자별 사용량',
  noUserData: '아직 사용자별 데이터가 없습니다.',
  user: '사용자',
  totalOps: '작업 수',
};

export const analytics = { en, zh, ja, ko } as const;
