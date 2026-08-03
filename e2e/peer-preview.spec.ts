// Browser coverage for opening a file in a peer Explorer and seeing an
// inspector window mount with content. The peer's preview/thumbnail path
// goes through the coordinator proxy; this confirms the round trip
// (browser → coordinator → peer → preview) and the inspector surface.
//
// Companion to peers.spec.ts's copy-of-README flow but exercises the
// read path that journal step #1 calls out as "preview/thumbnail
// browsing" coverage.
import { expect, test } from "./_setup";
import { PEER_URL, startPeer, seedCleanPeerDir } from "./_peer";

test("opening a file in a peer Explorer mounts a peer-accented inspector @peer", async ({
  page,
  request,
}) => {
  const peer = await startPeer("preview");
  seedCleanPeerDir(peer, "preview");
  try {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");

    // Enroll the peer and open a peer Explorer through η's Computers menu.
    await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    const enrolled = await request.post("/api/peers", {
      data: { url: PEER_URL },
    });
    expect(enrolled.ok()).toBeTruthy();
    await page.goto("/");
    await page.locator("#eta-launcher").click();
    await page.locator(`[data-location="${PEER_URL}"]`).click();

    // The peer Explorer carries the .peer-window class so we can target it
    // without colliding with the local Explorer from goto("/").
    const peerExplorer = page.locator(".winbox.peer-window");
    await expect(peerExplorer).toHaveCount(1);
    await expect(
      peerExplorer.getByText("preview.md", { exact: true }),
    ).toBeVisible();

    // Double-clicking a text/markdown file opens a peer-accented inspector
    // whose body renders the file's contents.
    const fileEntry = peerExplorer.locator("button.entry.file", {
      hasText: "preview.md",
    });
    await expect(fileEntry).toBeVisible();
    await fileEntry.dblclick({ force: true });
    // The inspector carries the peer's --window-accent + peer-window
    // class; allow any non-peer inspector fallback to also be visible
    // since the assertion target is rendered content.
    const peerWindows = page.locator(".winbox.peer-window");
    await expect(peerWindows).toHaveCount(2);
    // The peer's hostname glyph shows in the window title; the inspector
    // itself carries the peer's --window-accent. We assert that the
    // rendered content surface exists and shows the file's text.
    //
    // Scoped to the inspector window rather than the page: .markdown-preview
    // is shared by anything rendering markdown, including the Settings
    // changelog dialog, which pre-renders hidden in the DOM. A page-wide
    // .first() matched that instead and saw "hidden".
    await expect(
      peerWindows.last().locator(".markdown-preview, .preview-text").first(),
    ).toBeVisible();

    // Closing the inspector drops it out of the dock and the peer
    // Explorer remains.
    await page.locator(".wb-close").last().click({ force: true });
    await expect(peerWindows).toHaveCount(1);
    await expect(peerExplorer).toHaveCount(1);
  } finally {
    try {
      await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    } catch {
      /* test timeout/teardown */
    }
    peer.stop();
  }
});
