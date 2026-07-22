import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { ScrollArea } from "./ui/scroll-area";
import type { LogLine } from "../hooks/useRunStream";

// LogPane is the collapsible, auto-scrolling live log view. Every line is
// rendered verbatim as a text node — never re-parsed, never re-formatted —
// so a redacted "***" (or anything else the server already sanitized) reaches
// the DOM exactly as sent.
export function LogPane({ lines }: { lines: LogLine[] }) {
  const [open, setOpen] = useState(true);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) bottomRef.current?.scrollIntoView?.();
  }, [lines.length, open]);

  return (
    <div className="rounded-md border">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 px-3 py-2 text-sm font-medium"
      >
        {open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
        Logs
      </button>
      {open && (
        <ScrollArea className="h-64 border-t">
          <div className="space-y-0.5 p-3 font-mono text-xs leading-relaxed">
            {lines.map((l, i) => (
              <div key={i} className="whitespace-pre-wrap break-all">{l.line}</div>
            ))}
            <div ref={bottomRef} />
          </div>
        </ScrollArea>
      )}
    </div>
  );
}
