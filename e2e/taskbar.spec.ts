import { expect, test } from "@playwright/test";

test("taskbar closes and reopens Explorer", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
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
  await expect(explorerTask).toHaveAttribute("title", /Explorer/);
  const explorerTaskBox = await explorerTask.boundingBox();
  expect(explorerTaskBox?.width).toBeLessThanOrEqual(34);

  await explorer.locator(".wb-close").click();
  await expect(explorer).toHaveCount(0);
  await expect(page.locator("#task-strip .task-window")).toHaveCount(0);

  await page.locator("#eta-launcher").click();
  const localLocation = page.locator('[data-location="local"]');
  await expect(localLocation).toBeVisible();
  await expect(localLocation).toContainText((await page.locator("#host-name").textContent())?.toUpperCase() || "");
  await localLocation.click();
  const reopened = page.locator(".winbox.eta-window");
  await expect(reopened).toHaveCount(1);
  await expect(
    reopened.locator('select[aria-label="Filesystem root"]'),
  ).toBeVisible();
});
