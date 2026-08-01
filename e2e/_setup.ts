// Shared Playwright fixture: ensure the stateful Eta web-server fixture
// starts each test from an empty desktop so window state from a prior
// test cannot poison assertions about explorer counts, paths, or active
// windows. The state save in web/app.ts uses an immediate flush on
// structural changes, but a defensive wipe here means a test that
// legitimately leaves Explorers open still cannot leak into the next one.
import { test as base, expect } from "@playwright/test";
import { rmSync } from "node:fs";

export const test = base;

test.beforeEach(() => {
  rmSync("test-results/state.json", { force: true });
});

export { expect };
