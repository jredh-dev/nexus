import { defineMiddleware } from 'astro:middleware';

// Proxy Connect RPC calls to the portal backend.
// In production, PORTAL_URL points to the portal Cloud Run service.
// This avoids CORS issues — browser calls same-origin '/', SSR proxies to portal.
const portalUrl = import.meta.env.PORTAL_URL || process.env.PORTAL_URL || 'http://localhost:8080';

export const onRequest = defineMiddleware(async (context, next) => {
  const { pathname } = context.url;

  // Proxy form POSTs (login/signup) to portal backend
  const isProxiedPost = context.request.method === 'POST' &&
    (pathname === '/login' || pathname === '/signup');

  // Proxy Connect RPC, legacy API calls, and auth form POSTs to portal
  if (pathname.startsWith('/portal.v1.') || pathname.startsWith('/api/') || isProxiedPost) {
    const target = new URL(pathname + context.url.search, portalUrl);

    const headers = new Headers(context.request.headers);
    // Remove host header so it doesn't confuse the upstream
    headers.delete('host');

    const resp = await fetch(target.toString(), {
      method: context.request.method,
      headers,
      body: context.request.method !== 'GET' && context.request.method !== 'HEAD'
        ? context.request.body
        : undefined,
      // @ts-expect-error — Node fetch supports duplex for streaming bodies
      duplex: 'half',
    });

    return new Response(resp.body, {
      status: resp.status,
      statusText: resp.statusText,
      headers: resp.headers,
    });
  }

  return next();
});
