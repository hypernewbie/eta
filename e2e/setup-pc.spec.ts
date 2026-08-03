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
