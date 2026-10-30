import { fragmentMarkdown } from './markdown';
import { parseHTML } from 'linkedom';

export type RSSItem = {
  title: string;
  link: string;
  published: string;
  author: string;
  categories: string[];
  html: string;
};
export type RSSFeed = {
  title: string;
  link: string;
  description: string;
  updated: string;
  items: RSSItem[];
};

/** 在隔离浏览器中使用原生 XML 解析器，仅返回结构化数据，不执行订阅正文。 */
export function parseRSS(xml: string): RSSFeed | null {
  const declarations = xml.replace(/<!--[\s\S]*?-->|<!\[CDATA\[[\s\S]*?\]\]>|<\?[\s\S]*?\?>/g, '');
  if (/<!DOCTYPE\b|<!ENTITY\b/i.test(declarations)) throw new Error('rss_dtd_not_allowed');
  const document = new DOMParser().parseFromString(xml, 'application/xml');
  if (document.querySelector('parsererror')) throw new Error('invalid_rss_xml');
  const root = document.documentElement;
  if (root.localName !== 'rss') return null;
  if (root.getAttribute('version') !== '2.0') throw new Error('unsupported_rss_version');
  const child = (parent: Element, name: string): Element | undefined =>
    Array.from(parent.children).find(node => node.localName === name && node.namespaceURI === parent.namespaceURI);
  const text = (parent: Element, name: string): string => child(parent, name)?.textContent?.trim() ?? '';
  const channel = child(root, 'channel');
  if (!channel || !text(channel, 'title')) throw new Error('rss_channel_title_required');
  const items = Array.from(channel.children).filter(node => node.localName === 'item' && node.namespaceURI === channel.namespaceURI);
  return {
    title: text(channel, 'title'),
    link: text(channel, 'link'),
    description: text(channel, 'description'),
    updated: text(channel, 'lastBuildDate'),
    items: items.map(item => {
      const content = Array.from(item.children).find(node => node.localName === 'encoded' && node.namespaceURI === 'http://purl.org/rss/1.0/modules/content/');
      const creator = Array.from(item.children).find(node => node.localName === 'creator' && node.namespaceURI === 'http://purl.org/dc/elements/1.1/');
      const title = text(item, 'title');
      const description = text(item, 'description');
      if (!title && !description) throw new Error('rss_item_title_or_description_required');
      return {
        title,
        link: text(item, 'link'),
        published: text(item, 'pubDate'),
        author: creator?.textContent?.trim() || text(item, 'author'),
        categories: Array.from(item.children).filter(node => node.localName === 'category' && node.namespaceURI === item.namespaceURI).map(node => node.textContent?.trim() ?? ''),
        html: content?.textContent?.trim() || description,
      };
    }),
  };
}

/** RSS 链接只保留可访问的 HTTP 地址，拒绝凭据及可执行协议。 */
function rssURL(raw: string, base: string): string {
  const url = URL.parse(raw, base);
  if (!url || !['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw new Error('invalid_rss_link');
  return url.href;
}

/** 元数据以纯文本进入 Markdown，避免标题中的标记改变文档结构。 */
function markdownText(raw: string): string {
  return raw.replace(/\s+/g, ' ').replace(/[\\`*_[\]<>#]/g, '\\$&').trim();
}

/** 完整转换全部条目，正文链接按条目原文地址解析，超限明确失败。 */
export function rssMarkdown(feed: RSSFeed, source: string): { title: string; markdown: string; links: string[] } {
  const parts = [`# ${markdownText(feed.title)}`, `订阅来源：<${source}>`, `条目数量：${feed.items.length}`];
  if (feed.link) parts.push(`站点：<${rssURL(feed.link, source)}>`);
  if (feed.description) parts.push(fragmentMarkdown(feed.description, source));
  if (feed.updated) parts.push(`订阅更新时间：${markdownText(feed.updated)}`);
  const links = new Set<string>();
  for (const [index, item] of feed.items.entries()) {
    const link = item.link ? rssURL(item.link, source) : '';
    const base = link || source;
    parts.push(`## ${index + 1}. ${markdownText(item.title)}`);
    if (link) { parts.push(`原文：<${link}>`); links.add(link); }
    if (item.published) parts.push(`发布时间：${markdownText(item.published)}`);
    if (item.author) parts.push(`作者：${markdownText(item.author)}`);
    if (item.categories.length) parts.push(`分类：${item.categories.map(markdownText).join('、')}`);
    if (item.html) parts.push(fragmentMarkdown(item.html, base));
    const { document } = parseHTML(`<html><body>${item.html}</body></html>`);
    for (const anchor of document.querySelectorAll('a[href]')) {
      const address = URL.parse(anchor.getAttribute('href') ?? '', base);
      if (address && ['http:', 'https:'].includes(address.protocol) && !address.username && !address.password) links.add(address.href);
    }
  }
  const markdown = parts.filter(Boolean).join('\n\n');
  if (!feed.title.trim() || feed.title.length > 500 || Array.from(markdown).length > 240000) throw new Error('rss_empty_or_oversized_document');
  return { title: feed.title, markdown, links: [...links] };
}
