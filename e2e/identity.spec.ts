import { expect, test } from "./_setup";

test("local host identity labels the desktop and its windows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const response = await page.request.get("/api/identity");
  const identity = await response.json();
  const roots = await page.request.get("/api/roots").then((r) => r.json());
  await page.goto("/");

  // The header renders the hostname uppercase to match phi's brand area.
  await expect(page.locator("#hostname-display")).toHaveText(
    identity.hostname.toUpperCase(),
  );
  const title = page.locator(".winbox .wb-title").first();
  // A window is titled with the folder it is showing, not with the app
  // name, so at the top of a root that is the root's own name.
  await expect(title).toContainText(`${identity.glyph} ${roots[0].name}`);
  await expect(title).toHaveCSS(
    "color",
    await page.evaluate(() => {
      const identityColor = getComputedStyle(
        document.documentElement,
      ).getPropertyValue("--identity-accent");
      const probe = document.createElement("i");
      probe.style.color = identityColor;
      document.body.append(probe);
      const color = getComputedStyle(probe).color;
      probe.remove();
      return color;
    }),
  );
});

test("header actions sit on one row inside the header", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/");

  // These are laid out by a single .header-actions flex rule. Without
  // it they are block boxes (.icon-button is display: grid) and stack:
  // measured 77px of content in a 48px header, the status line clipped
  // above it and the palette button hanging 14px below it.
  const header = await page.locator(".app-header").boundingBox();
  const actions = await page.locator(".header-actions").boundingBox();
  const addPeer = await page.locator("#add-peer-button").boundingBox();
  const theme = await page.locator("#theme-button").boundingBox();

  expect(theme!.y).toBe(addPeer!.y);
  expect(theme!.x).toBeGreaterThan(addPeer!.x + addPeer!.width - 1);
  expect(actions!.height).toBeLessThanOrEqual(header!.height);
  expect(actions!.y).toBeGreaterThanOrEqual(header!.y);
  expect(actions!.y + actions!.height).toBeLessThanOrEqual(
    header!.y + header!.height,
  );
});

test("picking a swatch persists across a hard reload", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });

  // Reset the identity so this spec doesn't depend on the host's
  // pre-existing accent.
  const initial = await request.get("/api/identity").then((r) => r.json());
  await request.post("/api/identity", { data: { accent: initial.accent } });

  // Pick the first accent, hard-reload, verify server + UI agree,
  // then pick a different one to confirm the round-trip is not
  // cached. Use teal for the first pick because it has a stable
  // CSS value (no gradient aliases) and red is reserved for a
  // later assertion.
  await page.goto("/");
  await page.click("#theme-button");
  await page.click('[data-theme="teal"]');
  // Wait for the dialog to close before reopening it for the next
  // swatch — otherwise the click on #theme-button is occluded by
  // the still-open overlay.
  await page.locator("#theme-dialog").waitFor({ state: "hidden" });
  const tealAfterClick = await request
    .get("/api/identity")
    .then((r) => r.json());
  expect(tealAfterClick.accent).toBe("teal");

  await page.reload();
  await expect(page.locator("#hostname-display")).toBeVisible();
  const tealAfterReload = await request
    .get("/api/identity")
    .then((r) => r.json());
  expect(tealAfterReload.accent).toBe("teal");
  const tealCss = await page.evaluate(() =>
    getComputedStyle(document.documentElement)
      .getPropertyValue("--identity-accent")
      .trim(),
  );
  expect(tealCss.toLowerCase()).toBe(EXPECTED_ACCENT_HEX.teal);

  await page.click("#theme-button");
  await page.click('[data-theme="red"]');
  await page.locator("#theme-dialog").waitFor({ state: "hidden" });
  await page.reload();
  await expect(page.locator("#hostname-display")).toBeVisible();
  const redAfterReload = await request
    .get("/api/identity")
    .then((r) => r.json());
  expect(redAfterReload.accent).toBe("red");
  const redCss = await page.evaluate(() =>
    getComputedStyle(document.documentElement)
      .getPropertyValue("--identity-accent")
      .trim(),
  );
  expect(redCss.toLowerCase()).toBe(EXPECTED_ACCENT_HEX.red);

  // Leave the host at its original accent so other specs aren't
  // poisoned by this one.
  await request.post("/api/identity", { data: { accent: initial.accent } });
});

// Local mirror of the accent table for the assertions above. Eta
// keeps the authoritative table in web/app.ts; if a swatch moves,
// update both. The intent is "what color should the UI be after a
// reload", not a re-implementation of the registry.
const EXPECTED_ACCENT_HEX: Record<string, string> = {
  purple: "#7c6af7",
  blue: "#38bdf8",
  green: "#10b981",
  amber: "#fbbf24",
  red: "#f87171",
  pink: "#ec4899",
  teal: "#14b8a6",
  indigo: "#6366f1",
  orange: "#f97316",
  cyan: "#06b6d4",
};
