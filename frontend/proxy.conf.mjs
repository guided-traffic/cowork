// The Angular dev server's proxy (make frontend-serve, make dev), the developer's stand-in for the
// Ingress: /api and /auth go to the backend on :8080 as the Ingress routes them on an installation
// (docs/adr/0001 D3), and so do the backend's /healthz and /readyz; the dev server serves the rest.
// It holds no credential: the browser logs in like on an installation, and the session cookie
// travels in both directions (docs/adr/0038 D2).
const backend = process.env['COWORK_DEV_BACKEND'] ?? 'http://localhost:8080';

// Node keeps response headers until the first body byte, and an event stream may send none for
// twenty seconds; the browser would not see the stream open until the first heartbeat. The proxy
// sets the headers in the same tick it emits this event, so they are flushed on the next one.
//
// A browser that aborts a request — the chat's Stop — closes only its own side, and the proxy
// keeps the request to the backend open: the backend would go on with the turn and its model.
// Ending the backend's request when the browser's side closes before the answer ended is what
// nginx does by default (proxy_ignore_client_abort off), the Ingress stand-in of local runs
// among it.
function configure(proxy) {
  proxy.on('proxyReq', (proxyReq, req, res) => {
    res.on('close', () => {
      // Whether the browser's side is done says nothing over HTTP/2, whose aborted stream counts
      // as finished; the backend's answer that has not ended does.
      if (!proxyReq.res?.complete) {
        proxyReq.destroy();
      }
    });
  });
  proxy.on('proxyRes', (proxyRes, req, res) => {
    if (String(proxyRes.headers['content-type'] ?? '').startsWith('text/event-stream')) {
      process.nextTick(() => res.flushHeaders());
    }
  });
}

const route = { target: backend, secure: false, configure };

export default {
  '/api': route,
  '/auth': route,
  '/healthz': { target: backend, secure: false },
  '/readyz': { target: backend, secure: false },
};
