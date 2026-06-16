import { render, screen, waitFor } from "@testing-library/react";
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

  it("renders live remaining values from the token snapshot API", async () => {
    renderBadge();

    await waitFor(() => {
      expect(screen.getByText("Claude 잔량 40%")).toBeInTheDocument();
      expect(screen.getByText("GPT 잔량 70%")).toBeInTheDocument();
      expect(screen.getByText("5h 잔량")).toBeInTheDocument();
      expect(screen.getByText("75%")).toBeInTheDocument();
    });
    expect(document.querySelector("[data-acceptance='chat-five-hour-remaining-gauge']")).toBeTruthy();
    expect(globalThis.fetch).toHaveBeenCalledWith(
      "/api/dashboard/llm-limit-status",
      expect.objectContaining({ cache: "no-store" }),
    );
  });
});
