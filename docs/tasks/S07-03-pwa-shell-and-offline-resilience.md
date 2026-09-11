# S07-03 — Configure Nuxt 3 PWA module, service worker, and offline shell caching

**Status:** Not started  
**Sprint:** 07 — MVP experience, operations, and validation  
**Source:** PRD §74; technical plan §§2, 11; OPENCODE.md  
**Depends on:** S07-01

## What to do

Configure `@vite-pwa/nuxt` in `frontend/nuxt.config.ts` to provide Progressive Web App capabilities including web application manifest, custom app icons, offline app-shell caching, and service worker lifecycle management. Implement read-only offline fallback caching for last-known squad, league standings, and club financial state when connectivity is lost.

## Acceptance criteria

- PWA web application manifest is served with complete branding assets, theme colors, display mode `standalone`, and installability prompts.
- Service worker precaches essential static assets and app shell for immediate loading.
- When offline, the app gracefully presents read-only cached views of squad, standings, and dashboard with an explicit offline indicator bar.
- Re-establishing network connection automatically reconnects the single WebSocket stream (`useSocket`) and synchronizes client state.
- Lighthouse PWA audit passes core installability and offline readiness checks.

## Delivery evidence

- Pending.
