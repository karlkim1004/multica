# Client-side Security — NEX-672

## 1. XSS 방어
- HTML injection, `innerHTML`, `dangerouslySetInnerHTML` 변경 없음.

## 2. Content Security Policy
- 변경 없음.

## 3. SubResource Integrity
- 변경 없음.

## 4. Cookie & Storage
- 기존 `multica_logged_in`, `last_workspace_slug` cookie를 읽는 proxy behavior만 확장.
- 세션 없는 `/chat`는 `/login`으로 redirect한다.

## 5. CSRF
- 변경 없음.

## 6. Trusted Types
- 변경 없음.

## 7. Dependency Audit
- 신규 dependency 없음.

## 8. OWASP Top 10 Client-side
- route redirect target은 same-origin workspace route만 생성한다.
- target path는 고정 map 또는 기존 pathname이며 external redirect를 만들지 않는다.
