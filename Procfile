pgrokd: task pgrokd --watch
web: OVERMIND_PORT=5173 BACKEND_URL=http://localhost:3320 cd pgrokd/web && pnpm install && pnpm run dev
oidc-server: go run ./integration-tests/oidc-server
