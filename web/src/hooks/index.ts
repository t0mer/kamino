import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../api/client";
import { handleError } from "../api/queryClient";
import { useApiToken } from "./useApi";
import type {
  Config, Plan, RunDetail, RunSummary, Settings, SystemInfo, TestResult,
} from "../api/types";

const LIVE = 60_000;

export function useSystem() {
  const token = useApiToken();
  return useQuery({
    queryKey: ["system"],
    queryFn: () => apiFetch<SystemInfo>("/api/v1/system", { token }),
    refetchInterval: LIVE,
    throwOnError: (e) => { handleError(e); return false; },
  });
}

export function useSettings() {
  const token = useApiToken();
  return useQuery({
    queryKey: ["settings"],
    queryFn: () => apiFetch<Settings>("/api/v1/settings", { token }),
    throwOnError: (e) => { handleError(e); return false; },
  });
}

export function useConfig() {
  const token = useApiToken();
  return useQuery({
    queryKey: ["config"],
    queryFn: () => apiFetch<Config>("/api/v1/config", { token }),
    refetchInterval: LIVE,
    throwOnError: (e) => { handleError(e); return false; },
  });
}

export function useRuns() {
  const token = useApiToken();
  return useQuery({
    queryKey: ["runs"],
    queryFn: () => apiFetch<RunSummary[]>("/api/v1/runs", { token }),
    refetchInterval: LIVE,
    throwOnError: (e) => { handleError(e); return false; },
  });
}

export function useRun(id: string) {
  const token = useApiToken();
  return useQuery({
    queryKey: ["run", id],
    queryFn: () => apiFetch<RunDetail>(`/api/v1/runs/${id}`, { token }),
    throwOnError: (e) => { handleError(e); return false; },
  });
}

export function useTestSettings() {
  const token = useApiToken();
  return useMutation({
    mutationFn: (body: Partial<Settings> & { repo_token?: string | null }) =>
      apiFetch<TestResult>("/api/v1/settings/test", { method: "POST", body, token }),
    onError: handleError,
  });
}

export function useSaveSettings() {
  const token = useApiToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<Settings> & { repo_token?: string | null }) =>
      apiFetch<Settings>("/api/v1/settings", { method: "PUT", body, token }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); qc.invalidateQueries({ queryKey: ["config"] }); },
    onError: handleError,
  });
}

export function usePlan() {
  const token = useApiToken();
  return useMutation({
    mutationFn: (body: { profile: string; arch?: string }) =>
      apiFetch<Plan>("/api/v1/plan", { method: "POST", body, token }),
    onError: handleError,
  });
}

export function useStartRun() {
  const token = useApiToken();
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { profile: string; arch?: string; secrets?: Record<string, string>; continue_on_error?: boolean }) =>
      apiFetch<{ run_id: string }>("/api/v1/runs", { method: "POST", body, token }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["runs"] }),
    onError: handleError,
  });
}

export function useCancelRun() {
  const token = useApiToken();
  return useMutation({
    mutationFn: (id: string) =>
      apiFetch<{ run_id: string }>(`/api/v1/runs/${id}/cancel`, { method: "POST", token }),
    onError: handleError,
  });
}
