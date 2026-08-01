// Browser coverage for cut+paste semantics in a single Explorer window.
// Two scenarios:
//
//   1. Successful cut+paste: source file removed, destination file
//      present.
//   2. Failing cut+paste (destination already exists): source preserved,
//      no cut applied. This is the journal's "interrupted Cut correctly
//      leaves the source untouched" guarantee (Known gaps #8) —
//      asserted end-to-end through the browser.
//
// The drive uses the local server's filesystem as the fixture:
// test-results/move-semantics/{src,dst} on the project root.
import { expect, test, type Locator, type Page } from "./_setup";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";

const FIXTURE = join(process.cwd(), "test-results/move-semantics");

async function setupFixture() {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(join(FIXTURE, "src"), { recursive: true });
  mkdirSync(join(FIXTURE, "dst"), { recursive: true });
}

async function openMoveSemantics(
  explorer: Locator,
  page: Page,
  request: import("@playwright/test").APIRequestContext,
) {
  await page.setViewportSize({ width: 1440, height: 900 });
  // Defeat any stale pagehide beacon from the previous test by
  // pushing empty state through the API one more time, immediately
  // before the first goto. handleStateGet re-reads from disk so this
  // is race-free relative to the pagehide save landing later.
  await request.put("/api/state", { data: { version: 1, windows: [] } });
  await page.goto("/");
  await expect(explorer).toBeVisible();
  await expect(explorer.locator("button.entry").first()).toBeVisible();
  // Drill test-results/ → move-semantics/ → src/
  for (const dir of ["test-results", "move-semantics", "src"]) {
    const target = explorer
      .locator("button.entry.directory", { hasText: dir })
      .first();
    await expect(target).toBeVisible();
    await target.dblclick();
    await expect(explorer.locator("button.entry").first()).toBeVisible();
  }
}

test("successful cut+paste moves a regular file and leaves source removed", async ({
  page,
  request,
}) => {
  await setupFixture();
  writeFileSync(join(FIXTURE, "src/alpha.md"), "# alpha\n");

  const explorer = page.locator(".winbox.eta-window").first();
  await openMoveSemantics(explorer, page, request);

  await explorer
    .locator("button.entry.file", { hasText: "alpha.md" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="cut"]').click();

  // Navigate back up to test-results/move-semantics/ by double-clicking
  // the parent entry (the leading ".." row).
  await explorer.locator("button.entry.parent").first().dblclick();
  await expect(
    explorer.locator("button.entry.directory", { hasText: "dst" }),
  ).toBeVisible();

  // Right-click the dst/ directory entry and Paste into Folder.
  await explorer
    .locator("button.entry.directory", { hasText: "dst" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="paste"]').click();

  // pasteIntoFolder's /api/copy + /api/delete pair runs after the
  // click dispatch, so we poll for the post-cut state rather than
  // asserting on a moment in between.
  await expect
    .poll(
      () =>
        existsSync(join(FIXTURE, "dst/alpha.md")) &&
        !existsSync(join(FIXTURE, "src/alpha.md")),
      { timeout: 5_000 },
    )
    .toBe(true);
  expect(readFileSync(join(FIXTURE, "dst/alpha.md"), "utf8")).toBe("# alpha\n");
});

test("failing cut+paste leaves the source intact and reports the error", async ({
  page,
  request,
}) => {
  await setupFixture();
  writeFileSync(join(FIXTURE, "src/alpha.md"), "# alpha source\n");
  // Pre-populate the destination with the same filename so the paste
  // trips fileops.Copy's "destination already exists" guard.
  writeFileSync(join(FIXTURE, "dst/alpha.md"), "# alpha destination\n");

  const explorer = page.locator(".winbox.eta-window").first();
  await openMoveSemantics(explorer, page, request);

  await explorer
    .locator("button.entry.file", { hasText: "alpha.md" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="cut"]').click();

  await explorer.locator("button.entry.parent").first().dblclick();
  await expect(
    explorer.locator("button.entry.directory", { hasText: "dst" }),
  ).toBeVisible();
  await explorer
    .locator("button.entry.directory", { hasText: "dst" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="paste"]').click();

  // The server returns an error; the webapp's surrounding try/catch
  // surfaces it as a toast (#error-message). We assert both the
  // toast and that the source remains untouched (no atomic delete
  // happened because the copy call threw before completeCut).
  await expect(page.locator("#error-message")).toContainText(
    /destination already exists/i,
  );
  expect(existsSync(join(FIXTURE, "src/alpha.md"))).toBe(true);
  expect(readFileSync(join(FIXTURE, "src/alpha.md"), "utf8")).toBe(
    "# alpha source\n",
  );
  expect(readFileSync(join(FIXTURE, "dst/alpha.md"), "utf8")).toBe(
    "# alpha destination\n",
  );
});
