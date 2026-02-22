// Proxy helper for forwarding requests to the portal backend.
// Shared by middleware.ts and page POST handlers.

// Resolve portal backend URL at runtime only — import.meta.env is inlined
// at build time and will be undefined for non-PUBLIC_ variables.
export function getPortalUrl(): string {
  return process.env.PORTAL_URL || 'http://localhost:8080';
}

// Proxy a request to the portal backend, preserving headers (except Host).
export async function proxyToPortal(
  request: Request,
  pathAndSearch: string,
): Promise<Response> {
  const target = new URL(pathAndSearch, getPortalUrl());

  const headers = new Headers(request.headers);
  headers.delete('host');

  const resp = await fetch(target.toString(), {
    method: request.method,
    headers,
    body: request.method !== 'GET' && request.method !== 'HEAD'
      ? request.body
      : undefined,
    // @ts-expect-error — Node fetch supports duplex for streaming bodies
    duplex: 'half',
    redirect: 'manual',
  });

  return new Response(resp.body, {
    status: resp.status,
    statusText: resp.statusText,
    headers: resp.headers,
  });
}
