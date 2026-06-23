import { describe, expect, it } from "vitest";
import { formatChatTimestamp } from "./format";

describe("formatChatTimestamp", () => {
  it("formats UTC chat message timestamps as KST absolute time", () => {
    expect(formatChatTimestamp("2026-06-06T08:12:43Z")).toBe("2026-06-06 17:12:43");
  });

  it("uses the provided fallback when a timestamp is missing or invalid", () => {
    const fallback = new Date("2026-06-06T08:12:43Z");

    expect(formatChatTimestamp(undefined, fallback)).toBe("2026-06-06 17:12:43");
    expect(formatChatTimestamp("not-a-date", fallback)).toBe("2026-06-06 17:12:43");
  });
});
