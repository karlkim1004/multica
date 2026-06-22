# Component Architecture — NEX-580

## Reused Items
- NEX-566: Web Speech API `SpeechRecognition`/`webkitSpeechRecognition`, `ko-KR`, transcript -> input draft, unsupported/permission graceful fallback.
- NEX-569: `speechSynthesis`, `SpeechSynthesisUtterance`, `ko-KR` voice preference, toggle OFF means no `speak()` call.
- NEX-575: base branch `origin/agent/fe/nex-575-restore-nex562-kst-copy` / commit `a1112581`.

## Components
| Component | Level | Change |
|---|---|---|
| `ChatInput` | molecule | Adds mic button and speech status inside existing action row. |
| `ChatMessageList` | organism | Speaks latest assistant message when `voiceOutputEnabled=true`. |
| `ChatWindow` | organism | Adds speaker toggle beside refresh without touching session/header controls. |

## State Boundary
- Local: voice support/listening/status in `ChatInput`; voice output toggle in `ChatWindow`.
- Server: unchanged chat session/message/task query flow.
- URL/global state: unchanged.

## Re-render Strategy
- Speech support detection runs after mount to avoid SSR hydration mismatch.
- TTS stores last spoken assistant id in a ref to prevent repeat speech on re-render.
