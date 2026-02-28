# Frontend Improvements Plan

**Version**: 0.9.2-beta  
**Last Updated**: February 28, 2026  
**Status**: Implementation roadmap

This document outlines a phased plan to implement the frontend improvements identified during the code review, including a specific strategy to resolve the i18n freezing issue.

---

## Table of Contents

1. [Phase 0: Fix i18n Freezing (Priority)](#phase-0-fix-i18n-freezing-priority)
2. [Phase 1: Quick Wins & Foundation](#phase-1-quick-wins--foundation)
3. [Phase 2: Architecture Refactors](#phase-2-architecture-refactors)
4. [Phase 3: i18n Rollout](#phase-3-i18n-rollout)
5. [Phase 4: Performance & DX](#phase-4-performance--dx)
6. [Phase 5: Testing & Accessibility](#phase-5-testing--accessibility)

---

## Phase 0: Fix i18n Freezing (Priority)

**Problem**: The interface freezes when switching languages or when components search for translations. This typically occurs due to:

- **Full-tree re-render on `changeLanguage()`** — Every component using `useTranslation` re-renders at once.
- **Large monolithic translation files** — Single JSON with hundreds of keys; lookup and JSON traversal can block the main thread.
- **Suspense / async init** — If i18n initializes asynchronously, React may suspend and cause jank.
- **Language change in `LanguageProvider` useEffect** — Can trigger cascading updates before the app is stable.

**Solution (implement in order):**

### 0.1 Configure i18n for synchronous, non-blocking behavior

**File**: `src/i18n.ts`

```typescript
// Add to i18n.init():
{
  lng: 'en',                    // Explicit default, no "search"
  load: 'languageOnly',        // Don't load en-US, en-GB — just 'en'
  preload: ['en', 'es'],       // Preload both at init (already synchronous)
  react: {
    useSuspense: false,        // CRITICAL: Avoid Suspense-related freezes
    bindI18nStore: 'languageChanged',  // Only re-render on language change (default)
  },
}
```

### 0.2 Split translations into namespaces

**Current**: One giant `translation.json` per language (~400+ keys).

**Target**: Multiple small files loaded on demand:

```
locales/
  en/
    common.json      (~30 keys)
    auth.json        (~40 keys)
    navigation.json  (~15 keys)
    buckets.json     (~60 keys)
    users.json       (~70 keys)
    ...
  es/
    (same structure)
```

- **Why**: Smaller lookup scope per namespace. Components request only the namespace they need.
- **Config**: `i18n.init({ ns: ['common'], defaultNS: 'common' })` and load other namespaces lazily with `i18n.loadNamespaces(['buckets'])` when entering a page.

### 0.3 Defer language change to avoid initial storm

**File**: `src/contexts/LanguageContext.tsx`

- Remove the `useEffect` that calls `i18n.changeLanguage` on mount if `i18n.language` differs. That can cause a re-render storm before first paint.
- Set `lng` and `fallbackLng` explicitly in `i18n.init` so there is no "detection" jank on load.
- When user changes language: debounce or use `requestAnimationFrame` before calling `i18n.changeLanguage()` so the UI commits the click before the heavy update.

### 0.4 Use namespaced `t()` to limit re-renders

- Prefer `useTranslation('common')` or `useTranslation('buckets')` instead of `useTranslation()` (which loads all namespaces).
- For components that only need a few keys, consider passing translated strings as props from a parent that owns the namespace — reduces subscribers to language changes.

**Estimated effort**: 2–4 hours. Validate with language switch on Login, Dashboard, and a heavy page (e.g. Buckets).

---

## Phase 1: Quick Wins & Foundation

**Goal**: Low-risk improvements that unblock later phases.

### 1.1 `useBasePath` hook

**Create**: `src/hooks/useBasePath.ts`

```typescript
export function useBasePath(): string {
  return (typeof window !== 'undefined' ? (window.BASE_PATH || '/') : '/').replace(/\/$/, '');
}
```

- Replace duplicated `basePath` logic in `AppLayout`, `AboutPage`, and `App.tsx`.
- **Estimated**: 30 min.

### 1.2 Centralize API error handling

**File**: `src/lib/api.ts`

- Map common S3/API error codes (`QuotaExceeded`, `AccessDenied`, `NoSuchBucket`, etc.) to user-friendly messages in the response interceptor.
- Ensure 401 triggers logout in one place (already partially done).
- **Estimated**: 1 hour.

### 1.3 Add `OPERATIONS.md` link in docs section

- Already done in previous sessions.

---

## Phase 2: Architecture Refactors

**Goal**: Improve maintainability without breaking behavior.

### 2.1 Extract `AppLayout` subcomponents

**Create**:

- `src/components/layout/SidebarNav.tsx` — Navigation structure and expand logic.
- `src/components/layout/TopBar.tsx` — User menu, notifications, theme toggle.
- `src/components/layout/MaintenanceBanner.tsx` — Read-only banner.

- **Benefit**: Easier testing, smaller file sizes, clearer responsibilities.
- **Estimated**: 2–3 hours.

### 2.2 API config flexibility

**File**: `src/lib/api.ts`

- Derive S3 base URL from `ServerConfig` or `window` when available, instead of hardcoding `:8080`.
- **Estimated**: 1 hour.

---

## Phase 3: i18n Rollout

**Goal**: Enable EN/ES across the app after Phase 0 fixes.

### 3.1 Migrate high-traffic, low-complexity screens first

**Order**:

1. **Login** — Few keys, critical path.
2. **Navigation** — Reuse `navigation.*` keys from current `translation.json`.
3. **Dashboard** — Reuse `dashboard.*`.
4. **Buckets list** — Reuse `buckets.*`.
5. **Users list** — Reuse `users.*`.

### 3.2 Patterns to avoid freezes

- **Do NOT** call `t()` inside `.map()` for large lists without memoization. Prefer translating headers/empty states outside the loop.
- **Do** use `Trans` only when you need embedded components (e.g. `<strong>`) — plain `t()` is faster.
- **Do** keep page-level components as the main `useTranslation` consumer; pass strings to presentational children as props when possible.

### 3.3 Validation

- Toggle EN ↔ ES on each migrated page.
- Measure time from click to painted UI (should feel instant; target < 100 ms perceived).

**Estimated**: 4–8 hours depending on number of screens.

---

## Phase 4: Performance & DX

### 4.1 Code splitting with `React.lazy`

**File**: `src/App.tsx`

- Wrap heavy pages: `ClusterOverview`, `ClusterMigrations`, `Metrics`, `About`, `BucketCreate` in `React.lazy(() => import(...))`.
- Add `<Suspense fallback={<Loading />}>` around routes.
- **Estimated**: 1 hour.

### 4.2 React Query tuning

- Review `refetchInterval` and `staleTime` for dashboard, metrics, cluster pages.
- Use `enabled: isPageVisible` or `refetchOnWindowFocus: false` where appropriate.
- **Estimated**: 1–2 hours.

---

## Phase 5: Testing & Accessibility

### 5.1 Tests for layout and i18n

- `AppLayout.test.tsx` — Nav visibility by role, maintenance banner, notification count.
- `LanguageContext.test.tsx` — Language change updates context.
- **Estimated**: 2 hours.

### 5.2 Accessibility

- Add `aria-current="page"` on active nav item.
- Add `aria-label` on icon-only buttons (notifications, theme, menu).
- **Estimated**: 1–2 hours.

---

## Implementation Order Summary

| Phase | Tasks | Priority | Effort |
|-------|-------|----------|--------|
| **0** | Fix i18n freezing | Critical | 2–4 h |
| **1** | useBasePath, API errors | High | ~1.5 h |
| **2** | AppLayout extraction, S3 URL | Medium | ~4 h |
| **3** | i18n rollout (EN/ES) | High | 4–8 h |
| **4** | Code split, Query tuning | Medium | ~2 h |
| **5** | Tests, a11y | Low | ~3 h |

**Total estimated**: ~17–23 hours.

---

## References

- [i18next react options](https://react.i18next.com/latest/usetranslation-hook#options)
- [i18next namespaces](https://www.i18next.com/how-to/add-or-load-translations#namespaces)
- Current translation files: `src/locales/en/translation.json`, `src/locales/es/translation.json`
