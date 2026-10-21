'use client';

import { useContext } from 'react';
import { AuthContext } from '@/lib/auth';

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) throw new Error('useAuth requires AuthProvider');
  return context;
}
