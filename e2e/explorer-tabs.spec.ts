// Browser coverage for the per-Explorer tab strip added in journal
// step #6. Each Explorer hosts multiple folder tabs; click switches,
// the X on each tab closes, drag within the strip reorders, and the
// "+" button opens a new tab at the current root. Tabs are
// in-memory for this commit (persistence is a follow-up to keep
// this commit focused on the UI surface the journal asks for).
import { expect, test } from "./_setup";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const FIXTURE = join(process.cwd(), "test-results/explorer-tabs");

async function openExplorer(
  page: import("@playwright/test").Page,
  request: import("@playwright/test").APIRequestContext,
) {
  await page.setViewportSize({ width: 1440, height: 900 });
  await request.put("/api/state", { data: { version: 1, windows: [] } });
  await page.goto("/");
  await expect(page.locator(".winbox.eta-window").first()).toBeVisible();
}

// Returns the explorer-scoped entry-name locator scoped to file rows
// only — skips the leading ".." parent row at non-root paths. The
// directory-scoped helper filters by directory-kind rows matching the
// given name as a substring; callers should use names that don't
// collide with subdirectory names.
function filesIn(explorer: import("@playwright/test").Locator) {
  return explorer.locator("button.entry.file .entry-name");
}
function dirRow(explorer: import("@playwright/test").Locator, name: string) {
  return explorer
    .locator("button.entry.directory")
    .filter({ hasText: name })
    .first();
}

test("clicking a tab switches the explorer's view to that path", async ({
  page,
  request,
}) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(join(FIXTURE, "src"), { recursive: true });
  mkdirSync(join(FIXTURE, "other"), { recursive: true });
  writeFileSync(join(FIXTURE, "src/alpha.md"), "# alpha\n");
  writeFileSync(join(FIXTURE, "other/beta.md"), "# beta\n");

  await openExplorer(page, request);
  const explorer = page.locator(".winbox.eta-window").first();
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await dirRow(explorer, "src").dblclick();
  await expect(filesIn(explorer)).toContainText("alpha.md");

  await explorer.locator("[data-tab-new]").click();
  await expect(explorer.locator(".eta-tab")).toHaveCount(2);
  // New tab starts at project root; wait for the root listing to
  // render before the next dblclick uses its row data-paths.
  await expect(dirRow(explorer, "test-results")).toBeVisible();

  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await dirRow(explorer, "other").dblclick();
  await expect(filesIn(explorer)).toContainText("beta.md");

  // Click back to the first tab — switches to src/.
  await explorer.locator(".eta-tab").nth(0).click();
  await expect(filesIn(explorer)).toContainText("alpha.md");
});

test("drag reorders tabs within the strip", async ({ page, request }) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(join(FIXTURE, "a"), { recursive: true });
  mkdirSync(join(FIXTURE, "b"), { recursive: true });
  writeFileSync(join(FIXTURE, "a/one.md"), "# one\n");
  writeFileSync(join(FIXTURE, "b/two.md"), "# two\n");

  await openExplorer(page, request);
  const explorer = page.locator(".winbox.eta-window").first();

  // Drill into test-results/ → explorer-tabs/ once on the first tab.
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();

  // Open a new tab; navigate it into b/. The new tab starts at root,
  // so drill into test-results/ → explorer-tabs/ before reaching b/.
  await explorer.locator("[data-tab-new]").click();
  await expect(dirRow(explorer, "test-results")).toBeVisible();
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await dirRow(explorer, "b").dblclick();
  await expect(filesIn(explorer)).toContainText("two.md");

  await explorer.locator("[data-tab-new]").click();
  await expect(dirRow(explorer, "test-results")).toBeVisible();
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await dirRow(explorer, "a").dblclick();
  await expect(filesIn(explorer)).toContainText("one.md");

  const tabs = explorer.locator(".eta-tab");
  // Initial tab + two new tabs = 3.
  await expect(tabs).toHaveCount(3);

  // Drive the drag via dispatchEvent. Playwright's locator.dragTo
  // doesn't reliably trigger HTML5 drag handlers on chromium headless.
  await page.evaluate(() => {
    const strip = document.querySelector(
      '.winbox.eta-window [data-explorer="tab-strip"]',
    ) as HTMLElement | null;
    if (!strip) throw new Error("strip missing");
    const tabs = strip.querySelectorAll("[data-tab]");
    if (tabs.length < 2) throw new Error("expected at least 2 tabs");
    const dt = new DataTransfer();
    tabs[1]!.dispatchEvent(
      new DragEvent("dragstart", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      }),
    );
    tabs[0]!.dispatchEvent(
      new DragEvent("dragover", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      }),
    );
    tabs[0]!.dispatchEvent(
      new DragEvent("drop", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
      }),
    );
  });

  // After reorder: tabs[0] is the moved tab (b); tabs[1] is the
  // original tab; tabs[2] still a.
  const titles = await tabs.evaluateAll((nodes) =>
    nodes.map((n) => n.getAttribute("title")),
  );
  expect(titles[0]).toContain("explorer-tabs/b");
  expect(titles[1]).toBe("test-results/explorer-tabs");
  expect(titles[2]).toContain("explorer-tabs/a");
});

test("close button removes a tab", async ({ page, request }) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(join(FIXTURE, "src"), { recursive: true });
  writeFileSync(join(FIXTURE, "src/alpha.md"), "# alpha\n");

  await openExplorer(page, request);
  const explorer = page.locator(".winbox.eta-window").first();
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await explorer.locator("[data-tab-new]").click();
  // The new tab is at project root; navigate into test-results/ then
  // explorer-tabs/ to reach src/. Wait for the root listing to render
  // before dblclicking on its entries.
  await expect(dirRow(explorer, "test-results")).toBeVisible();
  await dirRow(explorer, "test-results").dblclick();
  await dirRow(explorer, "explorer-tabs").dblclick();
  await dirRow(explorer, "src").dblclick();
  await expect(explorer.locator(".eta-tab")).toHaveCount(2);

  // Close the active tab — should leave exactly one.
  await explorer.locator("[data-tab-close='1']").click();
  await expect(explorer.locator(".eta-tab")).toHaveCount(1);
});
