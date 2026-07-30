import { expect, test } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
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
    { stdio: "ignore" },
  );
  try {
    await waitForPeer();
    await request.delete(`/api/peers?url=${encodeURIComponent(peerURL)}`);
    const enrolled = await request.post("/api/peers", {
      data: { url: peerURL },
    });
    expect(enrolled.ok()).toBeTruthy();

    await page.goto("/");
    const launcher = page.locator(".peer-launcher");
    await expect(launcher).toHaveCount(1);
    await launcher.click();

    const remoteExplorer = page.locator(".winbox.peer-window");
    await expect(remoteExplorer).toHaveCount(1);
    await expect(remoteExplorer.getByText("remote.txt", { exact: true })).toBeVisible();
  } finally {
    await request.delete(`/api/peers?url=${encodeURIComponent(peerURL)}`);
    peer.kill("SIGTERM");
    rmSync(peerDir, { recursive: true, force: true });
    rmSync(peerCacheDir, { recursive: true, force: true });
  }
});
