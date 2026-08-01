import { expect, test } from "./_setup";

test("taskbar closes and reopens Explorer", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const identity = await page.request
    .get("/api/identity")
    .then((r) => r.json());
  await page.goto("/");

  const taskbar = page.locator("#taskbar");
  await expect(taskbar).toBeVisible();
  const taskbarBox = await taskbar.boundingBox();
  if (!taskbarBox) throw new Error("Taskbar has no bounding box");
  expect(taskbarBox.x).toBe(0);
  expect(taskbarBox.width).toBe(1440);
  expect(taskbarBox.y + taskbarBox.height).toBe(900);

  const explorer = page.locator(".winbox.eta-window").first();
  await expect(explorer).toBeVisible();
  const explorerTask = page.locator("#task-strip .task-window");
  await expect(explorerTask).toHaveCount(1);
  // The dock button carries the window's own title, which names the folder
  // the Explorer is showing rather than the application. Compared against
  // the window's title rather than an expected folder: this asserts the
  // dock/window relationship without also depending on which folder the
  // desktop happens to boot into.
  expect(await explorerTask.getAttribute("title")).toBe(
    await explorer.locator(".wb-title").textContent(),
  );
  expect(await explorerTask.getAttribute("title")).toContain(identity.glyph);
  const explorerTaskBox = await explorerTask.boundingBox();
  expect(explorerTaskBox?.width).toBeGreaterThanOrEqual(132);

  await explorer.locator(".wb-close").click();
  await expect(explorer).toHaveCount(0);
  await expect(page.locator("#task-strip .task-window")).toHaveCount(0);

  await page.locator("#eta-launcher").click();
  const localLocation = page.locator('[data-location="local"]');
  await expect(localLocation).toBeVisible();
  await expect(localLocation).toContainText((await page.locator("#hostname-display").textContent())?.toUpperCase() || "");
  await localLocation.click();
  const reopened = page.locator(".winbox.eta-window");
  await expect(reopened).toHaveCount(1);
  await expect(
    reopened.locator('select[aria-label="Filesystem root"]'),
  ).toBeVisible();
});

// A minimized window is represented by its dock button and nothing else.
// WinBox's own minimize leaves the window header parked at the bottom of
// the viewport, which lands above Eta's taskbar and reads as a second,
// half-broken taskbar.
test("minimizing hides the window and leaves only its dock button", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  const explorer = page.locator(".winbox.eta-window").first();
  await expect(explorer).toBeVisible();
  // Track this window's own dock button by key, so the assertions describe
  // one window rather than the whole desktop.
  const key = await page
    .locator("#task-strip .task-window")
    .first()
    .getAttribute("data-window");
  const dockButton = page.locator(`#task-strip [data-window="${key}"]`);

  await explorer.locator(".wb-min").click();
  await expect(explorer).toBeHidden();
  // The window still exists and is still listed; it is only put away.
  await expect(explorer).toHaveCount(1);
  await expect(dockButton).toHaveCount(1);
  await expect(dockButton).toHaveClass(/task-window-minimized/);

  // The dock button restores it, and clicking it again while focused
  // puts it away: taskbar toggle semantics.
  await dockButton.click();
  await expect(explorer).toBeVisible();
  await expect(dockButton).not.toHaveClass(/task-window-minimized/);

  await dockButton.click();
  await expect(explorer).toBeHidden();
  await expect(dockButton).toHaveClass(/task-window-minimized/);
});
