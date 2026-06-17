"use client";

import { RefreshCw } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";

interface LlmLimitStatus {
  five_hour_pct: number;
  seven_day_pct: number;
  sonnet_pct: number;
  gpt_five_hour_pct: number;
  gpt_seven_day_pct: number;
  five_hour_reset_label?: string;
  seven_day_reset_label?: string;
  sonnet_reset_label?: string;
  gpt_five_reset_label?: string;
  gpt_seven_reset_label?: string;
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

function compactResetLabel(label: string | undefined): string {
  if (!label || label === "—" || label === "-") return "-";
  return label
    .replace(/^resets\s+/i, "")
    .replace(/에 재설정$/, "")
    .trim();
}

function limitingClaudeSevenDayResetLabel(data: LlmLimitStatus): string | undefined {
  const sevenDayRemaining = remainingFromUsage(data.seven_day_pct);
  const sonnetRemaining = remainingFromUsage(data.sonnet_pct);
  return sonnetRemaining < sevenDayRemaining ? data.sonnet_reset_label : data.seven_day_reset_label;
}

export function LlmRemainingBadge({ className }: { className?: string }) {
  const { data, isFetching, refetch } = useQuery({
    queryKey: ["chat-llm-limit-status"],
    queryFn: fetchLlmLimitStatus,
    refetchInterval: 60_000,
    staleTime: 30_000,
  });

  if (!data) return null;

  const claudeFiveHourRemaining = remainingFromUsage(data.five_hour_pct);
  const claudeSevenDayRemaining = limitingRemaining(data.seven_day_pct, data.sonnet_pct);
  const gptFiveHourRemaining = remainingFromUsage(data.gpt_five_hour_pct);
  const gptSevenDayRemaining = remainingFromUsage(data.gpt_seven_day_pct);
  const claudeFiveHourReset = compactResetLabel(data.five_hour_reset_label);
  const claudeSevenDayReset = compactResetLabel(limitingClaudeSevenDayResetLabel(data));
  const gptFiveHourReset = compactResetLabel(data.gpt_five_reset_label);
  const gptSevenDayReset = compactResetLabel(data.gpt_seven_reset_label);

  const ariaLabel = [
    `채팅 LLM 잔량: Claude 5시간 ${claudeFiveHourRemaining}%, 리셋 ${claudeFiveHourReset}`,
    `Claude 1주 ${claudeSevenDayRemaining}%, 리셋 ${claudeSevenDayReset}`,
    `GPT 5시간 ${gptFiveHourRemaining}%, 리셋 ${gptFiveHourReset}`,
    `GPT 1주 ${gptSevenDayRemaining}%, 리셋 ${gptSevenDayReset}`,
  ].join(", ");

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      className={cn(
        "hidden h-6 shrink-0 items-center gap-1 rounded-md border bg-background/50 px-1.5 text-[11px] leading-none text-muted-foreground sm:flex",
        className,
      )}
      aria-label={ariaLabel}
    >
      <span
        data-acceptance="chat-claude-token-remaining-badge"
        data-testid="chat-llm-gauge-claude"
        aria-label={`Claude 5시간 잔량 ${claudeFiveHourRemaining}%, Claude 1주 잔량 ${claudeSevenDayRemaining}%`}
        className="whitespace-nowrap font-medium tabular-nums text-foreground"
      >
        C:5h{claudeFiveHourRemaining}%/7d{claudeSevenDayRemaining}%
      </span>
      <span className="w-1.5" aria-hidden="true" />
      <span
        data-acceptance="chat-gpt-token-remaining-badge"
        data-testid="chat-llm-gauge-gpt"
        aria-label={`GPT 5시간 잔량 ${gptFiveHourRemaining}%, GPT 1주 잔량 ${gptSevenDayRemaining}%`}
        className="whitespace-nowrap font-medium tabular-nums text-foreground"
      >
        G:5h{gptFiveHourRemaining}%/7d{gptSevenDayRemaining}%
      </span>
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        className="ml-0.5 size-5 rounded"
        data-acceptance="chat-llm-gauge-manual-refresh"
        aria-label="채팅 LLM 잔량 새로고침"
        onClick={() => void refetch()}
      >
        <RefreshCw className={cn("h-3 w-3", isFetching && "animate-spin")} />
      </Button>
    </div>
  );
}
