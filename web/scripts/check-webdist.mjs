import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../../", import.meta.url));
const directory = "internal/httpapi/webdist";
const tracked = execFileSync("git", ["ls-files", "-z", "--", directory], {
  cwd: root,
  encoding: "utf8",
})
  .split("\0")
  .filter(Boolean);
if (tracked.length !== 1 || tracked[0] !== `${directory}/.gitkeep`) {
  throw new Error(
    "webdist must track only .gitkeep; keep the fallback page outside generated assets",
  );
}
execFileSync("git", ["diff", "--exit-code", "--", directory], {
  cwd: root,
  stdio: "inherit",
});
console.log("webdist tracked files unchanged after build");
