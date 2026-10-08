import { afterEach, expect, test, vi } from "vitest";
import { format, loadCatalog } from "./i18n";
afterEach(() => vi.unstubAllGlobals());
test("placeholder values are replaced once as literal text", () => {
  expect(
    format({ title: "{name}: {count}" }, "title", {
      name: "<script>{count}",
      count: 2,
    }),
  ).toBe("<script>{count}: 2");
});
test("initial catalog uses the server Accept-Language negotiation", async () => {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ "ui.save": "保存" }), {
      headers: { "Content-Language": "zh-CN" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  expect(await loadCatalog()).toEqual({
    language: "zh-CN",
    catalog: { "ui.save": "保存" },
  });
  expect(fetchMock).toHaveBeenCalledWith("/locales/default.json");
});

test("all static UI keys resolve in the single Go catalog", async () => {
  const { globSync, readFileSync } = await import("node:fs");
  const catalog = JSON.parse(
    readFileSync("../internal/i18n/locales/en.json", "utf8"),
  );
  for (const file of globSync("src/**/*.{ts,tsx}")) {
    if (file.includes(".test.")) continue;
    const source = readFileSync(file, "utf8");
    for (const match of source.matchAll(/["'](ui\.[\w-]+)["']/g))
      expect(catalog[match[1]], `${file}: ${match[1]}`).toBeTruthy();
  }
});
