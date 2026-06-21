import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatMessage } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

const writeText = vi.fn();

Object.defineProperty(navigator, "clipboard", {
  configurable: true,
  value: { writeText },
});

Element.prototype.scrollTo = vi.fn();

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

function message(overrides: Partial<ChatMessage>): ChatMessage {
  return {
    id: "msg-1",
    chat_session_id: "session-1",
    role: "user",
    content: "default",
    task_id: null,
    created_at: "2026-06-22T00:00:00Z",
    ...overrides,
  };
}

function renderMessages(messages: ChatMessage[]) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatMessageList
          messages={messages}
          pendingTask={null}
          availability={undefined}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("ChatMessageList copy action", () => {
  beforeEach(() => {
    writeText.mockReset();
    writeText.mockResolvedValue(undefined);
  });

  it("copies a user message's original markdown, including links and code blocks", async () => {
    const content = [
      "Please copy https://example.com/path?a=1",
      "",
      "```bash",
      "pnpm test",
      "```",
    ].join("\n");

    renderMessages([message({ id: "user-1", role: "user", content })]);

    await userEvent.click(screen.getByRole("button", { name: "Copy" }));

    expect(writeText).toHaveBeenCalledWith(content);
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });
});
