import { readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";

// Authorize the exact CSS injected by the installed Sonner build.
const source = await readFile(
  new URL("../node_modules/sonner/dist/index.mjs", import.meta.url),
  "utf8",
);
const styles = [...source.matchAll(/__insertCSS\(("(?:[^"\\]|\\.)*")\);/g)].map(
  (match) => JSON.parse(match[1]),
);
if (styles.length !== 1)
  throw new Error(
    "Review Sonner CSS injection before building the CSP manifest",
  );
const hashes = styles.map(
  (css) => `sha256-${createHash("sha256").update(css).digest("base64")}`,
);
await writeFile(
  new URL("../../internal/httpapi/webdist/style-hashes.json", import.meta.url),
  JSON.stringify(hashes),
);
