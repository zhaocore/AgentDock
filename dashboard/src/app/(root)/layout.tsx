import { Metadata } from 'next';
import { DashboardClientLayout } from './dashboard-client-layout';

export const metadata: Metadata = {
  title: 'dutybotGo',
  description: 'dutybotGo knowledge workspace',
};

export default function DashboardLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return <DashboardClientLayout>{children}</DashboardClientLayout>;
}
