"use client";

import { useEffect, useState } from "react";
import { useChatStore, type ChatMode } from "@multica/core/chat";

/** Below this viewport width, docked-right always falls back to floating —
 * there isn't enough room to both pin a panel and keep content usable. */
export const CHAT_DOCK_MIN_VIEWPORT_W = 768;

/** Whether the viewport is wide enough to offer docked-right at all. */
export function useCanDockChat(): boolean {
  const [canDock, setCanDock] = useState(
    () => typeof window !== "undefined" && window.innerWidth >= CHAT_DOCK_MIN_VIEWPORT_W,
  );

  useEffect(() => {
    const mql = window.matchMedia(`(min-width: ${CHAT_DOCK_MIN_VIEWPORT_W}px)`);
    const update = () => setCanDock(mql.matches);
    update();
    mql.addEventListener("change", update);
    return () => mql.removeEventListener("change", update);
  }, []);

  return canDock;
}

/**
 * Resolves the persisted chat-mode preference against viewport width. Narrow
 * screens always render floating regardless of the stored preference — the
 * preference itself is left untouched so it resumes once the window widens.
 */
export function useEffectiveChatMode(): ChatMode {
  const chatMode = useChatStore((s) => s.chatMode);
  const canDock = useCanDockChat();
  return canDock ? chatMode : "floating";
}

/**
 * Horizontal space the docked chat panel currently reserves, so page content
 * can pad itself and avoid being covered. 0 when floating, closed, or the
 * viewport is too narrow to dock.
 */
export function useChatDockOffset(): number {
  const isOpen = useChatStore((s) => s.isOpen);
  const chatWidth = useChatStore((s) => s.chatWidth);
  const mode = useEffectiveChatMode();
  return isOpen && mode === "docked-right" ? chatWidth : 0;
}
