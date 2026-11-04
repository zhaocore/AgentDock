import type { ConfluenceLibraryPage } from './schemas';

export type LibraryDirectory = {
  name: string;
  path: string;
  page: ConfluenceLibraryPage | null;
  children: LibraryDirectory[];
};

/** 每页路径对应一个目录，纯构建新树，保留嵌套目录中的 Markdown。 */
export function buildLibraryDirectories(
  pages: ConfluenceLibraryPage[],
): LibraryDirectory[] {
  const roots: LibraryDirectory[] = [];
  for (const page of pages) {
    let siblings = roots;
    const segments = page.path.split('/');
    let path = '';
    for (let index = 0; index < segments.length; index++) {
      const name = segments[index];
      path = path ? `${path}/${name}` : name;
      let directory = siblings.find((item) => item.name === name);
      if (!directory) {
        directory = { name, path, page: null, children: [] };
        siblings.push(directory);
      }
      if (index === segments.length - 1) directory.page = page;
      siblings = directory.children;
    }
  }
  return roots;
}
