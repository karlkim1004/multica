# Accessibility Compliance — NEX-580

## Applied
- Mic button has `aria-label`, `aria-pressed`, disabled state for unsupported/no-agent/archived cases.
- Speaker toggle has `aria-label`, `aria-pressed`, and disabled state when `speechSynthesis` is unavailable.
- Existing keyboard/focus behavior remains button-based and tooltip-backed.

## WCAG Notes
- No color-only state: labels and disabled states are exposed.
- Touch targets use existing icon button sizes.
- Automated axe/Lighthouse was not run due authenticated chat access constraint.
