import { expect, test } from "./_setup";

test("local Explorer opens an xterm-backed terminal window", async ({
  page,
}) => {
  await page.goto("/");
  const explorer = page.locator(".winbox.eta-window:not(.peer-window)").last();
  await explorer.getByText("e2e", { exact: true }).click({ button: "right" });
  await page.locator('[data-file-action="terminal"]').click();

  const terminal = page.locator(".winbox.eta-window").last();
  await expect(terminal.locator(".terminal-xterm .xterm")).toBeVisible();
  const terminalTask = page.locator(
    '#task-strip .task-window[title*="Terminal"]',
  );
  await expect(terminalTask).toHaveCount(1);
  await terminal.locator(".wb-close").click({ force: true });
  await expect(terminalTask).toHaveCount(0);
});
