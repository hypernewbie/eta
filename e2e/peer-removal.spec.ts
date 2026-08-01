// Browser coverage for the peer inventory lifecycle: enrolling a peer via
// /api/peers makes it appear in η's Computers menu; removing it via the
// same endpoint removes the menu entry.
//
// Drives both transitions through the API (no peer UI control exists yet)
// and asserts on the visible menu state. This is the "peer removal"
// browser coverage called out in journal step #1.
import { expect, test } from "./_setup";
import { PEER_URL, startPeer, writeTextFile } from "./_peer";

test("η menu removes a peer once its inventory entry is deleted @peer", async ({
  page,
  request,
}) => {
  const peer = await startPeer("removal");
  writeTextFile(peer.dir, "hello.md", "hi from peer\n");
  try {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");

    // η menu shows only the local host before enrollment.
    await page.locator("#eta-launcher").click();
    await expect(page.locator('[data-location="local"]')).toBeVisible();
    await expect(page.locator(`[data-location="${PEER_URL}"]`)).toHaveCount(0);
    await page.keyboard.press("Escape");
    await page.locator("#eta-menu").evaluate((menu) => ((menu as HTMLElement).hidden = true));

    // Enroll and reload; the peer should appear. (Reload is required
    // until web/app.ts refreshes enrolledPeers on inventory mutation;
    // task A adds the coverage, not the live-refresh behaviour.)
    await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    const enrolled = await request.post("/api/peers", { data: { url: PEER_URL } });
    expect(enrolled.ok()).toBeTruthy();
    await page.goto("/");

    await page.locator("#eta-launcher").click();
    const peerEntry = page.locator(`[data-location="${PEER_URL}"]`);
    await expect(peerEntry).toBeVisible();
    await page.keyboard.press("Escape");
    await page.locator("#eta-menu").evaluate((menu) => ((menu as HTMLElement).hidden = true));

    // Remove and reload; menu should drop back to local-only.
    const removed = await request.delete(
      `/api/peers?url=${encodeURIComponent(PEER_URL)}`,
    );
    expect(removed.ok()).toBeTruthy();
    await page.goto("/");

    await page.locator("#eta-launcher").click();
    await expect(peerEntry).toHaveCount(0);
    await expect(page.locator('[data-location="local"]')).toBeVisible();
  } finally {
    try {
      await request.delete(`/api/peers?url=${encodeURIComponent(PEER_URL)}`);
    } catch {
      /* ignore */
    }
    peer.stop();
  }
});
