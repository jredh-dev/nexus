import { defineMiddleware } from 'astro:middleware';
import { proxyToPortal } from './lib/proxy';

// Proxy Connect RPC, API calls, and portal-owned GET routes to the Go backend.
// POST /login and POST /signup are handled by page-level POST exports instead.
export const onRequest = defineMiddleware(async (context, next) => {
  const { pathname } = context.url;

  // Proxy GET routes that the portal backend owns (not Astro pages)
  const isProxiedGet = context.request.method === 'GET' &&
    (pathname === '/logout' || pathname.startsWith('/auth/'));

  // Proxy Connect RPC, legacy API calls, and portal-owned GETs
  if (pathname.startsWith('/portal.v1.') || pathname.startsWith('/api/') || isProxiedGet) {
    return proxyToPortal(context.request, pathname + context.url.search);
  }

  return next();
});
