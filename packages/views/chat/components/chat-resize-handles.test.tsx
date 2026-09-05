import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { ChatResizeHandles } from "./chat-resize-handles";

describe("ChatResizeHandles", () => {
  it("renders left, top, and corner handles in floating mode", () => {
    const { container } = render(<ChatResizeHandles onDragStart={vi.fn()} mode="floating" />);
    expect(container.querySelectorAll("[aria-hidden]")).toHaveLength(3);
    expect(container.querySelector(".cursor-col-resize")).not.toBeNull();
    expect(container.querySelector(".cursor-row-resize")).not.toBeNull();
    expect(container.querySelector(".cursor-nw-resize")).not.toBeNull();
  });

  it("renders only the left (width) handle when docked-right", () => {
    const { container } = render(<ChatResizeHandles onDragStart={vi.fn()} mode="docked-right" />);
    const handles = container.querySelectorAll("[aria-hidden]");
    expect(handles).toHaveLength(1);
    expect(container.querySelector(".cursor-col-resize")).not.toBeNull();
    expect(container.querySelector(".cursor-row-resize")).toBeNull();
    expect(container.querySelector(".cursor-nw-resize")).toBeNull();
  });

  it("defaults to floating mode when no mode prop is given", () => {
    const { container } = render(<ChatResizeHandles onDragStart={vi.fn()} />);
    expect(container.querySelectorAll("[aria-hidden]")).toHaveLength(3);
  });
});
