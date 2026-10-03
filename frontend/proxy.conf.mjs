// The Angular dev server's proxy (make frontend-serve, make dev): /api, /auth, /healthz and
// /readyz go to the backend on :8080, the same shape nginx has in the container. It holds no
// credential: the browser logs in like on an installation, and the session cookie travels in
// both directions (docs/adr/0038 D2).
const backend = process.env['COWORK_DEV_BACKEND'] ?? 'http://localhost:8080';

// Node keeps response headers until the first body byte, and an event stream may send none for
// twenty seconds; the browser would not see the stream open until the first heartbeat. The proxy
// sets the headers in the same tick it emits this event, so they are flushed on the next one.
function configure(proxy) {
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
