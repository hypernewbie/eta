import { expect, test } from "./_setup";
import { spawn } from "node:child_process";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const peerPort = 17081;
const peerURL = `http://127.0.0.1:${peerPort}`;
const peerDir = "test-results/peer-e2e";
const peerCacheDir = "test-results/peer-e2e-cache";

async function waitForPeer() {
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const response = await fetch(`${peerURL}/api/healthz`);
      if (response.ok) return;
    } catch {
      // Eta is still compiling or starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("peer Eta instance did not start");
}

test("enrolled peer opens as a source-aware remote Explorer", async ({
  page,
  request,
}) => {
  rmSync(peerDir, { recursive: true, force: true });
  rmSync(peerCacheDir, { recursive: true, force: true });
  mkdirSync(peerDir, { recursive: true });
  mkdirSync(peerCacheDir, { recursive: true });
  mkdirSync(join(peerDir, "destination"));
  writeFileSync(join(peerDir, "remote.txt"), "remote eta fixture\n");
  const peer = spawn(
    "go",
    [
      "run",
      ".",
      "--ip",
      "127.0.0.1",
      "--port",
      String(peerPort),
      "--root",
      peerDir,
      "--identity-file",
      join(peerCacheDir, "identity.json"),
      "--state-file",
      join(peerCacheDir, "state.json"),
      "--peers-file",
      join(peerCacheDir, "peers.json"),
      "--thumbnail-cache-dir",
      join(peerCacheDir, "thumbnails"),
      "--remote-cache-dir",
      join(peerCacheDir, "remote-cache"),
      "--transfer-dir",
      join(peerCacheDir, "transfers"),
    ],
    { stdio: "ignore", detached: true },
  );
  try {
    await waitForPeer();
    await request.delete(`/api/peers?url=${encodeURIComponent(peerURL)}`);
    const enrolled = await request.post("/api/peers", {
      data: { url: peerURL },
    });
    expect(enrolled.ok()).toBeTruthy();

    // Push empty state right before goto so the prior test's
    // pagehide beacon (racy with our _setup.ts beforeEach reset)
    // can't pin the local Explorer at a stale path. The local
    // explorer must open at root so README.md is visible for the
    // copy-and-paste cross-instance sequence below.
    await request.put("/api/state", {
      data: { version: 1, windows: [] },
    });
    await page.goto("/");
    await page.locator("#eta-launcher").click();
    const launcher = page.locator(`[data-location="${peerURL}"]`);
    await expect(launcher).toHaveCount(1);
    await launcher.click();

    const remoteExplorer = page.locator(".winbox.peer-window");
    await expect(remoteExplorer).toHaveCount(1);
    await expect(remoteExplorer.getByText("remote.txt", { exact: true })).toBeVisible();
    await remoteExplorer.getByText("destination", { exact: true }).click({ button: "right" });
    await page.locator('[data-file-action="terminal"]').click();
    const remoteTerminal = page.locator(".winbox.peer-window").last();
    await expect(remoteTerminal.locator(".terminal-xterm .xterm")).toBeVisible();
    await remoteTerminal.locator(".wb-close").click({ force: true });

    const localExplorer = page.locator(".winbox.eta-window:not(.peer-window)").last();
    await page.locator("#task-strip .task-button").filter({ hasText: "Explorer" }).first().click();
    await localExplorer.getByText("README.md", { exact: true }).click({ button: "right" });
    await page.locator('[data-file-action="copy"]').click();
    await page.locator("#task-strip .task-button").filter({ hasText: "Explorer" }).last().click();
    await remoteExplorer.getByText("destination", { exact: true }).click({ button: "right" });
    await page.locator('[data-file-action="paste"]').click();
    const copyTask = page.locator(".copy-task");
    await expect(copyTask).toContainText("Copy README.md");
    await page.waitForTimeout(750);
    const copyError = await copyTask.getAttribute("title");
    if (copyError) throw new Error(`copy task failed: ${copyError}`);
    await expect.poll(() => existsSync(join(peerDir, "destination", "README.md")), { timeout: 10_000 }).toBeTruthy();
    await remoteExplorer.getByText("destination", { exact: true }).dblclick();
    await expect(remoteExplorer.getByText("README.md", { exact: true })).toBeVisible();
    await remoteExplorer.locator(".wb-close").click({ force: true });
  } finally {
    try { await request.delete(`/api/peers?url=${encodeURIComponent(peerURL)}`); } catch { /* test timeout/teardown */ }
    if (peer.pid) process.kill(-peer.pid, "SIGTERM");
    rmSync(peerDir, { recursive: true, force: true });
    rmSync(peerCacheDir, { recursive: true, force: true });
  }
});
