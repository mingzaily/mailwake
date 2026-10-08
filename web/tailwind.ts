import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { compile } from "tailwindcss";
import { Scanner } from "@tailwindcss/oxide";
import type { Plugin } from "vite";

// Use Tailwind's MIT compiler and scanner directly. Its official Vite adapter
// additionally requires MPL-licensed Lightning CSS; Vite handles our CSS output.
export function tailwind(): Plugin {
  const require = createRequire(import.meta.url);
  let root = "";
  return {
    name: "mailwake-tailwind",
    enforce: "pre",
    configResolved(config) {
      root = config.root;
    },
    async transform(source, id) {
      if (!id.endsWith("/src/styles.css")) return;
      const fonts = source.match(/^@import "@fontsource[^\n]+/gm) ?? [];
      const css = source.replace(/^@import "@fontsource[^\n]+/gm, "");
      const compiler = await compile(css, {
        base: path.dirname(id),
        async loadStylesheet(name, base) {
          const file = require.resolve(
            name === "tailwindcss"
              ? "tailwindcss/index.css"
              : name.startsWith(".")
                ? path.resolve(base, name)
                : name,
          );
          return {
            path: file,
            base: path.dirname(file),
            content: await readFile(file, "utf8"),
          };
        },
      });
      const scanner = new Scanner({
        sources: [{ base: root, pattern: "src/**/*.{ts,tsx}", negated: false }],
      });
      return {
        code: fonts.join("\n") + "\n" + compiler.build(scanner.scan()),
        map: null,
      };
    },
    handleHotUpdate(context) {
      if (/\/src\/.*\.tsx?$/.test(context.file)) {
        for (const module of context.server.moduleGraph.getModulesByFile(
          path.join(root, "src/styles.css"),
        ) ?? [])
          context.server.moduleGraph.invalidateModule(module);
        context.server.ws.send({ type: "full-reload" });
      }
    },
  };
}
