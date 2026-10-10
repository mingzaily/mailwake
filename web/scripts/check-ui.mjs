import { readFile, readdir } from "node:fs/promises";
const root = new URL("../src/", import.meta.url);
const css = await readFile(new URL("styles.css", root), "utf8");
if (
  /\[data-slot\s*=|\.(?:auth|wizard|spinner)\b|(?:^|\n)[^\n{]*\bselect\s*\{/m.test(
    css,
  )
)
  throw new Error("Global component CSS override");
const nativeDialogs = new Set([
  "components/folder-scan-dialog.tsx",
  "components/native-devices.tsx",
  "components/confirm-action-dialog.tsx",
  "components/shell.tsx",
  "components/delivery-details.tsx",
]);
async function check(directory, prefix = "") {
  for (const item of await readdir(directory, { withFileTypes: true })) {
    const name = prefix + item.name;
    const path = new URL(item.name, directory);
    if (item.isDirectory()) {
      await check(new URL(`${item.name}/`, directory), name + "/");
      continue;
    }
    if (!name.endsWith(".tsx") || name.includes(".test.")) continue;
    const source = await readFile(path, "utf8");
    if (/<select\b/.test(source) && name !== "components/ui/native-select.tsx")
      throw new Error(`Use NativeSelect: ${name}`);
    if (/<dialog\b/.test(source) && !nativeDialogs.has(name))
      throw new Error(`Review CSP exception for dialog: ${name}`);
    if (/type=["']checkbox["']|\bspace-y-|\bbg-[a-z]+-\d{2,3}\b/.test(source))
      throw new Error(`Unmigrated control or styling: ${name}`);
  }
}
await check(root);
console.log(
  "UI audit passed: NativeSelect primitive + 5 documented CSP dialog exceptions",
);
