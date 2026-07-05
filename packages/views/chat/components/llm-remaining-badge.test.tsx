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

  it("renders provider-specific 5h and 7d remaining values from the token snapshot API", async () => {
    renderBadge();

    await waitFor(() => {
      expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h 90%/7d 70%");
    });
    expect(document.querySelector("[data-acceptance='chat-claude-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "Claude 5시간 잔량 75%, 리셋 (수) 오후 9:30; Claude 1주 잔량 40%, 리셋 (금) 오전 12:00",
    );
    expect(document.querySelector("[data-acceptance='chat-gpt-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "GPT 5시간 잔량 90%, 리셋 10:45 PM; GPT 1주 잔량 70%, 리셋 May 17",
    );
    expect(
      screen.getByLabelText(
        "채팅 LLM 잔량: Claude 5시간 75%, 리셋 (수) 오후 9:30, Claude 1주 40%, 리셋 (금) 오전 12:00, GPT 5시간 90%, 리셋 10:45 PM, GPT 1주 70%, 리셋 May 17",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText("채팅 LLM 잔량 새로고침"));
    expect(document.querySelector("[data-acceptance='chat-llm-gauge-manual-refresh']")).toBeTruthy();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/dashboard/llm-limit-status",
      expect.objectContaining({ cache: "no-store" }),
    );
  });

  it("keeps the live acceptance token gauge as a single compact line", async () => {
    renderBadge();

    const compactGauge = await screen.findByTestId("chat-llm-gauge-compact");

    expect(compactGauge).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h 90%/7d 70%");
    expect(compactGauge).toHaveClass("whitespace-nowrap");
    expect(compactGauge).toHaveAttribute("data-acceptance", "chat-token-gauge");
  });

  it("does not turn unavailable GPT limits into fake 100 percent remaining", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          five_hour_pct: 25,
          seven_day_pct: 60,
          sonnet_pct: 27,
          gpt_five_hour_pct: null,
          gpt_seven_day_pct: null,
          five_hour_reset_label: "(수) 오후 9:30에 재설정",
          seven_day_reset_label: "(금) 오전 12:00에 재설정",
          sonnet_reset_label: "(토) 오전 9:00에 재설정",
          gpt_five_reset_label: "—",
          gpt_seven_reset_label: "—",
        }),
      })),
    );

    renderBadge();

    await waitFor(() => {
      expect(screen.getByTestId("chat-llm-gauge-compact")).toHaveTextContent("C: 5h 75%/7d 40% · G: 5h --/7d --");
    });
    expect(document.querySelector("[data-acceptance='chat-gpt-token-remaining-badge']")).toHaveAttribute(
      "aria-label",
      "GPT 5시간 잔량 확인 불가, 리셋 -; GPT 1주 잔량 확인 불가, 리셋 -",
    );
    expect(screen.queryByText("100%")).not.toBeInTheDocument();
  });
});
