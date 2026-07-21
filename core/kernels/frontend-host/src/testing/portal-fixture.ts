import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { portalNoncePlaceholder } from "../assets/portal-assets";

export async function createPortalFixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "vastplan-portal-host-"));
  await mkdir(join(root, "assets"));
  await writeFile(join(root, "index.html"), `<script nonce="${portalNoncePlaceholder}"></script><div id="vastplan-portal" aria-live="polite"></div>`);
  await writeFile(join(root, "assets", "app.js"), "export const ready = true;\n");
  return root;
}
