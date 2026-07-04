# Cross-browser Compatibility — NEX-672

## 1. Browser Support Matrix
| Browser | Min Version | Test |
|---|---|---|
| Chrome | N-2 | HTTP redirect behavior browser-independent |
| Safari | N-2 | HTTP redirect behavior browser-independent |
| Firefox | ESR | HTTP redirect behavior browser-independent |
| Edge | N-2 | HTTP redirect behavior browser-independent |

## 2. Mobile Browsers
| Browser | OS | Test |
|---|---|---|
| Safari | iOS 15+ | 기존 responsive QA 재검증 대상 |
| Chrome | Android 10+ | 기존 responsive QA 재검증 대상 |

## 3. Feature Detection
- 신규 browser feature 없음.

## 4. Known Issues
- `/chat`는 독립 page가 아니라 workspace dashboard overlay다. canonical route는 `/{slug}/issues`.

## 5. Test Tools
- `curl` redirect 검증.
- `vitest` proxy unit test.
