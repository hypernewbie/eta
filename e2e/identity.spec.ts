import { expect, test } from "./_setup";

test("local host identity labels the desktop and its windows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const response = await page.request.get("/api/identity");
  const identity = await response.json();
  await page.goto("/");

  await expect(page.locator("#host-name")).toHaveText(identity.hostname);
  const title = page.locator(".winbox .wb-title").first();
  await expect(title).toContainText(`${identity.glyph} Explorer`);
  await expect(title).toHaveCSS("color", await page.evaluate(() => {
    const identityColor = getComputedStyle(document.documentElement).getPropertyValue("--identity-accent");
    const probe = document.createElement("i");
    probe.style.color = identityColor;
    document.body.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }));
});
