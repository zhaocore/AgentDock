import '@/styles/globals.css';
import '@/features/dutybot/dutybot.css';
import React from 'react';
import { Inter, InterDisplay, Roboto, Fustat, DMMono } from '../(root)/fonts';
import { cn } from '@/lib/utils';
import { ThemeProvider } from '@/components/theme-provider';
import { AuthProvider } from '@/lib/auth';
import { LocaleProvider } from '@/i18n/core';

export const metadata = {
  title: 'dutybotGo',
  description: 'dutybotGo account',
};

export default function AuthLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html
      lang="en"
      className={cn(
        Fustat.variable,
        InterDisplay.variable,
        Inter.variable,
        Roboto.variable,
        DMMono.variable,
      )}
      suppressHydrationWarning
    >
      <body className="font-fustat" suppressHydrationWarning>
        <AuthProvider>
          <ThemeProvider
            attribute="class"
            defaultTheme="system"
            enableSystem
            disableTransitionOnChange
          >
            <LocaleProvider>{children}</LocaleProvider>
          </ThemeProvider>
        </AuthProvider>
      </body>
    </html>
  );
}
