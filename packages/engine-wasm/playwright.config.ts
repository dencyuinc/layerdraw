// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {defineConfig, devices} from "@playwright/test";

export default defineConfig({
  testDir: "./test/browser",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "line",
  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node test/http-server.mjs",
    url: "http://127.0.0.1:4173/test/browser/harness.html",
    reuseExistingServer: false,
    timeout: 30_000,
  },
  projects: [
    {name: "chromium", use: {...devices["Desktop Chrome"]}},
    {name: "firefox", use: {...devices["Desktop Firefox"]}},
    {name: "webkit", use: {...devices["Desktop Safari"]}},
  ],
});
