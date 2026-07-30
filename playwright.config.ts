import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  // Tests share the one stateful Eta web-server fixture.
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:17080",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command:
      "go run . --ip 127.0.0.1 --port 17080 --root . --identity-file test-results/identity.json --state-file test-results/state.json --peers-file test-results/peers.json --thumbnail-cache-dir test-results/thumbnails --remote-cache-dir test-results/remote-cache --transfer-dir test-results/transfers",
    url: "http://127.0.0.1:17080/api/healthz",
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
