import { expect, test } from "vitest";
import { readFileSync } from "node:fs";
test("entry has external assets and a viewport", () => {
  const html = readFileSync("index.html", "utf8");
  expect(html).toContain('name="viewport"');
  expect(html).not.toMatch(/<style|\sstyle=|<script(?![^>]*src=)/);
});
