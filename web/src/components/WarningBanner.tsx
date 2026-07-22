import { AlertTriangle } from "lucide-react";

// WarningBanner is the one place a browser operator sees a root-install risk
// before authorising it: stale config, unverified downloads (no sha256), and
// scripts that run as root. Deliberately loud (amber, icon, own paragraph) —
// this must never blend into the rest of the page.
export function WarningBanner({ message, detail }: { message: string; detail?: string }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-md border border-amber-500/50 bg-amber-500/10 p-3 text-sm"
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
      <div>
        <p className="font-medium text-amber-800 dark:text-amber-300">{message}</p>
        {detail && <p className="text-xs text-amber-800/80 dark:text-amber-300/80">{detail}</p>}
      </div>
    </div>
  );
}
