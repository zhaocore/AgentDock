'use client';

import { BookOpen } from 'lucide-react';

export default function ThemeAwareLogo() {
  return (
    <div className="flex items-center gap-2 text-xl font-semibold">
      <BookOpen className="size-6 text-emerald-600" />
      AgentDock
    </div>
  );
}
