import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { CredentialsForm } from './credentials-form';
import { LocaleProvider } from '@/i18n/core';

const { replace } = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock('next/navigation', () => ({ useRouter: () => ({ replace }) }));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});

it('submits credentials once and navigates only after success', async () => {
  const submit = vi.fn().mockResolvedValue(undefined);
  render(
    <LocaleProvider>
      <CredentialsForm
        title="DutyBotGo"
        action="Login"
        submit={submit}
        destination="/dashboard/agents"
      >
        {null}
      </CredentialsForm>
    </LocaleProvider>,
  );
  fireEvent.change(screen.getByTestId('auth-email'), {
    target: { value: 'user@example.com' },
  });
  fireEvent.change(screen.getByTestId('auth-password'), {
    target: { value: 'password123' },
  });
  fireEvent.submit(screen.getByTestId('auth-form'));
  await waitFor(() =>
    expect(replace).toHaveBeenCalledWith('/dashboard/agents'),
  );
  expect(submit).toHaveBeenCalledExactlyOnceWith(
    'user@example.com',
    'password123',
  );
});

it('keeps failed authentication visible', async () => {
  render(
    <LocaleProvider>
      <CredentialsForm
        title="DutyBotGo"
        action="Login"
        submit={async () => {
          throw new Error('invalid credentials');
        }}
        destination="/dashboard/agents"
      >
        {null}
      </CredentialsForm>
    </LocaleProvider>,
  );
  fireEvent.submit(screen.getByTestId('auth-form'));
  await waitFor(() =>
    expect(screen.getByTestId('api-error').textContent).toBe(
      'invalid credentials',
    ),
  );
  expect(replace).not.toHaveBeenCalled();
});
