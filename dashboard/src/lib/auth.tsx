'use client';

import {
  createContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react';
import axios from 'axios';
import { api } from '@/utils/api';
import { getErrorMessage } from '@/lib/error-message';
import { userSchema, type AuthUser } from '@/features/agentdock/schemas';

interface AuthContextValue {
  user: AuthUser | null;
  isLoading: boolean;
  error: string;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refreshUser: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const refreshUser = useCallback(async () => {
    const response = await api.get('/auth/me');
    setUser(userSchema.parse(response.data));
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    api
      .get('/auth/me', { signal: controller.signal })
      .then((response) => {
        setUser(userSchema.parse(response.data));
      })
      .catch((cause) => {
        if (
          !axios.isCancel(cause) &&
          !(axios.isAxiosError(cause) && cause.response?.status === 401)
        )
          setError(getErrorMessage(cause));
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsLoading(false);
      });
    return () => controller.abort();
  }, []);
  const login = useCallback(
    async (email: string, password: string) => {
      await api.post('/auth/login', { email, password });
      await refreshUser();
    },
    [refreshUser],
  );
  const register = useCallback(
    async (email: string, password: string) => {
      await api.post('/auth/register', { email, password });
      await refreshUser();
    },
    [refreshUser],
  );
  const logout = useCallback(async () => {
    await axios.delete('/api/session');
    setUser(null);
    window.location.assign('/login');
  }, []);
  const value = useMemo(
    () => ({ user, isLoading, error, login, register, logout, refreshUser }),
    [user, isLoading, error, login, register, logout, refreshUser],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
