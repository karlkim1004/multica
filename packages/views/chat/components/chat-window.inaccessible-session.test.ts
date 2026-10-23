import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { clearInaccessibleActiveSession, isInaccessibleSessionError } from "./chat-window";

describe("inaccessible chat session recovery", () => {
  it("recognizes a forbidden API response", () => {
    expect(isInaccessibleSessionError(new ApiError("Forbidden", 403, "Forbidden"))).toBe(true);
    expect(isInaccessibleSessionError(new ApiError("Unavailable", 500, "Error"))).toBe(false);
    expect(isInaccessibleSessionError(new Error("Forbidden"))).toBe(false);
  });

  it("clears the active session that received 403", () => {
    const clearActiveSession = vi.fn();

    expect(clearInaccessibleActiveSession(
      new ApiError("Forbidden", 403, "Forbidden"),
      "inaccessible-session",
      "inaccessible-session",
      clearActiveSession,
    )).toBe(true);
    expect(clearActiveSession).toHaveBeenCalledOnce();
  });

  it("does not clear a newer session after a late 403", () => {
    const clearActiveSession = vi.fn();

    expect(clearInaccessibleActiveSession(
      new ApiError("Forbidden", 403, "Forbidden"),
      "old-session",
      "new-session",
      clearActiveSession,
    )).toBe(false);
    expect(clearActiveSession).not.toHaveBeenCalled();
  });
});
