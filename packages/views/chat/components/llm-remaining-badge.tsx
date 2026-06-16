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

  const ariaLabel = [
    `채팅 LLM 잔량: Claude 5시간 ${claudeFiveHourRemaining}%`,
    `Claude 7일 ${claudeSevenDayRemaining}%`,
    `GPT 5시간 ${gptFiveHourRemaining}%`,
    `GPT 7일 ${gptSevenDayRemaining}%`,
  ].join(", ");

  return (
    <div
      data-acceptance="chat-token-remaining-badge"
      className={cn(
        "hidden min-w-[13.5rem] items-stretch gap-1.5 rounded-md border px-2 py-1 text-[11px] text-muted-foreground sm:flex",
        className,
      )}
      aria-label={ariaLabel}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <ProviderRemainingGroup
          provider="Claude"
          dataAcceptance="chat-claude-token-remaining-badge"
          fiveHour={claudeFiveHourRemaining}
          sevenDay={claudeSevenDayRemaining}
        />
        <ProviderRemainingGroup
          provider="GPT"
          dataAcceptance="chat-gpt-token-remaining-badge"
          fiveHour={gptFiveHourRemaining}
          sevenDay={gptSevenDayRemaining}
        />
      </div>
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        className="self-center"
        data-acceptance="chat-llm-gauge-manual-refresh"
        aria-label="채팅 LLM 잔량 새로고침"
        onClick={() => void refetch()}
      >
        <RefreshCw className={cn("h-3 w-3", isFetching && "animate-spin")} />
      </Button>
    </div>
  );
}

function ProviderRemainingGroup({
  provider,
  dataAcceptance,
  fiveHour,
  sevenDay,
}: {
  provider: "Claude" | "GPT";
  dataAcceptance: string;
  fiveHour: number;
  sevenDay: number;
}) {
  const prefix = provider === "Claude" ? "claude" : "gpt";
  return (
    <div
      data-acceptance={dataAcceptance}
      className="flex min-w-0 items-center justify-between gap-2 rounded border bg-background/40 px-1.5 py-0.5"
    >
      <span className="font-medium text-foreground">{provider}</span>
      <RemainingRow provider={provider} period="5h" value={fiveHour} testId={`chat-llm-gauge-${prefix}-5h`} />
      <RemainingRow provider={provider} period="7d" value={sevenDay} testId={`chat-llm-gauge-${prefix}-7d`} />
    </div>
  );
}

function RemainingRow({
  provider,
  period,
  value,
  testId,
}: {
  provider: "Claude" | "GPT";
  period: "5h" | "7d";
  value: number;
  testId: string;
}) {
  return (
    <div
      data-testid={testId}
      aria-label={`${provider} ${period} 잔량 ${value}%`}
      className="flex items-center gap-0.5 whitespace-nowrap leading-4"
    >
      <span>{period}</span>
      <span className="tabular-nums">{value}%</span>
    </div>
  );
}
