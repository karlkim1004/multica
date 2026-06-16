"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";

interface LlmLimitStatus {
  five_hour_pct: number;
  seven_day_pct: number;
  sonnet_pct: number;
  gpt_five_hour_pct: number;
  gpt_seven_day_pct: number;
}

async function fetchLlmLimitStatus(): Promise<LlmLimitStatus> {
  const response = await fetch("/api/dashboard/llm-limit-status", {
    cache: "no-store",
    credentials: "include",
  });
  if (!response.ok) {
    throw new Error("Failed to load LLM limit status");
  }
  return response.json() as Promise<LlmLimitStatus>;
}

function remainingFromUsage(value: number | undefined): number {
  if (!Number.isFinite(value)) return 100;
  return Math.max(0, Math.min(100, 100 - Math.round(value ?? 0)));
}

function limitingRemaining(...usageValues: Array<number | undefined>): number {
  return Math.min(...usageValues.map(remainingFromUsage));
}

export function LlmRemainingBadge({ className }: { className?: string }) {
  const { data } = useQuery({
    queryKey: ["chat-llm-limit-status"],
    queryFn: fetchLlmLimitStatus,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  const claudeRemaining = data
    ? limitingRemaining(data.five_hour_pct, data.seven_day_pct, data.sonnet_pct)
    : null;
  const gptRemaining = data
    ? limitingRemaining(data.gpt_five_hour_pct, data.gpt_seven_day_pct)
    : null;
  const fiveHourRemaining = remainingFromUsage(data?.five_hour_pct);

  if (!data) return null;

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      className={cn(
        "hidden min-w-44 flex-col gap-1 rounded-md border px-2 py-1 text-[11px] text-muted-foreground sm:flex",
        className,
      )}
      aria-label="채팅 LLM 잔량"
    >
      <div className="flex items-center gap-1.5">
        <span data-acceptance="chat-claude-token-remaining-badge">Claude 잔량 {claudeRemaining}%</span>
        <span className="text-border">·</span>
        <span data-acceptance="chat-gpt-token-remaining-badge">GPT 잔량 {gptRemaining}%</span>
      </div>
      <div data-acceptance="chat-five-hour-remaining-gauge" className="w-full">
        <div className="mb-0.5 flex items-center justify-between">
          <span>5h 잔량</span>
          <span className="tabular-nums">{fiveHourRemaining}%</span>
        </div>
        <div className="h-1.5 overflow-hidden rounded-full bg-muted">
          <div className="h-full rounded-full bg-brand" style={{ width: `${fiveHourRemaining}%` }} />
        </div>
      </div>
    </div>
  );
}
