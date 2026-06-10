# Trading System Web

React + TypeScript operator console for the Go trading backend.

```bash
nvm use
npm install
npm run dev
```

The dev server proxies `/api/*` to `http://127.0.0.1:8080`, so start the Go backend separately:

```bash
GOCACHE=/private/tmp/trading-system-gocache go run ./cmd/server
```

Open:

```text
http://127.0.0.1:5173
```
