// Regression: a peer record without name/id/accent/glyph used to crash
// the browser on peer.name.toUpperCase() in refreshEtaMenu and
// desktopIconModel, taking the whole desktop and the η menu down with
// it. The record shape exists in the wild: a PC set up over SSH by an
// eta older than the one in dc50055..e935a76 wrote the record with
// only SSHDestination and URL, and a re-set-up against the same
// destination is the user's normal way out — not something a clean
// reinstall fixes.
//
// Drives the failure through the API: /api/peers is stubbed to return
// the bad record shape. The desktop icon and the η menu must both
// render the peer, with whatever name the browser can synthesize from
// the URL, rather than throwing on the first iteration.
import { expect, test } from "./_setup";

test("a peer with no name still renders in the desktop and the η menu", async ({
  page,
}) => {
  await page.route("**/api/peers", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          url: "http://jupiter.local:7080",
          // Deliberately no name/id/accent/glyph — the regression shape.
        },
      ]),
    });
  });

  await page.goto("/");

  // The η menu must list the peer; on the unfixed code the
  // .map() throws on peer.name.toUpperCase() and the menu never
  // populates.
  await page.locator("#eta-launcher").click();
  const menuEntry = page.locator('[data-location="http://jupiter.local:7080"]');
  await expect(menuEntry).toBeVisible();
  // Whatever fallback the browser uses, it must be non-empty so the
  // user has something to recognize the PC by.
  const menuText = (await menuEntry.textContent())?.trim() ?? "";
  expect(menuText.length).toBeGreaterThan(0);
  await page.keyboard.press("Escape");
  await page
    .locator("#eta-menu")
    .evaluate((menu) => ((menu as HTMLElement).hidden = true));

  // The desktop icon must also render. data-desktop-icon is keyed by
  // "computer:<url>"; a missing icon for the peer means the icon
  // model threw and the whole list was discarded.
  const icon = page.locator(
    '[data-desktop-icon="computer:http://jupiter.local:7080"]',
  );
  await expect(icon).toBeVisible();
});
