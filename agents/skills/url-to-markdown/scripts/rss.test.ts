import { afterAll, beforeAll, expect, test } from 'bun:test';
import { chromium, type Browser } from 'playwright';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import https, { request } from 'node:https';
import { captureInContext } from './crawl';
import { parseRSS, rssMarkdown } from './rss';

let browser: Browser;
beforeAll(async () => { browser = await chromium.launch(); });
afterAll(async () => { await browser.close(); });

const feed = `<rss version="2.0" xmlns:body="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/"><channel><title>订阅 &amp; 文档</title><link>https://news.example/</link><description>订阅介绍</description><lastBuildDate>Fri, 30 Oct 2026 10:00:00 +0800</lastBuildDate><item><title>文章 &lt;标题&gt;</title><link>https://news.example/article/one</link><pubDate>Fri, 30 Oct 2026 09:00:00 +0800</pubDate><dc:creator>作者</dc:creator><category>汽车</category><description>摘要不应代替全文</description><body:encoded><![CDATA[<p>全部正文 <strong>重要</strong></p><a href="../next?a=1&amp;b=2">关联链接</a><img src="/image.png"><script>fetch('/do-not-call')</script>]]></body:encoded></item><item><title>第二条</title><link>/two</link><description>&lt;p&gt;仅有描述的正文&lt;/p&gt;</description></item></channel></rss>`;

test('严格解析 RSS 的命名空间、实体与 CDATA，转换全部条目和来源', async () => {
  const page = await browser.newPage();
  try {
    const parsed = await page.evaluate(parseRSS, feed);
    if (!parsed) throw new Error('测试订阅未识别');
    const result = rssMarkdown(parsed, 'https://feed.example/feed.xml');
    expect(result.title).toBe('订阅 & 文档');
    expect(result.markdown).toContain('## 2. 第二条');
    expect(result.markdown).toContain('仅有描述的正文');
    expect(result.markdown).toContain('全部正文 **重要**');
    expect(result.markdown).toContain('作者：作者');
    expect(result.markdown).toContain('分类：汽车');
    expect(result.markdown).toContain('发布时间：Fri, 30 Oct 2026');
    expect(result.markdown).toContain('https://news.example/image.png');
    expect(result.markdown).not.toContain('摘要不应代替全文');
    expect(result.markdown).not.toContain('do-not-call');
    expect(result.links).toEqual(['https://news.example/article/one', 'https://news.example/next?a=1&b=2', 'https://feed.example/two']);
    const blocks = rssMarkdown({...parsed,items:[{...parsed.items[0],html:'<article>第一正文块</article><article>第二正文块</article><footer>署名元数据</footer>'}]},'https://feed.example/multiple');
    expect(blocks.markdown).toContain('第一正文块');
    expect(blocks.markdown).toContain('第二正文块');
    expect(blocks.markdown).toContain('署名元数据');
    expect(rssMarkdown({...parsed,items:[]},'https://feed.example/empty').markdown).toContain('条目数量：0');
    expect(() => rssMarkdown({...parsed,items:[{...parsed.items[0],html:'文'.repeat(240001)}]}, 'https://feed.example/large')).toThrow('rss_empty_or_oversized_document');
    expect(() => rssMarkdown({...parsed,items:[{...parsed.items[0],link:'javascript:alert(1)'}]}, 'https://feed.example/unsafe')).toThrow('invalid_rss_link');
  } finally { await page.close(); }
});

test('RSS 无标题条目保留编号与描述，只有标题的条目仍可转换', async () => {
  const page = await browser.newPage();
  try {
    const parsed = await page.evaluate(parseRSS, '<rss version="2.0"><channel><title>Feed</title><item><description>完整正文</description></item><item><title>只有标题</title></item></channel></rss>');
    if (!parsed) throw new Error('测试订阅未识别');
    const result = rssMarkdown(parsed, 'https://feed.example/rss');
    expect(parsed.items[0].title).toBe('');
    expect(result.markdown).toContain('## 1.');
    expect(result.markdown).toContain('完整正文');
    expect(result.markdown).toContain('## 2. 只有标题');
  } finally { await page.close(); }
});

