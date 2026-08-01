// Browser coverage for two related journal step #2 items: persisting
// the clipboard source descriptor (host, root, path, operation)
// across page reloads, and using drag/drop between Explorer windows
// as another frontend into the same paste planner.
//
// The drag/drop test uses sibling source + destination inside one
// Explorer so we don't have to navigate mid-test (Playwright locators
// re-evaluate after navigation). The cross-Explorer case is the same
// planner path; the journal calls them "another frontend" precisely
// because the planner is identical.
import { expect, test } from "./_setup";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const FIXTURE = join(process.cwd(), "test-results/clipboard-dragdrop");

async function openFixture(explorer: import("@playwright/test").Locator, page: import("@playwright/test").Page, request: import("@playwright/test").APIRequestContext) {
  await page.setViewportSize({ width: 1440, height: 900 });
  await request.put("/api/state", { data: { version: 1, windows: [] } });
  await page.goto("/");
  await expect(explorer).toBeVisible();
  await expect(explorer.locator("button.entry").first()).toBeVisible();
  for (const dir of ["test-results", "clipboard-dragdrop"]) {
    await explorer
      .locator("button.entry.directory", { hasText: dir })
      .first()
      .dblclick();
  }
}

test("drag/drop a file onto a sibling folder row routes through the same paste planner", async ({
  page,
  request,
}) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(FIXTURE, { recursive: true });
  mkdirSync(join(FIXTURE, "dst"), { recursive: true });
  writeFileSync(join(FIXTURE, "dragged.md"), "# dragged\n");

  const explorer = page.locator(".winbox.eta-window").first();
  await openFixture(explorer, page, request);

  const source = explorer.locator("button.entry.file", {
    hasText: "dragged.md",
  }).first();
  await expect(source).toBeVisible();
  const targetFolder = explorer.locator("button.entry.directory", {
    hasText: "dst",
  }).first();
  await expect(targetFolder).toBeVisible();

  // Drive the drag via programmatic event dispatch. Playwright's
  // locator.dragTo simulates pointer move/down/up which Chromium's
  // drag handler treats as a synthetic drag but doesn't always
  // route through to HTML5 dataTransfer listeners on chromium
  // headless; explicit dispatchEvent is reliable.
  const dispatchResult = await page.evaluate(
    async ({ sourceSel, targetSel }) => {
      const source = document.querySelector(sourceSel);
      const target = document.querySelector(targetSel);
      if (!(source instanceof HTMLElement) || !(target instanceof HTMLElement)) {
        return { ok: false, reason: "elements not found" };
      }
      const rect = target.getBoundingClientRect();
      const dt = new DataTransfer();
      const dragStart = new DragEvent("dragstart", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height / 2,
      });
      source.dispatchEvent(dragStart);
      const dragOver = new DragEvent("dragover", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height / 2,
      });
      target.dispatchEvent(dragOver);
      const drop = new DragEvent("drop", {
        bubbles: true,
        cancelable: true,
        dataTransfer: dt,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height / 2,
      });
      target.dispatchEvent(drop);
      return { ok: true };
    },
    {
      sourceSel: "button.entry.file",
      targetSel: "button.entry.directory",
    },
  );
  expect(dispatchResult.ok, JSON.stringify(dispatchResult)).toBe(true);

  // Poll until the file appears at dst/dragged.md. Same-local paste
  // hits /api/copy, which is near-instant.
  await expect
    .poll(() => existsSync(join(FIXTURE, "dst/dragged.md")), {
      timeout: 5_000,
    })
    .toBe(true);
  expect(readFileSync(join(FIXTURE, "dst/dragged.md"), "utf8")).toBe(
    "# dragged\n",
  );

  // The clipboard descriptor should now be persisted in localStorage
  // (set during dragstart), exercising the "persist" half of step #2.
  const persisted = await page.evaluate(() =>
    window.localStorage.getItem("eta.clipboard"),
  );
  expect(persisted).toBeTruthy();
  const parsed = JSON.parse(persisted!);
  expect(parsed.operation).toBe("copy");
  expect(parsed.path).toBe("test-results/clipboard-dragdrop/dragged.md");
  expect(parsed.host).toBe("local");
});

