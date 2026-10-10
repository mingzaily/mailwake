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
