import { Shell } from '@/features/dutybot/shell';

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <Shell>{children}</Shell>;
}
