import { test, expect } from '@playwright/test';

// Full browser-driven login cycle test.
// Uses the seeded demo account (demo@demo.com / demo).
//
// Run:
//   BASE_URL=https://portal.jredh.com npx playwright test
//   BASE_URL=http://localhost:4321 npx playwright test

test.describe('Login cycle', () => {
  test('shows login page', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('h1')).toContainText('Welcome Back');
    await expect(page.locator('form')).toBeVisible();
  });

  test('bad credentials show error on login page', async ({ page }) => {
    await page.goto('/login');

    await page.fill('input[name="email"]', 'nobody@example.com');
    await page.fill('input[name="password"]', 'wrongpassword');
    await page.click('button[type="submit"]');

    // Should stay on /login with an error message.
    await page.waitForURL(/\/login/);
    await expect(page.locator('.notification.is-danger')).toBeVisible();
    await expect(page.locator('.notification.is-danger')).toContainText('Invalid');
  });

  test('valid credentials redirect to dashboard with real data', async ({ page }) => {
    await page.goto('/login');

    await page.fill('input[name="email"]', 'demo@demo.com');
    await page.fill('input[name="password"]', 'demo');
    await page.click('button[type="submit"]');

    // Should redirect to /dashboard.
    await page.waitForURL(/\/dashboard/, { timeout: 15_000 });
    await expect(page.locator('h1')).toContainText('Dashboard');

    // Should show the demo user's profile data.
    await expect(page.locator('text=demo@demo.com')).toBeVisible();

    // Should have at least one active session.
    await expect(page.locator('table')).toBeVisible();
  });

  test('logout redirects to home', async ({ page }) => {
    // Login first.
    await page.goto('/login');
    await page.fill('input[name="email"]', 'demo@demo.com');
    await page.fill('input[name="password"]', 'demo');
    await page.click('button[type="submit"]');
    await page.waitForURL(/\/dashboard/, { timeout: 15_000 });

    // Click logout.
    await page.click('a[href="/logout"]');

    // Should redirect to /.
    await page.waitForURL(/^\/$|\/$/);
  });
});

test.describe('Signup page', () => {
  test('shows signup page', async ({ page }) => {
    await page.goto('/signup');
    await expect(page.locator('h1')).toContainText('Create Account');
    await expect(page.locator('form')).toBeVisible();
  });

  test('missing fields show error', async ({ page }) => {
    await page.goto('/signup');

    // Fill all required fields but use an invalid/duplicate email to trigger
    // a server-side error from the portal backend.
    await page.fill('input[name="username"]', 'testuser_' + Date.now());
    await page.fill('input[name="email"]', 'demo@demo.com'); // duplicate
    await page.fill('input[name="phone"]', '5551234567');
    await page.fill('input[name="password"]', 'password123');

    await page.click('button[type="submit"]');

    // Should redirect back to /signup with an error.
    await page.waitForURL(/\/signup/, { timeout: 10_000 });
    await expect(page.locator('.notification.is-danger')).toBeVisible();
  });
});
