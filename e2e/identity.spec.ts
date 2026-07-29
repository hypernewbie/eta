import { expect, test } from "@playwright/test";

test("local host identity labels the desktop and its windows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const response = await page.request.get("/api/identity");
  const identity = await response.json();
  await page.goto("/");

  await expect(page.locator("#host-name")).toHaveText(identity.hostname);
  await expect(page.locator(".winbox .wb-title").first()).toContainText(
    `${identity.glyph} Explorer`,
  );
});
