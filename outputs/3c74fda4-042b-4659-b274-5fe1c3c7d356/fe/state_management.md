# State Management — NEX-672

## 1. State Categorization
| State | Type | Storage | Lifetime |
|---|---|---|---|
| Session marker | Cookie | `multica_logged_in` | Browser session |
| Last workspace | Cookie | `last_workspace_slug` | 1 year |
| Chat open state | Global UI | existing chat store | Persistent |
| URL route | URL | Next request pathname | Request |

## 2. Server State
- 서버 state query/cache 변경 없음.
- redirect 후 기존 workspace-scoped pages가 기존 TanStack Query flow를 사용한다.

## 3. Form State
- 변경 없음.

## 4. Cache Invalidation
- 변경 없음.

## 5. Race Condition 방어
- redirect는 request-time cookie만 읽는다.
- `last_workspace_slug`가 없으면 기존 정책대로 `/`로 보내 workspace resolution flow를 사용한다.
