export interface SystemInfo {
  os: string;
  distro: string;
  version_id: string;
  arch: string;
  hostname: string;
  root: boolean;
}

export interface Settings {
  repo_url: string;
  ref: string;
  raw_base_template?: string;
  has_repo_token: boolean;
  configured: boolean;
}

export interface TestResult {
  ok: boolean;
  name: string;
  categories: number;
  profiles: number;
  sha: string;
}

export interface ConfigItem {
  ref: string;
  id: string;
  name: string;
  type: string;
  version?: string;
  depends_on?: string[];
  secrets?: string[];
}

export interface ConfigCategory {
  id: string;
  name: string;
  order: number;
  items: ConfigItem[];
}

export interface Config {
  name: string;
  sha: string;
  stale: boolean;
  fetched_at?: string;
  categories: ConfigCategory[];
  profiles: { id: string; name: string }[];
  warnings?: string[];
}

export interface PlanStep {
  ref: string;
  name: string;
  type: string;
  version?: string;
  implicit?: boolean;
}

export interface Plan {
  profile: string;
  arch: string;
  config_sha: string;
  stale: boolean;
  steps: PlanStep[];
  warnings?: string[];
  secrets?: string[];
}

export type RunStatus =
  | "pending" | "running" | "success" | "failed" | "skipped" | "blocked" | "cancelled";

export interface RunSummary {
  id: string;
  profile: string;
  config_sha: string;
  status: RunStatus;
  started_at: string;
  finished_at?: string;
}

export interface RunStep {
  id: string;
  item_ref: string;
  name: string;
  status: RunStatus;
  exit_code: number;
  started_at?: string;
  finished_at?: string;
}

export interface RunDetail extends RunSummary {
  steps: RunStep[];
}

// ApiEvent is one SSE frame. `type` is "step" | "log" | "run".
export interface ApiEvent {
  type: "step" | "log" | "run";
  run_id: string;
  step_id?: string;
  status?: string;
  stream?: string;
  line?: string;
  ts: string;
}
