# frontend

The Angular 22 workspace of cowork: the UI the frontend container serves with nginx, which
also proxies `/api/` to the backend. Everything about building, testing and the conventions is
in the repository root — [docs/developer/](../docs/developer/README.md) — and the commands are
Makefile targets:

```bash
make frontend-install        # npm ci, when frontend/package-lock.json changed
make dev                     # from the root: the whole stack with demo data, the UI on :4200 (hack/dev.sh)
make frontend-serve          # ng serve on :4200, /api proxied to the backend on :8080 (make run)
make frontend-generate       # the API client in src/app/api from backend/api/openapi.gen.json
make frontend-lint           # ng lint (angular-eslint)
make frontend-test           # ng test, vitest on jsdom, once
make frontend-test-coverage  # with coverage under frontend/coverage/
make frontend-build          # production build → frontend/dist/frontend/browser
make docker-build-frontend   # the nginx image from frontend/Containerfile
```

`ng` commands work as usual from this directory; `proxy.conf.mjs` is wired into `ng serve`
in `angular.json`. How the UI is built is [docs/developer/frontend.md](../docs/developer/frontend.md). [nginx/default.conf.template](nginx/default.conf.template) is the
configuration the container renders at start.
