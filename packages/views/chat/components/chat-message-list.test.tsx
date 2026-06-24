import { forwardRef, useImperativeHandle } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import enChat from "../../locales/en/chat.json";
import enCommon from "../../locales/en/common.json";
import type { ChatMessage, ChatPendingTask } from "@multica/core/types";
import { ChatMessageList } from "./chat-message-list";

vi.mock("react-virtuoso", () => ({
  Virtuoso: forwardRef(function MockVirtuoso(
    {
      data,
      itemContent,
      components,
    }: {
      data: unknown[];
      itemContent: (i: number, item: unknown) => unknown;
      components?: { Header?: () => React.ReactNode; Footer?: () => React.ReactNode };
    },
    ref: React.Ref<unknown>,
  ) {
    useImperativeHandle(ref, () => ({
      scrollIntoView: vi.fn(),
      scrollToIndex: vi.fn(),
    }));
    return (
      <div data-testid="virtuoso-mock">
        {components?.Header?.()}
        {data.map((item, i) => (
          <div key={i}>{itemContent(i, item) as React.ReactElement}</div>
        ))}
        {components?.Footer?.()}
      </div>
    );
  }),
}));

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

function renderList({
  messages,
  pendingTask = null,
}: {
  messages: ChatMessage[];
  pendingTask?: ChatPendingTask | null;
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability="online"
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function renderMessages(messages: ChatMessage[], voiceOutputEnabled = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <ChatMessageList
          messages={messages}
          pendingTask={null}
          availability="online"
          voiceOutputEnabled={voiceOutputEnabled}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function message(overrides: Partial<ChatMessage>): ChatMessage {
  return {
    id: "msg-1",
    chat_session_id: "session-1",
    role: "assistant",
    content: "Done",
    task_id: null,
    created_at: "2026-06-23T13:31:02.000Z",
    attachments: [],
    failure_reason: null,
    elapsed_ms: null,
    ...overrides,
  };
}

describe("ChatMessageList timing metadata", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("renders assistant message KST timestamp with response elapsed", () => {
    renderList({
      messages: [message({ elapsed_ms: 38000 })],
    });

    expect(screen.getByText("Done")).toBeInTheDocument();
    expect(screen.getByText("Replied in 38s")).toBeInTheDocument();
    expect(screen.getByTestId("chat-message-timestamp")).toHaveTextContent(
      "2026-06-23 22:31:02",
    );
    expect(screen.getByTestId("chat-message-timestamp")).toHaveAttribute(
      "aria-label",
      "2026-06-23 22:31:02 KST",
    );
  });

  it("keeps the live progress pill visible with elapsed time", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-23T13:31:04.000Z"));

    renderList({
      messages: [],
      pendingTask: {
        task_id: "task-1",
        status: "queued",
        created_at: "2026-06-23T13:31:02.000Z",
      },
    });

    const pill = screen.getByTestId("chat-task-status-pill");
    expect(pill).toHaveAttribute("data-acceptance", "chat-response-in-progress");
    expect(pill).toHaveTextContent("Queued");
    expect(pill).toHaveTextContent("2s");
  });
});

describe("ChatMessageList voice output", () => {
  beforeEach(() => {
    vi.stubGlobal("SpeechSynthesisUtterance", vi.fn(function SpeechSynthesisUtterance(this: { text: string; lang: string; voice: SpeechSynthesisVoice | null }, text: string) {
      this.text = text;
      this.lang = "";
      this.voice = null;
    }));
    vi.stubGlobal("speechSynthesis", {
      speak: vi.fn(),
      cancel: vi.fn(),
      getVoices: vi.fn(() => [{ lang: "ko-KR", name: "Korean" }]),
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("speaks a newly received assistant message in Korean when enabled", () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrap = (msgs: ChatMessage[]) => (
      <QueryClientProvider client={queryClient}>
        <I18nProvider resources={TEST_RESOURCES} locale="en">
          <ChatMessageList messages={msgs} pendingTask={null} availability="online" voiceOutputEnabled />
        </I18nProvider>
      </QueryClientProvider>
    );
    const { rerender } = render(wrap([]));

    rerender(wrap([
      message({
        id: "assistant-1",
        role: "assistant",
        content: "대표님, 확인했습니다.",
        created_at: "2026-06-22T00:00:00Z",
      }),
    ]));

    const speak = window.speechSynthesis.speak as unknown as ReturnType<typeof vi.fn>;
    expect(speak).toHaveBeenCalledTimes(1);
    const utterance = speak.mock.calls[0]![0] as SpeechSynthesisUtterance;
    expect(utterance.text).toBe("대표님, 확인했습니다.");
    expect(utterance.lang).toBe("ko-KR");
  });

  it("does not speak assistant messages when disabled", () => {
    renderMessages([
      message({
        id: "assistant-1",
        role: "assistant",
        content: "조용히 표시만 합니다.",
        created_at: "2026-06-22T00:00:00Z",
      }),
    ]);

    expect(window.speechSynthesis.speak).not.toHaveBeenCalled();
  });
});
