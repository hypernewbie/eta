// Shared Playwright fixture: ensure the stateful Eta web-server fixture
// starts each test from an empty desktop so window state from a prior
// test cannot poison assertions about explorer counts, paths, or active
// windows.
//
// Inter-test state cleanup runs in two layers:
//
//   1. beforeEach: PUTs empty state to /api/state via the request
//      fixture, then rmSyncs the on-disk file. The PUT is synchronous
//      (await the response), so by the time the test body runs the
//      server's state file is empty.
//
//   2. An addInitScript that neuters `navigator.sendBeacon` for the
//      rest of the page's lifetime. The webapp's pagehide handler
//      (web/app.ts) calls sendBeacon to persist desktop state when
//      the tab closes — without this, the previous test's pagehide
//      beacon can land on the server AFTER beforeEach's PUT fired
//      and pin the next test's Explorer at a stale path. Tests do
//      their own state writes through /api/state via the request
//      fixture, so no real persistence is lost.
import { test as base, expect } from "@playwright/test";
import { rmSync } from "node:fs";

export const test = base.extend({});

test.beforeEach(async ({ page, request }) => {
  await page.addInitScript(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    navigator.sendBeacon = () => true;
  });
  await request
    .put("/api/state", { data: { version: 1, windows: [] } })
    .catch(() => {
      // If the server isn't reachable yet the first test in a fresh
      // run will fail more informatively at its first goto.
    });
  rmSync("test-results/state.json", { force: true });
});

export { expect };
