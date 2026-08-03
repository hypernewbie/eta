import { expect, test } from "./_setup";

// The SSH setup flow, driven only as far as it can go without a real
// second machine to reach. What is checked here is the part that is this
// project's own: the dialog, the request it sends, the phase it shows,
// and that a failure surfaces the remote's own words rather than a bare
// "something went wrong".
//
// The connection itself is covered where it can be done honestly, in
// internal/remotepc's real-sshd tests, against a real sshd.

test("the setup dialog opens from the header and sends the typed destination", async ({
  page,
}) => {
  await page.goto("/");

  const dialog = page.locator("#setup-pc-dialog");
  await expect(dialog).not.toBeVisible();

  await page.locator("#setup-pc-button").click();
  await expect(dialog).toBeVisible();

  // Progress is hidden until something is actually running: an idle
  // dialog claiming a phase would be lying.
  await expect(page.locator("#setup-pc-progress")).not.toBeVisible();

  const posted: string[] = [];
  await page.route("**/api/remote-pc", async (route) => {
    if (route.request().method() === "POST") {
      posted.push(route.request().postData() ?? "");
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({ destination: "minerva", phase: "connecting" }),
      });
      return;
    }
    await route.continue();
  });
  // Keep it in a working phase so this test is only about the request.
  await page.route("**/api/remote-pc?*", async (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ destination: "minerva", phase: "installing" }),
    }),
  );

  await page.locator("#setup-pc-destination").fill("minerva");
  await page.locator("#setup-pc-start").click();

  await expect.poll(() => posted.length).toBeGreaterThan(0);
  expect(JSON.parse(posted[0])).toEqual({ destination: "minerva" });

  // The phase is shown in words, not the server's own terse token.
  await expect(page.locator("#setup-pc-progress")).toBeVisible();
  await expect(page.locator("#setup-pc-phase-text")).toContainText(
    /installing eta/i,
  );
});

test("a failed setup shows the reason and the remote's own output", async ({
  page,
}) => {
  await page.goto("/");

  await page.route("**/api/remote-pc", async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({ destination: "minerva", phase: "connecting" }),
      });
      return;
    }
    await route.continue();
  });
  await page.route("**/api/remote-pc?*", async (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        destination: "minerva",
        phase: "failed",
        error: "no Go toolchain found on this PC",
        recent: ["ETA:fail:no Go toolchain found on this PC"],
      }),
    }),
  );

  await page.locator("#setup-pc-button").click();
  await page.locator("#setup-pc-destination").fill("minerva");
  await page.locator("#setup-pc-start").click();

  // The specific reason, not a generic failure: this is the difference
  // between a user installing Go and a user filing a bug.
  await expect(page.locator("#setup-pc-phase-text")).toContainText(
    "no Go toolchain found on this PC",
  );
  // The remote's own output appears only on failure.
  await expect(page.locator("#setup-pc-output")).toBeVisible();
  await expect(page.locator("#setup-pc-output")).toContainText(
    "no Go toolchain found",
  );

  // Recoverable: the form is usable again rather than stuck disabled.
  await expect(page.locator("#setup-pc-start")).toBeEnabled();
  await expect(page.locator("#setup-pc-destination")).toBeEnabled();
});

test("a rejected destination is reported without starting anything", async ({
  page,
}) => {
  await page.goto("/");
  await page.locator("#setup-pc-button").click();

  // The real server answers this one: a destination ssh would read as an
  // option is refused at the edge, so no session ever starts.
  await page.locator("#setup-pc-destination").fill("-oProxyCommand=whoami");
  await page.locator("#setup-pc-start").click();

  // The server's own reason reaches the user, not a generic
  // "Request failed (400)" -- which is what this showed before the
  // handlers were switched to the project's JSON error convention.
  await expect(page.locator("#setup-pc-phase-text")).toContainText(
    /ssh would read/i,
  );
  await expect(page.locator("#setup-pc-start")).toBeEnabled();
});

// The SSH actions must appear only for a PC that is actually SSH-backed.
// An ordinary peer is something already running that this instance just
// talks to: there is nothing to reconnect, disconnect or uninstall, and
// offering "Remove Eta from that PC" for one would be a lie about what
// the button does.
test("SSH-only actions appear for an SSH-backed PC and not an ordinary one", async ({
  page,
}) => {
  await page.route("**/api/peers", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          url: "http://ordinary.example:7080",
          name: "ORDINARY",
          id: "a",
          accent: "#4c8",
          glyph: "O",
        },
        {
          url: "http://127.0.0.1:41999",
          name: "VIASSH",
          id: "b",
          accent: "#84c",
          glyph: "V",
          ssh_destination: "pi@minerva",
        },
      ]),
    });
  });
  await page.goto("/");

  const ssh = ["reconnect-pc", "disconnect-pc", "cleanup-pc"];
  const menu = page.locator("#desktop-context-menu");

  // The ordinary peer offers none of them.
  await page
    .locator('[data-desktop-icon="computer:http://ordinary.example:7080"]')
    .click({ button: "right" });
  await expect(menu).toBeVisible();
  for (const action of ssh) {
    await expect(
      menu.locator(`[data-desktop-action="${action}"]`),
    ).not.toBeVisible();
  }
  // It still offers the ordinary one, so this is gating and not a menu
  // that simply failed to populate.
  await expect(
    menu.locator('[data-desktop-action="remove-peer"]'),
  ).toBeVisible();
  // Dismiss first: the open menu is positioned at the cursor and overlays
  // the next icon, so the second right-click would land on the menu. A
  // real click away from it fires the pointerdown the app closes on.
  await page.mouse.click(1000, 300);
  await expect(menu).not.toBeVisible();

  // The SSH-backed one offers all three.
  await page
    .locator('[data-desktop-icon="computer:http://127.0.0.1:41999"]')
    .click({ button: "right" });
  await expect(menu).toBeVisible();
  for (const action of ssh) {
    await expect(
      menu.locator(`[data-desktop-action="${action}"]`),
    ).toBeVisible();
  }
});

// Reconnecting sends the saved SSH destination, not the peer's current
// URL. The URL is a forwarded port from a previous session and is
// meaningless once that session ended -- using it would reconnect
// nothing.
test("reconnecting a saved PC sends its SSH destination", async ({ page }) => {
  await page.route("**/api/peers", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          url: "http://127.0.0.1:41999",
          name: "VIASSH",
          id: "b",
          accent: "#84c",
          glyph: "V",
          ssh_destination: "pi@minerva",
        },
      ]),
    });
  });
  const posted: string[] = [];
  await page.route("**/api/remote-pc", async (route) => {
    if (route.request().method() === "POST") {
      posted.push(route.request().postData() ?? "");
      return route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({
          destination: "pi@minerva",
          phase: "connecting",
        }),
      });
    }
    await route.continue();
  });
  await page.route("**/api/remote-pc?*", async (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ destination: "pi@minerva", phase: "installing" }),
    }),
  );

  await page.goto("/");
  await page
    .locator('[data-desktop-icon="computer:http://127.0.0.1:41999"]')
    .click({ button: "right" });
  await page.locator('[data-desktop-action="reconnect-pc"]').click();

  await expect.poll(() => posted.length).toBeGreaterThan(0);
  expect(JSON.parse(posted[0])).toEqual({ destination: "pi@minerva" });
  // The dialog opens already showing progress, prefilled and locked.
  await expect(page.locator("#setup-pc-dialog")).toBeVisible();
  await expect(page.locator("#setup-pc-destination")).toHaveValue("pi@minerva");
});
