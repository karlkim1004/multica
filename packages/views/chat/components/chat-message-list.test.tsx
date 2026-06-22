import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { ChatMessage } from "@multica/core/types";
import { ChatMessageList } from "./chat-message-list";

const { copyText } = vi.hoisted(() => ({
  copyText: vi.fn(),
}));

vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    components,
  }: {
    data: ChatMessage[];
    itemContent: (_index: number, item: ChatMessage) => ReactNode;
    components?: {
      Header?: () => ReactNode;
      Footer?: () => ReactNode;
    };
  }) => (
    <div>
      {components?.Header?.()}
      {data.map((item, index) => (
        <div key={item.id}>{itemContent(index, item)}</div>
      ))}
      {components?.Footer?.()}
    </div>
  ),
}));

vi.mock("@multica/views/common/markdown", () => ({
  Markdown: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("../../issues/components/comment-card", () => ({
  AttachmentList: () => null,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: unknown, params?: Record<string, string>) => {
      if (params?.elapsed) return `Replied in ${params.elapsed}`;
      const source = String(selector);
      if (source.includes("copied_toast")) return "Copied";
      if (source.includes("copy_action")) return "Copy";
      return "Copy";
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

const message = (
  overrides: Partial<ChatMessage> & Pick<ChatMessage, "id" | "role" | "content" | "created_at">,
): ChatMessage => ({
  chat_session_id: "session-1",
  task_id: null,
  ...overrides,
});

function renderMessages(messages: ChatMessage[]) {
  render(
    <ChatMessageList
      messages={messages}
      pendingTask={null}
      availability="online"
    />,
  );
}

describe("ChatMessageList timestamps and copy action", () => {
  beforeEach(() => {
    copyText.mockReset();
    copyText.mockResolvedValue(true);
  });

  it("renders KST timestamps for user and assistant messages", () => {
    renderMessages([
      message({
        id: "user-1",
        role: "user",
        content: "대표님 메시지",
        created_at: "2026-06-06T08:12:43Z",
      }),
      message({
        id: "assistant-1",
        role: "assistant",
        content: "아이유 답변",
        created_at: "2026-06-06T08:12:48Z",
        elapsed_ms: 5000,
      }),
    ]);

    expect(screen.getByText("2026-06-06 17:12:43")).toBeInTheDocument();
    expect(screen.getByText("2026-06-06 17:12:48")).toBeInTheDocument();
    expect(screen.getByText("Replied in 5s")).toBeInTheDocument();
  });

  it("copies a user message's original markdown, including links and code blocks", async () => {
    const content = [
      "Please copy https://example.com/path?a=1",
      "",
      "```bash",
      "pnpm test",
      "```",
    ].join("\n");

    renderMessages([
      message({
        id: "user-1",
        role: "user",
        content,
        created_at: "2026-06-22T00:00:00Z",
      }),
    ]);

    await userEvent.click(screen.getByRole("button", { name: "Copy" }));

    expect(copyText).toHaveBeenCalledWith(content);
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });
});
