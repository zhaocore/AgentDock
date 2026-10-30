import { parseHTML } from 'linkedom';
import { toMarkdown } from './main';

/** 清理不可执行内容并解析远程资源，不改变调用方输入。 */
function cleanDocument(html: string, base: string): Document {
  const { document } = parseHTML(html);
  for (const node of document.querySelectorAll('script,style,noscript,form')) node.remove();
  for (const node of document.querySelectorAll('[href],[src],[poster],[data]')) {
    for (const attribute of ['href','src','poster','data']) {
      const raw = node.getAttribute(attribute);
      if (raw && !raw.startsWith('#')) {
        const address = URL.parse(raw, base);
        if (address && ['https:','http:'].includes(address.protocol)) node.setAttribute(attribute,address.href);
        else node.removeAttribute(attribute);
      }
    }
  }
  return document;
}

/** 网页提取主要正文，保留远程资源链接，不递归下载。 */
export function pageMarkdown(html: string, base: string): string {
  const document = cleanDocument(html, base);
  for (const node of document.querySelectorAll('nav,header,footer')) node.remove();
  const body = document.querySelector('main,article') ?? document.body;
  return toMarkdown(body.innerHTML).trim();
}

/** 订阅正文完整转换，保留多个文章块及元数据，不套用网页主区域筛选。 */
export function fragmentMarkdown(html: string, base: string): string {
  const document = cleanDocument(`<html><body>${html}</body></html>`, base);
  return toMarkdown(document.body.innerHTML).trim();
}

