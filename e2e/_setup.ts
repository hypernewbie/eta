// Shared Playwright fixture: ensure the stateful Eta web-server fixture
// starts each test from an empty desktop so window state from a prior
// test cannot poison assertions about explorer counts, paths, or active
// windows.
//
// This MUST be an auto fixture, not test.beforeEach().
//
// A hook registered at module scope here binds to the suite of whichever
// spec file is being loaded when this module first evaluates. Node caches
// the module, so it evaluates exactly once — during the load of the
// alphabetically first spec that imports it — and every other spec file
// then gets the `test` object with none of the hooks attached. That is
// not hypothetical: it was the live behavior here, and the reset below
// ran for 3 of 15 tests while appearing to protect all of them. The
// result was intermittent cross-spec contamination, where a test's
// Explorer came up in a folder created by an entirely different spec.
// An auto fixture runs for every test that uses this `test` object.
//
// Cleanup has two layers:
//
//   1. Reset: PUT empty state to /api/state via the request fixture, then
//      rmSync the on-disk file. The PUT is awaited, so by the time the
//      test body runs the server's state is empty.
//
//   2. An addInitScript that neuters `navigator.sendBeacon` for the rest
//      of the page's lifetime. The webapp's pagehide handler (web/app.ts)
//      calls sendBeacon to persist desktop state when the tab closes —
//      without this, the previous test's pagehide beacon can land on the
//      server AFTER the reset fired and pin the next test's Explorer at a
//      stale path. Tests do their own state writes through /api/state via
//      the request fixture, so no real persistence is lost.
import { test as base, expect } from "@playwright/test";
import { rmSync } from "node:fs";

export const test = base.extend<{ cleanDesktop: void }>({
  cleanDesktop: [
    async ({ page, request }, use) => {
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
      await use();
    },
    { auto: true },
  ],
});

export { expect };
