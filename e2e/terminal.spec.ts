import { expect, test } from "@playwright/test";

test("local Explorer opens an xterm-backed terminal window", async ({ page }) => {
  await page.goto("/");
  const explorer = page.locator(".winbox.eta-window:not(.peer-window)").last();
  await explorer.getByText("e2e", { exact: true }).click({ button: "right" });
  await page.locator('[data-file-action="terminal"]').click();

  const terminal = page.locator(".winbox.eta-window").last();
  await expect(terminal.locator(".terminal-xterm .xterm")).toBeVisible();
  await terminal.locator(".wb-close").click({ force: true });
});
