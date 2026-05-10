import { APIRequestContext, expect, Page, Request, test } from '@playwright/test';

const credentials = {
  username: process.env.MAXIOFS_E2E_USERNAME || 'admin',
  password: process.env.MAXIOFS_E2E_PASSWORD || 'admin',
};

const appRoutes = [
  '/',
  '/buckets',
  '/users',
  '/users/access-keys',
  '/tenants',
  '/groups',
  '/audit-logs',
  '/metrics',
  '/security',
  '/settings',
  '/roles/capabilities',
  '/cluster',
  '/cluster/nodes',
  '/cluster/migrations',
  '/cluster/ha',
  '/identity-providers',
  '/about',
];

type ApiResponse<T> = {
  data?: T;
  success?: boolean;
};

type BucketSummary = {
  name: string;
  tenant_id?: string;
  tenantId?: string;
};

type UserSummary = {
  id: string;
};

type GroupSummary = {
  id: string;
};

type PageIssue = {
  type: string;
  message: string;
  url?: string;
  status?: number;
};

function attachIssueCollectors(page: Page, issues: PageIssue[]) {
  page.on('pageerror', (error) => {
    issues.push({ type: 'pageerror', message: error.message });
  });

  page.on('console', (message) => {
    if (message.type() === 'error') {
      if (message.text().startsWith('Failed to load resource:')) {
        return;
      }
      issues.push({ type: 'console.error', message: message.text() });
    }
  });

  page.on('requestfailed', (request: Request) => {
    const url = request.url();
    if (url.includes('/api/v1/notifications/stream')) {
      return;
    }
    if (request.failure()?.errorText === 'net::ERR_ABORTED') {
      return;
    }
    issues.push({
      type: 'requestfailed',
      message: request.failure()?.errorText || 'request failed',
      url,
    });
  });

  page.on('response', (response) => {
    const status = response.status();
    const url = response.url();
    if (status >= 400) {
      issues.push({ type: 'http.error', message: response.statusText(), url, status });
    }
  });
}

async function login(page: Page) {
  await page.goto('/login', { waitUntil: 'domcontentloaded' });
  await expect(page.locator('#username')).toBeVisible();
  await page.locator('#username').fill(credentials.username);
  await page.locator('#password').fill(credentials.password);
  await page.locator('button[type="submit"]').click();
  await expect(page).not.toHaveURL(/\/login(?:\?|$)/);
  await expect(page.locator('body')).not.toContainText('Something went wrong');
}

async function getAuthHeaders(page: Page): Promise<Record<string, string>> {
  const token = await page.evaluate(() => localStorage.getItem('auth_token'));
  if (!token) {
    throw new Error('No auth token found after login');
  }
  return { Authorization: `Bearer ${token}` };
}

async function getApiData<T>(request: APIRequestContext, path: string, headers: Record<string, string>): Promise<T | null> {
  const response = await request.get(path, { headers });
  if (!response.ok()) {
    return null;
  }

  const payload = await response.json() as ApiResponse<T> | T;
  if (payload && typeof payload === 'object' && 'data' in payload) {
    return (payload as ApiResponse<T>).data ?? null;
  }
  return payload as T;
}

async function discoverDataRoutes(page: Page, request: APIRequestContext): Promise<string[]> {
  const headers = await getAuthHeaders(page);
  const routes: string[] = [];

  const buckets = await getApiData<BucketSummary[]>(request, '/api/v1/buckets', headers);
  const bucket = buckets?.[0];
  if (bucket?.name) {
    const bucketPath = `/buckets/${encodeURIComponent(bucket.name)}`;
    routes.push(bucketPath, `${bucketPath}/settings`);
  }

  const users = await getApiData<UserSummary[]>(request, '/api/v1/users', headers);
  const user = users?.[0];
  if (user?.id) {
    routes.push(`/users/${encodeURIComponent(user.id)}`);
  }

  const groupsPayload = await getApiData<GroupSummary[] | { groups?: GroupSummary[] }>(request, '/api/v1/groups?scope_global=true', headers);
  const groups = Array.isArray(groupsPayload) ? groupsPayload : groupsPayload?.groups;
  const group = groups?.[0];
  if (group?.id) {
    routes.push(`/groups/${encodeURIComponent(group.id)}`);
  }

  return routes;
}

async function assertRouteHealthy(page: Page, route: string) {
  await page.goto(route, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(new RegExp(`${route === '/' ? '/$' : route.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));
  await expect(page.locator('body')).not.toContainText('Something went wrong');
  await expect(page.locator('body')).not.toContainText('Failed to fetch dynamically imported module');
  await expect(page.locator('body')).not.toContainText('Loading chunk');
  await expect(page.locator('body')).not.toContainText('Cannot read properties of');
}

test.describe('MaxIOFS web console', () => {
  test('login and navigate primary console routes without frontend/runtime errors', async ({ page, request }) => {
    const issues: PageIssue[] = [];
    attachIssueCollectors(page, issues);

    await login(page);
    const dataRoutes = await discoverDataRoutes(page, request);

    for (const route of [...appRoutes, ...dataRoutes]) {
      await test.step(`route ${route}`, async () => {
        await assertRouteHealthy(page, route);
      });
    }

    expect(issues).toEqual([]);
  });
});
