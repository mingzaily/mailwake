import { expect, test } from "vitest";
import { folderLabel } from "./folders";
import zh from "../../../internal/i18n/locales/zh-CN.json";
const t = (key: string) => zh[key as keyof typeof zh];
test("server roles localize standard folders while custom paths keep their names", () => {
  const roles = { "[Gmail]/Drafts": "drafts", "Deleted Messages": "trash" };
  expect(folderLabel("[Gmail]/Drafts", roles, t)).toBe("草稿箱");
  expect(folderLabel("Deleted Messages", roles, t)).toBe("已删除");
  expect(folderLabel("INBOX", {}, t)).toBe("收件箱");
  expect(folderLabel("Drafts", {}, t)).toBe("Drafts");
  expect(folderLabel("Projects/CN", roles, t)).toBe("Projects/CN");
});