test('CDATA、注释及处理指令中的声明字面内容不阻断合法 RSS', async () => {
  const page = await browser.newPage();
  try {
    const xml = '<?xml version="1.0"?><!-- <!DOCTYPE rss> --><?sample <!ENTITY example "text">?><rss version="2.0"><channel><title>Feed</title><item><title>HTML 示例</title><description><![CDATA[<p>示例正文</p><!-- <!DOCTYPE html> <!ENTITY example "text"> -->]]></description></item></channel></rss>';
    const parsed = await page.evaluate(parseRSS, xml);
    if (!parsed) throw new Error('测试订阅未识别');
    expect(parsed.items[0].html).toContain('<!DOCTYPE html>');
    expect(rssMarkdown(parsed, 'https://feed.example/rss').markdown).toContain('示例正文');
    await expect(page.evaluate(parseRSS, '<!-- <![CDATA[ --> <!DOCTYPE rss [<!ENTITY example "expanded">]><rss version="2.0"><channel><title>&example;</title></channel></rss>')).rejects.toThrow('rss_dtd_not_allowed');
  } finally { await page.close(); }
});

test('XML 截断、DTD、缺 channel 或必要内容明确失败，非 RSS XML 不伪装为订阅', async () => {
  const page = await browser.newPage();
  try {
    for (const [xml, code] of [
      ['<rss version="2.0"><channel>', 'invalid_rss_xml'],
      ['<!DOCTYPE rss [<!ENTITY secret SYSTEM "https://private.example/">]><rss version="2.0"/>', 'rss_dtd_not_allowed'],
      ['<rss version="2.0"/>', 'rss_channel_title_required'],
      ['<rss version="2.0"><channel><title>Feed</title><item/></channel></rss>', 'rss_item_title_or_description_required'],
      ['<rss version="1.0"/>', 'unsupported_rss_version'],
    ]) await expect(page.evaluate(parseRSS, xml)).rejects.toThrow(code);
    expect(await page.evaluate(parseRSS, '<document>普通 XML</document>')).toBeNull();
  } finally { await page.close(); }
});

test('真实 HTTPS 采集识别 RSS／XML／纯文本订阅，保留长正文且不抓文章或图片', {timeout:60000}, async () => {
  const directory = await mkdtemp(path.join(tmpdir(),'rss-capture-test-'));
  execFileSync('openssl',['req','-x509','-newkey','rsa:2048','-nodes','-keyout',path.join(directory,'key.pem'),'-out',path.join(directory,'cert.pem'),'-days','1','-subj','/CN=localhost'],{stdio:'ignore'});
  const visited: string[] = [];
  const server = https.createServer({key:await readFile(path.join(directory,'key.pem')),cert:await readFile(path.join(directory,'cert.pem'))},(request,response) => {
    visited.push(request.url!);
    const pathname = new URL(request.url!,'https://localhost').pathname;
    const types: Record<string,string> = {'/rss':'application/rss+xml','/xml':'application/xml','/text':'text/xml','/plain':'text/plain'};
    response.writeHead(200,{'content-type':types[pathname]??'application/rss+xml'});
    if(pathname==='/broken'){response.end('<rss><channel>');return;}
    if(pathname==='/auth'){response.end(feed.replace('订阅介绍', '文'.repeat(26000)));return;}
    response.end(feed);
  });
  await new Promise<void>(resolve=>server.listen(0,'127.0.0.1',resolve));
  const address=server.address();
  if(!address||typeof address==='string')throw new Error('测试 TLS 服务地址缺失');
  const origin=`https://127.0.0.1:${address.port}`;
  try {
    for(const route of ['/rss','/xml','/text','/plain','/auth','/broken']) {
      const context=await browser.newContext({ignoreHTTPSErrors:true,acceptDownloads:false,serviceWorkers:'block'});
      try {
        const capture=captureInContext({url:origin+route,origin,cookie:'',ready_selector:''},context,(url,options,listener)=>request(url,{...options,rejectUnauthorized:false},listener));
        if(route==='/broken'){await expect(capture).rejects.toThrow('invalid_rss_xml');continue;}
        const page=await capture;
        expect(page.format).toBe('rss');
        expect(page.markdown).toContain('## 2. 第二条');
        if(route==='/auth')expect(Array.from(page.markdown).length).toBeGreaterThan(24000);
      } finally {await context.close();}
    }
    expect(visited).not.toContain('/image.png');
    expect(visited).not.toContain('/do-not-call');
    expect(visited).not.toContain('/two');
  } finally {server.closeAllConnections();await new Promise<void>(resolve=>server.close(()=>resolve()));await rm(directory,{recursive:true});}
});
