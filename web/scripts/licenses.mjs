import { readFile, readdir, writeFile } from "node:fs/promises";
const root = new URL("../", import.meta.url);
const lock = JSON.parse(
  await readFile(new URL("package-lock.json", root), "utf8"),
);
const sections = [
  "Mailwake Core frontend third-party notices\nBundled dependencies retain their respective licenses.\nGeist and Geist Mono fonts: SIL Open Font License 1.1.\n",
];
for (const [location, metadata] of Object.entries(lock.packages).sort()) {
  if (!location) continue;
  const license = metadata.license ?? "";
  if (
    !/^(MIT|ISC|Apache-2\.0|BSD-[234]-Clause|0BSD|OFL-1\.1|BlueOak-1\.0\.0|\(|\)| AND | OR )+$/.test(
      license,
    )
  )
    throw new Error(`Review license: ${location}: ${license}`);
  const directory = new URL(`${location}/`, root);
  let files;
  try {
    files = await readdir(directory);
  } catch {
    continue;
  } // Platform-specific optional packages.
  // Include build tools as well, to make the source distribution audit reproducible.
  sections.push(`\n${location} ${metadata.version}\nLicense: ${license}\n`);
  for (const name of files.filter((name) =>
    /^(licen[sc]e|copying|notice|ofl)(\.|$)/i.test(name),
  )) {
    try {
      sections.push(await readFile(new URL(name, directory), "utf8"));
    } catch {
      /* License directory is covered by package metadata. */
    }
  }
}
sections.push(await readFile(new URL("SHADCN-LICENSE", root), "utf8"));
await writeFile(
  new URL("../internal/httpapi/webdist/third-party-notices.txt", root),
  sections.join("\n"),
);

await writeFile(new URL("../internal/httpapi/webdist/.gitkeep", root), "");
