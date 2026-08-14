import { defineConfig, devices } from '@playwright/test'

const reuseServers = process.env.E2E_REUSE_SERVERS === '1'
const previewBuild = process.env.E2E_PREVIEW === '1'

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:15080',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: 'go run ./testsupport/fakeopencode',
      cwd: '..',
      port: 15096,
      reuseExistingServer: reuseServers,
      timeout: 60_000,
      env: { MARVO_FAKE_OPENCODE_ADDR: '127.0.0.1:15096' },
    },
    {
      command: 'go run ./testsupport/e2eserver',
      cwd: '..',
      port: 15090,
      reuseExistingServer: reuseServers,
      timeout: 60_000,
    },
    {
      command: previewBuild
        ? 'npx vite preview --host 127.0.0.1 --port 15080'
        : 'npm run dev -- --host 127.0.0.1 --port 15080',
      port: 15080,
      reuseExistingServer: reuseServers,
      timeout: 60_000,
      env: { VITE_API_TARGET: 'http://127.0.0.1:15090' },
    },
  ],
  projects: [
    {
      name: 'chromium-landscape',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1024, height: 768 } },
    },
    {
      name: 'chromium-portrait',
      use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 } },
    },
    {
      name: 'webkit-portrait',
      use: { ...devices['iPhone 13'], browserName: 'webkit' },
    },
  ],
})
