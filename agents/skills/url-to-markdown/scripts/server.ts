import { confluenceBundle, planConfluence } from './confluence';

/** 内存协议由服务端生成，转换器不持有凭据也不写文档。 */
const input = JSON.parse(await Bun.stdin.text()) as {
  page: string;
  url: string;
  assets?: {name: string; content: string}[];
};
if (typeof input.page !== 'string' || typeof input.url !== 'string') throw new Error('缺少 page/url');
/** 等待管道完整写入，避免大文件包在进程退出时截断。 */
const output = JSON.stringify(input.assets === undefined
  ? planConfluence(input.page, input.url)
  : confluenceBundle(input.page, input.url, input.assets)) + '\n';
await new Promise<void>((resolve, reject) => {
  process.stdout.write(output, (error) => error ? reject(error) : resolve());
});
