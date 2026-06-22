# State Management — NEX-580

## State Categorization
| State | Type | Storage | Lifetime |
|---|---|---|---|
| STT support | Local | `ChatInput` state | component |
| Listening status | Local | `ChatInput` state | active recognition |
| Voice output toggle | Local | `ChatWindow` state | mounted chat window |
| Chat messages/tasks | Server | TanStack Query | existing cache |

## Race Defense
- STT transcript uses existing draft restore path so Tiptap content remounts predictably.
- TTS speaks only a newly observed assistant message id.
- Toggle OFF cancels current speech before disabling.
