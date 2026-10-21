// 命名空间 configuration：配置页词条。en 为键结构真源。
import type { Shape } from '../dict-shape';

const en = {
  title: 'Configuration',
  failedToLoadConfig: 'Failed to load server configuration',
  failedToLoadProviders: 'Failed to load bundled providers',
  loadingEffective: 'Loading effective server configuration...',
  savedToast: 'Configuration saved',
  saveFailedToast: 'Failed to save configuration',
  llmProviderTitle: 'LLM Provider',
  embeddingModelTitle: 'Embedding Model',
  provider: 'Provider',
  model: 'Model',
  apiKey: 'API Key',
  selectProvider: 'Select provider',
  needAnotherProvider:
    'Need another provider? Install its Python package, rebuild the image, and extend the bundled list.',
  seeSetupGuide: 'See the',
  setupGuide: 'setup guide',
  saveConfiguration: 'Save Configuration',
  ssoMessage: 'Looking for SSO / SAML? Available in Enterprise.',
  contactSales: 'Contact sales',
};

const zh: Shape<typeof en> = {
  title: '配置',
  failedToLoadConfig: '加载服务器配置失败',
  failedToLoadProviders: '加载内置 provider 失败',
  loadingEffective: '正在加载生效的服务器配置...',
  savedToast: '配置已保存',
  saveFailedToast: '保存配置失败',
  llmProviderTitle: 'LLM 提供方',
  embeddingModelTitle: 'Embedding 模型',
  provider: '提供方',
  model: '模型',
  apiKey: 'API 密钥',
  selectProvider: '选择提供方',
  needAnotherProvider:
    '需要其他提供方？安装对应的 Python 包、重新构建镜像，并扩展内置列表。',
  seeSetupGuide: '详见',
  setupGuide: '设置指南',
  saveConfiguration: '保存配置',
  ssoMessage: '需要 SSO / SAML？Enterprise 版可用。',
  contactSales: '联系销售',
};

const ja: Shape<typeof en> = {
  title: '設定',
  failedToLoadConfig: 'サーバー設定の読み込みに失敗しました',
  failedToLoadProviders: '組み込みプロバイダーの読み込みに失敗しました',
  loadingEffective: '有効なサーバー設定を読み込み中...',
  savedToast: '設定を保存しました',
  saveFailedToast: '設定の保存に失敗しました',
  llmProviderTitle: 'LLM プロバイダー',
  embeddingModelTitle: 'Embedding モデル',
  provider: 'プロバイダー',
  model: 'モデル',
  apiKey: 'API キー',
  selectProvider: 'プロバイダーを選択',
  needAnotherProvider:
    '別のプロバイダーが必要ですか？対応する Python パッケージをインストールし、イメージを再構築して、組み込みリストを拡張してください。',
  seeSetupGuide: '詳しくは',
  setupGuide: 'セットアップガイド',
  saveConfiguration: '設定を保存',
  ssoMessage: 'SSO / SAML をお探しですか？Enterprise で利用できます。',
  contactSales: '営業に問い合わせる',
};

const ko: Shape<typeof en> = {
  title: '설정',
  failedToLoadConfig: '서버 설정을 불러오지 못했습니다',
  failedToLoadProviders: '내장 제공자를 불러오지 못했습니다',
  loadingEffective: '적용된 서버 설정을 불러오는 중...',
  savedToast: '설정이 저장되었습니다',
  saveFailedToast: '설정 저장 실패',
  llmProviderTitle: 'LLM 제공자',
  embeddingModelTitle: 'Embedding 모델',
  provider: '제공자',
  model: '모델',
  apiKey: 'API 키',
  selectProvider: '제공자 선택',
  needAnotherProvider:
    '다른 제공자가 필요하신가요? 해당 Python 패키지를 설치하고 이미지를 다시 빌드한 뒤 내장 목록을 확장하세요.',
  seeSetupGuide: '자세한 내용은',
  setupGuide: '설정 가이드',
  saveConfiguration: '설정 저장',
  ssoMessage: 'SSO / SAML이 필요하신가요? Enterprise에서 사용할 수 있습니다.',
  contactSales: '영업팀에 문의',
};

export const configuration = { en, zh, ja, ko } as const;
