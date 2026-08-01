// Shared Playwright fixture: ensure the stateful Eta web-server fixture
// starts each test from an empty desktop so window state from a prior
// test cannot poison assertions about explorer counts, paths, or active
// windows.
//
// The previous test's browser context fires a `pagehide` sendBeacon
// to /api/state at teardown, and that save races with a naive rmSync
// here. Putting empty state through the API right before each test is
// a synchronous overwrite: the call awaits the server response, so by
// the time the test's page.goto("/") fires the state on disk is empty.
// handleStateGet re-reads from disk on every call, so the long-lived
// webserver fixture picks up the empty state without a restart.
import { test as base, expect } from "@playwright/test";
import { rmSync } from "node:fs";

export const test = base.extend({});

test.beforeEach(async ({ request }) => {
  await request
    .put("/api/state", { data: { version: 1, windows: [] } })
    .catch(() => {
      // First test in a fresh run can race the server boot. The
      // test will fail more informatively at its first goto if so.
    });
  rmSync("test-results/state.json", { force: true });
});

export { expect };
