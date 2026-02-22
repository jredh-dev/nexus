/**
 * Smoke test for the full login/signup cycle.
 *
 * Requires a running Astro server with a reachable portal backend.
 *
 * Usage:
 *   BASE_URL=https://portal.jredh.com node tests/smoke.mjs
 *   BASE_URL=http://localhost:4321 PORTAL_URL=http://localhost:8080 node tests/smoke.mjs
 *
 * Exit 0 = all checks passed. Non-zero = failure.
 */

import { test } from 'node:test';
import assert from 'node:assert/strict';

const BASE_URL = process.env.BASE_URL || 'https://portal.jredh.com';

// Fetch without following redirects so we can inspect Location headers.
async function fetchNoFollow(url, init = {}) {
  return fetch(url, { ...init, redirect: 'manual' });
}

// --- POST /login ---

test('POST /login with bad credentials redirects to /login?error=', async () => {
  const body = new URLSearchParams({ email: 'nobody@example.com', password: 'wrong' });
  const resp = await fetchNoFollow(`${BASE_URL}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
  });

  // Portal returns 303 → /login?error=...
  assert.equal(resp.status, 303, `expected 303, got ${resp.status}`);
  const location = resp.headers.get('location') ?? '';
  assert.ok(
    location.includes('/login') && location.includes('error='),
    `expected redirect to /login?error=..., got Location: ${location}`,
  );
});

test('POST /login with demo credentials redirects to /dashboard and sets session cookie', async () => {
  const body = new URLSearchParams({ email: 'demo@demo.com', password: 'demo' });
  const resp = await fetchNoFollow(`${BASE_URL}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
  });

  assert.equal(resp.status, 303, `expected 303, got ${resp.status}`);
  const location = resp.headers.get('location') ?? '';
  assert.ok(location.includes('/dashboard'), `expected redirect to /dashboard, got Location: ${location}`);

  const setCookie = resp.headers.get('set-cookie') ?? '';
  assert.ok(setCookie.includes('session='), `expected session cookie, got Set-Cookie: ${setCookie}`);
});

// --- POST /signup ---

test('POST /signup with missing fields redirects to /signup?error=', async () => {
  // Missing phone — portal will reject.
  const body = new URLSearchParams({
    username: 'smoketest_' + Date.now(),
    email: `smoketest_${Date.now()}@example.com`,
    password: 'password123',
    name: 'Smoke Test',
    // phone intentionally omitted
  });
  const resp = await fetchNoFollow(`${BASE_URL}/signup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: body.toString(),
  });

  assert.equal(resp.status, 303, `expected 303, got ${resp.status}`);
  const location = resp.headers.get('location') ?? '';
  assert.ok(
    location.includes('/signup') && location.includes('error='),
    `expected redirect to /signup?error=..., got Location: ${location}`,
  );
});

// --- GET /login and GET /signup return 200 ---

test('GET /login returns 200', async () => {
  const resp = await fetch(`${BASE_URL}/login`);
  assert.equal(resp.status, 200, `expected 200, got ${resp.status}`);
});

test('GET /signup returns 200', async () => {
  const resp = await fetch(`${BASE_URL}/signup`);
  assert.equal(resp.status, 200, `expected 200, got ${resp.status}`);
});

// --- GET /logout redirects to / ---

test('GET /logout redirects to /', async () => {
  const resp = await fetchNoFollow(`${BASE_URL}/logout`);
  assert.equal(resp.status, 303, `expected 303, got ${resp.status}`);
  const location = resp.headers.get('location') ?? '';
  assert.ok(location === '/' || location.endsWith('/'), `expected redirect to /, got Location: ${location}`);
});
