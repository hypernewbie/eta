import { expect, test } from "./_setup";

test("Explorer can drag from its title bar and still maximize and restore", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  const explorer = page
    .locator(".winbox.eta-window")
    .filter({ hasText: "" })
    .first();
  await expect(explorer).toBeVisible();
  const titleBar = explorer.locator(".wb-drag");
  await expect(titleBar).toBeVisible();

  const before = await explorer.boundingBox();
  if (!before) throw new Error("Explorer has no bounding box");
  const title = await titleBar.boundingBox();
  if (!title) throw new Error("Explorer title bar has no bounding box");

  await page.mouse.move(title.x + 100, title.y + title.height / 2);
  await page.mouse.down();
  await page.mouse.move(title.x + 250, title.y + title.height / 2 + 90, {
    steps: 12,
  });
  await page.mouse.up();

  const after = await explorer.boundingBox();
  if (!after) throw new Error("Explorer disappeared after dragging");
  expect(after.x).toBeGreaterThan(before.x + 80);
  expect(after.y).toBeGreaterThan(before.y + 60);

  const maximize = explorer.locator(".wb-max");
  await maximize.click();
  await expect(explorer).toHaveClass(/max/);
  await maximize.click();
  await expect(explorer).not.toHaveClass(/max/);
});

test("scrolling the file list keeps the toolbar and column header in place", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  const win = page.locator(".winbox.eta-window").first();
  await expect(win.locator(".entry").first()).toBeVisible();

  // The list must overflow for this to prove anything. The test root is
  // the repo, which is comfortably taller than the window.
  const overflows = await win
    .locator(".entries")
    .evaluate((el) => el.scrollHeight > el.clientHeight + 1);
  expect(overflows).toBe(true);

  // Positions relative to the window body, so this measures the chrome
  // staying put rather than the page not moving.
  const chrome = async () =>
    await win.evaluate((w) => {
      const top = w.querySelector(".wb-body")!.getBoundingClientRect().top;
      const at = (s: string) =>
        Math.round(w.querySelector(s)!.getBoundingClientRect().top - top);
      return {
        toolbar: at(".toolbar"),
        head: at(".table-head"),
        footer: at(".panel-footer"),
        firstRow: at(".entry"),
      };
    });

  const before = await chrome();
  await win.locator(".entries").evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  const after = await chrome();

  expect(after.firstRow).toBeLessThan(before.firstRow - 100);
  expect(after.toolbar).toBe(before.toolbar);
  expect(after.head).toBe(before.head);
  expect(after.footer).toBe(before.footer);

  // The window itself must not be a scroller, or the chrome leaves with
  // the list however the inner list is configured.
  const bodyScrolls = await win
    .locator(".wb-body")
    .evaluate((el) => el.scrollHeight > el.clientHeight + 1);
  expect(bodyScrolls).toBe(false);
});
