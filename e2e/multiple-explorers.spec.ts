import { expect, test } from "./_setup";

test("launcher opens independently navigable Explorer windows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  const explorers = page.locator(".winbox.eta-window");
  await expect(explorers).toHaveCount(1);
  await page.locator("#eta-launcher").click();
  await page.locator('[data-location="local"]').click();
  await expect(explorers).toHaveCount(2);
  await expect(page.locator("#task-strip")).toContainText("Explorer 2");

  const first = explorers.nth(0);
  const second = explorers.nth(1);
  const folder = second.locator(".entry.directory").first();
  const folderName = await folder.locator(".entry-name").textContent();
  if (!folderName) throw new Error("Expected a folder to navigate into");
  await folder.dblclick();

  await expect(second.locator('[data-explorer="breadcrumbs"]')).toContainText(
    folderName,
  );
  await expect(
    first.locator('[data-explorer="breadcrumbs"]'),
  ).not.toContainText(folderName);

  await second.locator(".wb-close").click();
  await expect(explorers).toHaveCount(1);
  await expect(first).toBeVisible();
});
