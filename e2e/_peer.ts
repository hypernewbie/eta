// Shared helper: spawn an isolated second Eta instance and wait for its
// health endpoint. Used by every two-instance browser spec. Patterned
// after e2e/peers.spec.ts but factored out so each spec owns its
// fixture lifecycle.
//
// Note: the child process is detached and group-killed so `go run`
// does not leave stale compiled server children listening on the
// fixed test port. Each spec still needs to wire its own cleanup.
import { spawn } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export const PEER_PORT = 17081;
export const PEER_URL = `http://127.0.0.1:${PEER_PORT}`;

export interface PeerFixture {
  port: number;
  url: string;
  dir: string;
  cacheDir: string;
  pid: number | undefined;
  stop: () => void;
}

export async function waitForPeer(url: string, attempts = 100) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    try {
      const response = await fetch(`${url}/api/healthz`);
      if (response.ok) return;
    } catch {
      // Eta is still compiling or starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`peer Eta instance at ${url} did not start`);
}

export async function startPeer(name: string): Promise<PeerFixture> {
  const peerDir = `test-results/peer-${name}`;
  const peerCacheDir = `test-results/peer-${name}-cache`;
  rmSync(peerDir, { recursive: true, force: true });
  rmSync(peerCacheDir, { recursive: true, force: true });
  mkdirSync(peerDir, { recursive: true });
  mkdirSync(peerCacheDir, { recursive: true });
  const peer = spawn(
    "go",
    [
      "run",
      ".",
      "--ip",
      "127.0.0.1",
      "--port",
      String(PEER_PORT),
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
  await waitForPeer(PEER_URL);
  return {
    port: PEER_PORT,
    url: PEER_URL,
    dir: peerDir,
    cacheDir: peerCacheDir,
    pid: peer.pid,
    stop: () => {
      if (peer.pid) {
        try {
          process.kill(-peer.pid, "SIGTERM");
        } catch {
          // already gone
        }
      }
      rmSync(peerDir, { recursive: true, force: true });
      rmSync(peerCacheDir, { recursive: true, force: true });
    },
  };
}

// Populate a peer fixture dir with named text files. Used by tests that
// drive the browser to read files inside the peer Explorer.
export function writeTextFile(dir: string, relative: string, contents: string) {
  const full = join(dir, relative);
  mkdirSync(join(full, ".."), { recursive: true });
  writeFileSync(full, contents);
}

export function seedCleanPeerDir(fixture: PeerFixture, name: string) {
  rmSync(fixture.dir, { recursive: true, force: true });
  mkdirSync(fixture.dir, { recursive: true });
  writeTextFile(fixture.dir, `${name}.md`, `# ${name}\nHello from peer.\n`);
  writeTextFile(fixture.dir, `notes/${name}.txt`, `notes for ${name}\n`);
  writeTextFile(fixture.dir, `tree/inner/${name}.md`, `nested ${name}\n`);
}
