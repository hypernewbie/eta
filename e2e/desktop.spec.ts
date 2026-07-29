import { expect, test } from "@playwright/test";

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
