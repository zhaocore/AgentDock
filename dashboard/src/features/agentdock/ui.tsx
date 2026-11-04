'use client';

import { useState } from 'react';
import {
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  Trash2,
  X,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { useTranslation } from '@/i18n/core';
import { getErrorMessage } from '@/lib/error-message';

export function Notice({
  error,
  loading,
}: {
  error: string;
  loading: boolean;
}) {
  const { t } = useTranslation();
  if (error)
    return (
      <p
        role="alert"
        data-testid="api-error"
        className="break-words border-l-2 border-red-500 bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300"
      >
        {error}
      </p>
    );
  if (loading)
    return (
      <div
        role="status"
        className="flex items-center gap-2 py-4 text-sm text-muted-foreground"
      >
        <LoaderCircle className="size-4 animate-spin" />
        {t('agentdock.loading')}
      </div>
    );
  return null;
}

export function Pagination({
  offset,
  total,
  hasMore,
  onChange,
}: {
  offset: number;
  total: number;
  hasMore: boolean;
  onChange: (value: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-between border-t py-3 text-sm">
      <span className="text-muted-foreground">
        {total ? Math.min(offset + 1, total) : 0}–{Math.min(offset + 20, total)}{' '}
        / {total}
      </span>
      <div className="flex gap-2">
        <Button
          variant="outline"
          size="icon"
          title={t('agentdock.previous')}
          aria-label={t('agentdock.previous')}
          data-testid="page-previous"
          disabled={offset === 0}
          onClick={() => onChange(Math.max(0, offset - 20))}
        >
          <ChevronLeft className="size-4" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          title={t('agentdock.next')}
          aria-label={t('agentdock.next')}
          data-testid="page-next"
          disabled={!hasMore}
          onClick={() => onChange(offset + 20)}
        >
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  );
}

export function Status({ value }: { value: string }) {
  const color = [
    'active',
    'SUCCEEDED',
    'succeeded',
    'connected',
    'ok',
    'ready',
    'running',
  ].includes(value)
    ? 'text-emerald-700 bg-emerald-50 dark:bg-emerald-950 dark:text-emerald-300'
    : ['FAILED', 'failed', 'error', 'degraded'].includes(value)
      ? 'text-red-700 bg-red-50 dark:bg-red-950 dark:text-red-300'
      : 'text-sky-700 bg-sky-50 dark:bg-sky-950 dark:text-sky-300';
  return (
    <span
      className={`inline-block rounded px-2 py-1 text-xs font-medium ${color}`}
    >
      {value}
    </span>
  );
}

export function DeleteButton({
  onDelete,
  label,
  testId,
}: {
  onDelete: () => Promise<void>;
  label: string;
  testId: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const remove = async () => {
    setBusy(true);
    setError('');
    try {
      await onDelete();
      setOpen(false);
    } catch (cause) {
      setError(getErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        data-testid={testId}
        title={t('agentdock.delete')}
        aria-label={`${t('agentdock.delete')} ${label}`}
        onClick={() => setOpen(true)}
      >
        <Trash2 className="size-4 text-red-600" />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('agentdock.deleteTitle')}</DialogTitle>
            <DialogDescription>{t('agentdock.deleteDetail')}</DialogDescription>
          </DialogHeader>
          <p className="break-words font-medium">{label}</p>
          <Notice error={error} loading={false} />
          <div className="flex justify-end gap-2">
            <Button
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={busy}
            >
              <X className="size-4 mr-2" />
              {t('agentdock.cancel')}
            </Button>
            <Button
              variant="destructive"
              data-testid="delete-confirm"
              disabled={busy}
              onClick={remove}
            >
              <Trash2 className="size-4 mr-2" />
              {t('agentdock.delete')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
