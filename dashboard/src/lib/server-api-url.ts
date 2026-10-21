/** 上游地址只从服务端配置读取，浏览器不能选择代理目的地。 */
export function getServerApiUrl(): string {
  const url = new URL(
    process.env.API_INTERNAL_URL || 'http://127.0.0.1:8080/v1',
  );
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  ) {
    throw new Error(
      'API_INTERNAL_URL must be an HTTP(S) API URL without credentials, query or fragment',
    );
  }
  return url.toString().replace(/\/$/, '');
}
