import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LlmRemainingBadge } from "./llm-remaining-badge";

function renderBadge() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <LlmRemainingBadge />
    </QueryClientProvider>,
  );
}

describe("LlmRemainingBadge", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          five_hour_pct: 25,
          seven_day_pct: 60,
          sonnet_pct: 27,
          gpt_five_hour_pct: 10,
          gpt_seven_day_pct: 30,
        }),
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders provider-specific 5h and 7d remaining values from the token snapshot API", async () => {
    renderBadge();

    await waitFor(() => {
      expect(screen.getByLabelText("Claude 5h 잔량 75%")).toBeInTheDocument();
      expect(screen.getByLabelText("Claude 7d 잔량 40%")).toBeInTheDocument();
      expect(screen.getByLabelText("GPT 5h 잔량 90%")).toBeInTheDocument();
      expect(screen.getByLabelText("GPT 7d 잔량 70%")).toBeInTheDocument();
    });
    expect(document.querySelector("[data-testid='chat-llm-gauge-claude-5h']")).toHaveTextContent("Claude 5h75%");
    expect(document.querySelector("[data-testid='chat-llm-gauge-claude-7d']")).toHaveTextContent("Claude 7d40%");
    expect(document.querySelector("[data-testid='chat-llm-gauge-gpt-5h']")).toHaveTextContent("GPT 5h90%");
    expect(document.querySelector("[data-testid='chat-llm-gauge-gpt-7d']")).toHaveTextContent("GPT 7d70%");
    expect(screen.getByLabelText("채팅 LLM 잔량: Claude 5시간 75%, Claude 7일 40%, GPT 5시간 90%, GPT 7일 70%")).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("채팅 LLM 잔량 새로고침"));
    expect(document.querySelector("[data-acceptance='chat-llm-gauge-manual-refresh']")).toBeTruthy();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/dashboard/llm-limit-status",
      expect.objectContaining({ cache: "no-store" }),
    );
  });
});
