import { ApiError } from "../api/client";

// ErrorNotice renders any error uniformly: a sentence, plus the server's
// `details` list when present — which for a config validation failure names
// the exact file and field an operator must fix in their own repo.
export function ErrorNotice({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error);
  const details = error instanceof ApiError ? error.details : [];
  return (
    <div className="rounded border border-red-500/40 bg-red-500/10 p-3 text-sm">
      <p className="font-medium text-red-600 dark:text-red-400">{message}</p>
      {details.length > 0 && (
        <ul className="mt-2 list-disc pl-5 text-red-600/90 dark:text-red-400/90">
          {details.map((d, i) => <li key={i}>{d}</li>)}
        </ul>
      )}
    </div>
  );
}