test("copied descriptor survives a page reload via localStorage", async ({
  page,
  request,
}) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(FIXTURE, { recursive: true });
  writeFileSync(join(FIXTURE, "reload.md"), "# reload\n");

  const explorer = page.locator(".winbox.eta-window").first();
  await openFixture(explorer, page, request);

  await explorer
    .locator("button.entry.file", { hasText: "reload.md" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="copy"]').click();

  const before = await page.evaluate(() =>
    window.localStorage.getItem("eta.clipboard"),
  );
  expect(before).toBeTruthy();
  expect(JSON.parse(before!).path).toBe(
    "test-results/clipboard-dragdrop/reload.md",
  );

  await page.reload();
  await expect(
    page.locator(".winbox.eta-window").first(),
  ).toBeVisible();

  const after = await page.evaluate(() =>
    window.localStorage.getItem("eta.clipboard"),
  );
  expect(after).toBeTruthy();
  expect(JSON.parse(after!).path).toBe(
    "test-results/clipboard-dragdrop/reload.md",
  );
});

test("dragging a cut item preserves the cut operation through the drop", async ({
  page,
  request,
}) => {
  rmSync(FIXTURE, { recursive: true, force: true });
  mkdirSync(FIXTURE, { recursive: true });
  mkdirSync(join(FIXTURE, "dst"), { recursive: true });
  writeFileSync(join(FIXTURE, "moved.md"), "# moved\n");

  const explorer = page.locator(".winbox.eta-window").first();
  await openFixture(explorer, page, request);

  // Cut via the context menu so explorerClipboardOperation = "cut".
  await explorer
    .locator("button.entry.file", { hasText: "moved.md" })
    .first()
    .click({ button: "right" });
  await page.locator('[data-file-action="cut"]').click();

  const beforeDrag = await page.evaluate(() =>
    window.localStorage.getItem("eta.clipboard"),
  );
  expect(JSON.parse(beforeDrag!).operation).toBe("cut");

  // Drag the same file onto the dst folder. The dragstart handler
  // used to silently reset operation to "copy", silently demoting
  // the user's cut intent. With the fix, dragging the same item
  // preserves the cut, so the drop performs a move (source gone,
  // destination populated).
  const result = await page.evaluate(
    async ({ sourceSel, targetSel }) => {
      const source = document.querySelector(sourceSel);
      const target = document.querySelector(targetSel);
      if (!(source instanceof HTMLElement) || !(target instanceof HTMLElement)) {
        return { ok: false };
      }
      const rect = target.getBoundingClientRect();
      const dt = new DataTransfer();
      source.dispatchEvent(
        new DragEvent("dragstart", {
          bubbles: true,
          cancelable: true,
          dataTransfer: dt,
        }),
      );
      target.dispatchEvent(
        new DragEvent("dragover", {
          bubbles: true,
          cancelable: true,
          dataTransfer: dt,
          clientX: rect.left + rect.width / 2,
          clientY: rect.top + rect.height / 2,
        }),
      );
      target.dispatchEvent(
        new DragEvent("drop", {
          bubbles: true,
          cancelable: true,
          dataTransfer: dt,
          clientX: rect.left + rect.width / 2,
          clientY: rect.top + rect.height / 2,
        }),
      );
      return { ok: true };
    },
    {
      sourceSel: 'button.entry.file',
      targetSel: 'button.entry.directory',
    },
  );
  expect(result.ok).toBe(true);

  // Cut → move semantics: source disappears, destination gains it.
  await expect
    .poll(() => existsSync(join(FIXTURE, "moved.md")), { timeout: 5_000 })
    .toBe(false);
  await expect
    .poll(() => existsSync(join(FIXTURE, "dst/moved.md")), { timeout: 5_000 })
    .toBe(true);
});
