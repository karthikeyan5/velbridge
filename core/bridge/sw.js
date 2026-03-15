// VelBridge Service Worker — intercepts outbound requests and routes through proxy
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  if (url.hostname !== location.hostname) {
    // Route external requests through proxy
    const proxied = location.origin + '/bridge/proxy/' + url.hostname + url.pathname + url.search;
    event.respondWith(fetch(proxied, {
      method: event.request.method,
      headers: event.request.headers,
      body: event.request.body,
      redirect: 'follow'
    }));
  }
});

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
