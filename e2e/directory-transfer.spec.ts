// Browser coverage for cross-instance directory copy. The receiver side
// must materialize the full tree: nested subdirectories, regular files
// at each level, and entries whose relative paths match the source.
//
// Exercises internal/transfer/tree.go end-to-end through the Paste into
// Folder flow (Copy on a local source directory, Paste on a destination
// folder inside the peer Explorer). Journal step #1 calls out directory
// transfer coverage; peers.spec.ts only copies a single README into a
// pre-created destination folder.
import { expect, test } from "./_setup";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { PEER_URL, startPeer } from "./_peer";

test("copying a directory tree delivers nested files intact to a peer @peer", async ({
  page,
  request,
}) => {
  const peer = await startPeer("tree");
  mkdirSync(join(peer.dir, "destination"), { recursive: true });

  // Build a deterministically small tree on the local server.
  const srcDir = join(process.cwd(), "test-results/dir-source");
  rmSync(srcDir, { recursive: true, force: true });
  mkdirSync(join(srcDir, "nested/inner"), { recursive: true });
  writeFileSync(join(srcDir, "root.md"), "# root\n");
  writeFileSync(join(srcDir, "nested/inner/leaf.md"), "# leaf\n");

  try {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");

    await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    const enrolled = await request.post("/api/peers", { data: { url: PEER_URL } });
    expect(enrolled.ok()).toBeTruthy();
    await page.goto("/");
    await page.locator("#eta-launcher").click();
    await page.locator(`[data-location="${PEER_URL}"]`).click();

    const local = page.locator(".winbox.eta-window:not(.peer-window)").last();
    const remote = page.locator(".winbox.eta-window.peer-window");
    await expect(remote).toHaveCount(1);

    // Focus the local Explorer via the task strip and drill into
    // test-results/ so dir-source/ is visible as a directory entry.
    await page
      .locator('#task-strip .task-window[data-window^="explorer:"]')
      .first()
      .click();
    await local
      .locator("button.entry.directory", { hasText: "test-results" })
      .first()
      .dblclick();
    await expect(
      local.locator("button.entry.directory", { hasText: "dir-source" }),
    ).toBeVisible();

    // Copy the dir-source entry (Copy menu sets explorerClipboard to
    // this entry; Paste into Folder on the remote destination triggers
    // the cross-peer tree transfer).
    await local
      .locator("button.entry.directory", { hasText: "dir-source" })
      .first()
      .click({ button: "right", force: true });
    await page.locator('[data-file-action="copy"]').click();

    // Switch to the peer Explorer and paste into destination.
    await page
      .locator('#task-strip .task-window[data-window^="explorer:"]')
      .last()
      .click();
    await remote
      .locator("button.entry.directory", { hasText: "destination" })
      .first()
      .click({ button: "right", force: true });
    await page.locator('[data-file-action="paste"]').click();

    // Poll the disk until the full tree has arrived. The receiver
    // stages per file and promotes atomically; until the last chunk
    // verifies, the source-of-truth on the peer host is incomplete.
    await expect
      .poll(
        () =>
          existsSync(join(peer.dir, "destination/dir-source/root.md")) &&
          existsSync(
            join(peer.dir, "destination/dir-source/nested/inner/leaf.md"),
          ),
        { timeout: 15_000 },
      )
      .toBe(true);

    expect(
      readFileSync(join(peer.dir, "destination/dir-source/root.md"), "utf8"),
    ).toBe("# root\n");
    expect(
      readFileSync(
        join(peer.dir, "destination/dir-source/nested/inner/leaf.md"),
        "utf8",
      ),
    ).toBe("# leaf\n");
  } finally {
    try {
      await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    } catch {
      /* test timeout/teardown */
    }
    peer.stop();
  }
});
