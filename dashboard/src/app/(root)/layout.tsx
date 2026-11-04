import { Metadata } from 'next';
import { DashboardClientLayout } from './dashboard-client-layout';

export const metadata: Metadata = {
  title: 'AgentDock',
  description: 'AgentDock knowledge workspace',
};

export default function DashboardLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return <DashboardClientLayout>{children}</DashboardClientLayout>;
}
