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
          five_hour_reset_label: "(수) 오후 9:30에 재설정",
          seven_day_reset_label: "(금) 오전 12:00에 재설정",
          sonnet_reset_label: "(토) 오전 9:00에 재설정",
          gpt_five_reset_label: "resets 10:45 PM",
          gpt_seven_reset_label: "resets May 17",
        }),
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders a compact single-line provider-specific quota summary", async () => {
    renderBadge();

    await waitFor(() => {
      expect(screen.getByText("C:5h75%/7d40%")).toBeInTheDocument();
      expect(screen.getByText("G:5h90%/7d70%")).toBeInTheDocument();
    });
    expect(document.querySelector("[data-testid='chat-llm-gauge-claude']")).toHaveTextContent("C:5h75%/7d40%");
    expect(document.querySelector("[data-testid='chat-llm-gauge-gpt']")).toHaveTextContent("G:5h90%/7d70%");
    expect(
      screen.getByLabelText(
        "채팅 LLM 잔량: Claude 5시간 75%, 리셋 (수) 오후 9:30, Claude 1주 40%, 리셋 (금) 오전 12:00, GPT 5시간 90%, 리셋 10:45 PM, GPT 1주 70%, 리셋 May 17",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("채팅 LLM 잔량 새로고침"));
    expect(document.querySelector("[data-acceptance='chat-llm-gauge-manual-refresh']")).toBeTruthy();
    expect(document.querySelector("[data-acceptance='chat-token-remaining-badge']")).toHaveTextContent(
      "C:5h75%/7d40%G:5h90%/7d70%",
    );
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/dashboard/llm-limit-status",
      expect.objectContaining({ cache: "no-store" }),
    );
  });
});
