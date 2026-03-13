# Frontend Workspace

This React app is the dashboard shell for the monitor backend.

## Structure

- `src/App.tsx`: router shell and lazy route loading
- `src/pages/`: route entrypoints
- `src/features/`: heavier page implementations and domain UI
- `src/api/`: grouped API domains with a small `src/api.ts` compatibility facade

## Commands

```bash
pnpm --dir web install
pnpm --dir web build
pnpm --dir web lint
pnpm --dir web dev
```

## Notes

- Vite proxies API traffic to `http://127.0.0.1:8765` unless `VITE_API_BASE_URL` is set.
- Observer calls use `http://127.0.0.1:8776` by default and persist the override in local storage.
- Route-level lazy loading is intentional to keep the initial dashboard bundle smaller.
