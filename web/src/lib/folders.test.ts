import { expect, test } from "vitest";
import { folderLabel } from "./folders";
import zh from "../../../internal/i18n/locales/zh-CN.json";
const t = (key: string) => zh[key as keyof typeof zh];
test("server roles localize standard folders while custom paths keep their names", () => {
  const roles = { "[Gmail]/Drafts": "drafts", "Deleted Messages": "trash" };
  expect(folderLabel("[Gmail]/Drafts", roles, t)).toBe("草稿箱");
  expect(folderLabel("Deleted Messages", roles, t)).toBe("已删除");
  expect(folderLabel("INBOX", {}, t)).toBe("收件箱");
  expect(folderLabel("Drafts", {}, t)).toBe("草稿箱");
  expect(folderLabel("Projects/CN", roles, t)).toBe("Projects/CN");
});

test.each([
  ["Deleted Messages", "已删除"],
  ["Drafts", "草稿箱"],
  ["Junk", "垃圾箱"],
  ["Sent Messages", "已发送"],
  ["inbox", "收件箱"],
  ["Projects/Drafts", "Projects/Drafts"],
  ["收件箱", "收件箱"],
])(
  "common folder names translate without server roles: %s",
  (name, expected) => {
    expect(folderLabel(name, {}, t)).toBe(expected);
  },
);

test.each([
  ["en", "Inbox"],
  ["zh-CN", "收件箱"],
  ["zh-Hant", "收件匣"],
  ["ja", "受信トレイ"],
  ["ko", "받은편지함"],
  ["de", "Posteingang"],
  ["fr", "Boîte de réception"],
  ["es", "Bandeja de entrada"],
  ["pt-BR", "Caixa de entrada"],
])(
  "standard roles use the %s catalog and preserve custom paths",
  async (code, inbox) => {
    const { readFileSync } = await import("node:fs");
    const catalog: Record<string, string> = JSON.parse(
      readFileSync(`../internal/i18n/locales/${code}.json`, "utf8"),
    );
    const translate = (key: string) => catalog[key];
    expect(folderLabel("INBOX", {}, translate)).toBe(inbox);
    expect(
      folderLabel(
        "[Provider]/special",
        { "[Provider]/special": "inbox" },
        translate,
      ),
    ).toBe(inbox);
    expect(folderLabel("其他文件夹/JP区域", {}, translate)).toBe(
      "其他文件夹/JP区域",
    );
    expect(folderLabel("Projects/Drafts", {}, translate)).toBe(
      "Projects/Drafts",
    );
  },
);
