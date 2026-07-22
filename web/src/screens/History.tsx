import { Link } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Badge } from "../components/ui/badge";
import { useRuns } from "../hooks";
import { formatDuration } from "../components/StepList";
import type { RunStatus } from "../api/types";

const STATUS_VARIANT: Record<RunStatus, string> = {
  success: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-100",
  failed: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-100",
  running: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-100",
  pending: "bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-100",
  cancelled: "bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-100",
  skipped: "bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-100",
  blocked: "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-100",
};

function formatDate(isoString: string): string {
  try {
    return new Date(isoString).toLocaleString();
  } catch {
    return isoString;
  }
}

function runDuration(startedAt: string, finishedAt?: string): string | null {
  if (!startedAt || !finishedAt) return null;
  try {
    const ms = new Date(finishedAt).getTime() - new Date(startedAt).getTime();
    if (Number.isNaN(ms)) return null;
    return formatDuration(ms);
  } catch {
    return null;
  }
}

export function History() {
  const { data: runs } = useRuns();

  if (!runs || runs.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Run History</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground">No runs yet.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Run History</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="px-3 py-2 text-left font-semibold">Date</th>
                <th className="px-3 py-2 text-left font-semibold">Profile</th>
                <th className="px-3 py-2 text-left font-semibold">SHA</th>
                <th className="px-3 py-2 text-left font-semibold">Status</th>
                <th className="px-3 py-2 text-left font-semibold">Duration</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id} className="border-b hover:bg-muted/50 transition-colors">
                  <td className="px-3 py-2">
                    <Link to={`/run/${run.id}`} className="text-blue-600 hover:underline dark:text-blue-400">
                      {formatDate(run.started_at)}
                    </Link>
                  </td>
                  <td className="px-3 py-2">
                    <Link to={`/run/${run.id}`} className="text-blue-600 hover:underline dark:text-blue-400">
                      {run.profile}
                    </Link>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">
                    <Link to={`/run/${run.id}`} className="text-blue-600 hover:underline dark:text-blue-400">
                      {run.config_sha.substring(0, 8)}
                    </Link>
                  </td>
                  <td className="px-3 py-2">
                    <Badge className={STATUS_VARIANT[run.status]}>
                      {run.status}
                    </Badge>
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">
                    {runDuration(run.started_at, run.finished_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}
