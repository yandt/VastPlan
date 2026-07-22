import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { portalNoncePlaceholder } from "../assets/portal-assets";

export async function createPortalFixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "vastplan-portal-host-"));
  await mkdir(join(root, "assets"));
  await writeFile(join(root, "index.html"), `<script nonce="${portalNoncePlaceholder}"></script><div id="vastplan-portal" aria-live="polite"></div>`);
  await writeFile(join(root, "assets", "app.js"), "export const ready = true;\n");
	await mkdir(join(root, "assets", "access"));
	await writeFile(join(root, "assets", "access", "vastplan.svg"), portalFixtureLogo);
  return root;
}

export const portalFixtureLogo = '<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="#165dff"/></svg>';
