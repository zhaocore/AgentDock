// 命名空间 requests：请求日志页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Requests',
  subtitle: 'Recent request logs from your self-hosted instance.',
  failedToLoad: 'Failed to load request logs',
  time: 'Time',
  method: 'Method',
  path: 'Path',
  latency: 'Latency',
  auth: 'Auth',
  totalRequests: 'Total Requests',
  successRate: 'Success Rate',
  avgLatency: 'Avg Latency',
  emptyTitle: 'No request logs yet',
  emptyDescription:
    'Requests will appear here once your instance receives traffic.',
  pageRange: '{start}–{end} of {total}',
  previous: 'Previous',
  next: 'Next',
};

const zh: Shape<typeof en> = {
  title: '请求',
  subtitle: '来自自托管实例的最近请求日志。',
  failedToLoad: '加载请求日志失败',
  time: '时间',
  method: '方法',
  path: '路径',
  latency: '延迟',
  auth: '认证',
  totalRequests: '请求总数',
  successRate: '成功率',
  avgLatency: '平均延迟',
  emptyTitle: '暂无请求日志',
  emptyDescription: '实例收到流量后，请求会显示在这里。',
  pageRange: '{start}–{end} / 共 {total} 条',
  previous: '上一页',
  next: '下一页',
};

const ja: Shape<typeof en> = {
  title: 'リクエスト',
  subtitle: 'セルフホスト環境の最近のリクエストログです。',
  failedToLoad: 'リクエストログの読み込みに失敗しました',
  time: '時刻',
  method: 'メソッド',
  path: 'パス',
  latency: 'レイテンシ',
  auth: '認証',
  totalRequests: 'リクエスト合計',
  successRate: '成功率',
  avgLatency: '平均レイテンシ',
  emptyTitle: 'リクエストログはまだありません',
  emptyDescription:
    'インスタンスがトラフィックを受け取ると、リクエストが表示されます。',
  pageRange: '{start}–{end} / {total} 件',
  previous: '前へ',
  next: '次へ',
};

const ko: Shape<typeof en> = {
  title: '요청',
  subtitle: '셀프 호스팅 인스턴스의 최근 요청 로그입니다.',
  failedToLoad: '요청 로그를 불러오지 못했습니다',
  time: '시간',
  method: '메서드',
  path: '경로',
  latency: '지연 시간',
  auth: '인증',
  totalRequests: '총 요청 수',
  successRate: '성공률',
  avgLatency: '평균 지연 시간',
  emptyTitle: '아직 요청 로그가 없습니다',
  emptyDescription: '인스턴스에 트래픽이 유입되면 요청이 표시됩니다.',
  pageRange: '{start}–{end} / 총 {total}건',
  previous: '이전',
  next: '다음',
};

export const requests = { en, zh, ja, ko } as const;
