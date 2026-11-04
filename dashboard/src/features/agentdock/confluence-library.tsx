'use client';

import { useMemo, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import { api } from '@/utils/api';
import { getErrorMessage } from '@/lib/error-message';
import { useTranslation } from '@/i18n/core';
import { useResource } from './use-resource';
import { Notice } from './ui';
import { buildLibraryDirectories, type LibraryDirectory } from './library-tree';
import {
  confluenceLibrarySchema,
  confluenceImportReportSchema,
  markdownFileSchema,
} from './schemas';

/** 展开分支后再创建子目录 DOM，支持大型 Space。 */
function Directory({
  node,
  selected,
  onDirectory,
  onPage,
}: {
  node: LibraryDirectory;
  selected: string;
  onDirectory: (path: string) => void;
  onPage: (id: string) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <li>
      <div className="flex gap-1 items-center">
        <button
          type="button"
          className="p-1"
          aria-expanded={open}
          data-testid={`library-expand-${encodeURIComponent(node.path)}`}
          onClick={() => setOpen(!open)}
        >
          {open ? '▾' : '▸'}
        </button>
        <button
          type="button"
          className={`text-left py-1 break-all ${selected === node.path ? 'font-semibold text-emerald-600' : ''}`}
          data-testid={`library-directory-${encodeURIComponent(node.path)}`}
          onClick={() => {
            onDirectory(node.path);
            setOpen(true);
          }}
        >
          {node.name}
        </button>
      </div>
      {open && (
        <ul className="pl-5 border-l">
          {node.page && (
            <li>
              <button
                type="button"
                className="text-left py-1 hover:underline break-all"
                data-testid={`library-page-${node.page.id}`}
                onClick={() => onPage(node.page!.id)}
              >
                {node.page.title}.md
              </button>
            </li>
          )}
          {node.children.map((child) => (
            <Directory
              key={child.path}
              node={child}
              selected={selected}
              onDirectory={onDirectory}
              onPage={onPage}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

/** 多 Space 文件库及带覆盖检查的 ZIP 导入/导出。 */
export function ConfluenceLibrary() {
  const { t } = useTranslation();
  const library = useResource(
    '/confluence-library',
    confluenceLibrarySchema,
    null,
  );
  const [spaceID, setSpaceID] = useState('');
  const space =
    library.data?.spaces.find((item) => item.id === spaceID) ??
    library.data?.spaces[0];
  const [directory, setDirectory] = useState('');
  const [pageID, setPageID] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [report, setReport] = useState<z.infer<
    typeof confluenceImportReportSchema
  > | null>(null);
  const [notice, setNotice] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  const directories = useMemo(
    () => buildLibraryDirectories(space?.pages ?? []),
    [space?.pages],
  );
  const fileBase =
    space && pageID
      ? `/confluence-library/spaces/${space.id}/pages/${pageID}/file`
      : null;
  const markdown = useResource(
    fileBase ? `${fileBase}?name=page.md` : null,
    markdownFileSchema,
    null,
  );
  const currentPage = space?.pages.find((item) => item.id === pageID);
  const exportURL = space
    ? `/api/agentdock/confluence-library/archive?space_id=${space.id}${directory ? `&path=${encodeURIComponent(directory)}` : ''}`
    : '';
  const download = async (url: string) => {
    if (exporting) return;
    setError('');
    setExporting(true);
    try {
      await api.get(
        url.replace('/api/agentdock', '').replace('/archive', '/archive-check'),
      );
      const link = document.createElement('a');
      link.href = url;
      link.download = 'confluence-library.zip';
      document.body.appendChild(link);
      link.click();
      link.remove();
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setExporting(false);
    }
  };
  const preview = async (file: File) => {
    setError('');
    setReport(null);
    setNotice('');
    if (file.size > 10 * 1024 * 1024 * 1024) {
      setError(t('agentdock.libraryZIPLimit'));
      return;
    }
    setBusy(true);
    try {
      const response = await api.post('/confluence-library/imports', file, {
        headers: { 'Content-Type': 'application/zip' },
      });
      setReport(confluenceImportReportSchema.parse(response.data));
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  const apply = async () => {
    if (!report) return;
    setError('');
    setBusy(true);
    try {
      await api.post(`/confluence-library/imports/${report.id}/apply`);
      setReport(null);
      setNotice(t('agentdock.libraryMerged'));
      library.refresh();
      setPageID('');
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <div className="gg-toolbar">
        <h1>{t('agentdock.confluenceLibrary')}</h1>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            data-testid="library-refresh"
            onClick={library.refresh}
          >
            {t('agentdock.refresh')}
          </Button>
          {library.data && library.data.spaces.length > 0 && (
            <Button variant="outline" asChild>
              <a
                download="confluence-library.zip"
                data-testid="library-export-all"
                href="/api/agentdock/confluence-library/archive"
                aria-disabled={exporting}
                onClick={(event) => {
                  event.preventDefault();
                  void download('/api/agentdock/confluence-library/archive');
                }}
              >
                {t('agentdock.libraryExportAll')}
              </a>
            </Button>
          )}
          <Button
            data-testid="library-import"
            disabled={busy}
            onClick={() => {
              if (fileInput.current) {
                fileInput.current.value = '';
                fileInput.current.click();
              }
            }}
          >
            {busy
              ? t('agentdock.libraryChecking')
              : t('agentdock.libraryImport')}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept=".zip,application/zip"
            data-testid="library-import-file"
            aria-label={t('agentdock.libraryImport')}
            className="sr-only"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void preview(file);
            }}
          />
        </div>
      </div>
      <p className="text-sm text-muted-foreground mb-4">
        {t('agentdock.libraryPolicy')}
      </p>
      <Notice
        error={error || library.error || markdown.error}
        loading={library.loading || exporting}
      />
      {notice && (
        <p
          role="status"
          data-testid="library-notice"
          className="my-3 text-emerald-700"
        >
          {notice}
        </p>
      )}
      {report && (
        <section
          className="border rounded p-4 mb-4"
          data-testid="library-import-report"
        >
          <h2 className="font-semibold">{t('agentdock.libraryPreview')}</h2>
          <p data-testid="library-import-counts" className="my-2">
            {t('agentdock.libraryAdded')}: {report.added} ·{' '}
            {t('agentdock.libraryUpdated')}: {report.updated} ·{' '}
            {t('agentdock.libraryKept')}: {report.kept} ·{' '}
            {t('agentdock.libraryConflicts')}: {report.conflicts}
          </p>
          <div className="max-h-64 overflow-auto gg-table-wrap mb-3">
            <table className="gg-table">
              <thead>
                <tr>
                  <th>{t('agentdock.libraryDirectory')}</th>
                  <th>{t('agentdock.status')}</th>
                  <th>{t('agentdock.libraryReason')}</th>
                </tr>
              </thead>
              <tbody>
                {report.items.map((item) => (
                  <tr key={`${item.space_id}-${item.page_id}`}>
                    <td>{item.path}</td>
                    <td>{t(`agentdock.libraryAction_${item.action}`)}</td>
                    <td>{item.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex gap-2">
            <Button
              data-testid="library-import-apply"
              disabled={busy || report.added + report.updated === 0}
              onClick={() => void apply()}
            >
              {t('agentdock.libraryConfirm')}
            </Button>
            <Button
              data-testid="library-import-cancel"
              variant="outline"
              disabled={busy}
              onClick={() => setReport(null)}
            >
              {t('agentdock.cancel')}
            </Button>
          </div>
        </section>
      )}
      {library.data?.spaces.length === 0 && (
        <p className="py-8 text-muted-foreground" data-testid="library-empty">
          {t('agentdock.libraryEmpty')}
        </p>
      )}
      <div className="grid gap-3 md:grid-cols-3 mb-4">
        {library.data?.spaces.map((item) => (
          <button
            type="button"
            key={item.id}
            data-testid={`library-space-${item.id}`}
            className={`rounded border p-4 text-left ${space?.id === item.id ? 'border-emerald-600' : ''}`}
            onClick={() => {
              setSpaceID(item.id);
              setDirectory('');
              setPageID('');
            }}
          >
            <strong>{item.key}</strong>
            <p className="text-xs text-muted-foreground break-all">
              {item.base_url}
            </p>
            <p className="text-sm mt-2">
              {item.pages.length} {t('agentdock.libraryPages')}
            </p>
          </button>
        ))}
      </div>
      {space && (
        <>
          <div className="flex flex-wrap items-center gap-2 mb-3">
            <Button
              variant="ghost"
              data-testid="library-root"
              onClick={() => setDirectory('')}
            >
              {space.key}
            </Button>
            <span className="text-sm break-all">
              {directory ? `/ ${directory}` : ''}
            </span>
            <Button asChild variant="outline" className="ml-auto">
              <a
                download="confluence-library.zip"
                data-testid="library-export-directory"
                href={exportURL}
                aria-disabled={exporting}
                onClick={(event) => {
                  event.preventDefault();
                  void download(exportURL);
                }}
              >
                {t('agentdock.libraryExportDirectory')}
              </a>
            </Button>
          </div>
          <div className="grid gap-4 lg:grid-cols-[minmax(240px,1fr)_3fr]">
            <aside
              className="border rounded p-3 overflow-auto max-h-[70vh]"
              data-testid="library-tree"
            >
              <ul>
                {directories.map((node) => (
                  <Directory
                    key={`${space.id}-${node.path}`}
                    node={node}
                    selected={directory}
                    onDirectory={setDirectory}
                    onPage={setPageID}
                  />
                ))}
              </ul>
            </aside>
            <article
              className="border rounded p-4 overflow-auto prose dark:prose-invert max-w-none"
              data-testid="library-markdown"
            >
              {currentPage && (
                <p className="text-xs text-muted-foreground">
                  {currentPage.path} · {t('agentdock.libraryModified')}:{' '}
                  {currentPage.modified_at}
                </p>
              )}
              {markdown.data && (
                <ReactMarkdown
                  skipHtml
                  remarkPlugins={[remarkGfm]}
                  urlTransform={(value, key) => {
                    if (value.startsWith('./assets/'))
                      return `/api/agentdock${fileBase}?name=${encodeURIComponent(value.slice(2))}`;
                    if (key === 'src') return '';
                    return /^(https?:\/\/|#)/.test(value) ? value : '';
                  }}
                >
                  {markdown.data}
                </ReactMarkdown>
              )}
            </article>
          </div>
        </>
      )}
    </>
  );
}
