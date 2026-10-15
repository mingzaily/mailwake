import { afterEach, expect, test } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nContext } from "@/lib/i18n";
import { FolderDisplayContext, FolderName } from "./folder-name";
import { FolderSelection } from "./folder-selection";
import type { Folder } from "@/lib/types";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import type { ReactNode } from "react";
function show(children: ReactNode) {
  return render(
    <I18nContext.Provider
      value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
    >
      {children}
    </I18nContext.Provider>,
  );
}
afterEach(cleanup);
test("folder names display leaves and full paths while selection sends original identities", () => {
  const names = {
    "其他文件夹/A区域": "A区域",
    "项目/A区域": "A区域",
    "[Gmail]/重要": "重要",
  };
  let saved: Folder[] = [];
  show(
    <FolderSelection
      folders={[]}
      available={Object.keys(names)}
      names={names}
      onChange={(value) => {
        saved = value;
      }}
    />,
  );
  expect(screen.getByText("重要")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("checkbox", { name: "A区域 (其他文件夹/A区域)" }),
  );
  expect(saved).toEqual([{ name: "其他文件夹/A区域", check: "5m" }]);
  expect(
    screen.getByRole("checkbox", { name: "A区域 (项目/A区域)" }),
  ).toBeTruthy();
  expect(
    screen.getByRole("combobox", { name: "A区域 (其他文件夹/A区域) 检查方式" }),
  ).toBeTruthy();
});
test("shared mailbox presentation stays isolated and original paths remain visible", () => {
  const mailboxes = [
    { id: "one", folder_display: { folder_names: { "A.B": "B" } } },
    { id: "two", folder_display: { folder_names: { "A.B": "A.B" } } },
  ];
  show(
    <FolderDisplayContext.Provider value={mailboxes}>
      <FolderName name="A.B" mailboxId="one" />
      <FolderName name="A.B" mailboxId="two" />
      <FolderName name="顶层" />
      <FolderName name="Literal/Slash" />
    </FolderDisplayContext.Provider>,
  );
  expect(screen.getByText("B")).toBeTruthy();
  expect(screen.getByText("(A.B)")).toBeTruthy();
  expect(screen.getByText("A.B", { exact: true })).toBeTruthy();
  expect(screen.getByText("顶层").textContent).toBe("顶层");
  expect(screen.getByText("Literal/Slash").textContent).toBe("Literal/Slash");
});

test("monitoring uses the same saved names and roles as folder selection", async () => {
  const { FolderTable } = await import("./status-tables");
  const mailbox = {
    id: "mail",
    folder_display: {
      folder_names: { "其他文件夹/A区域": "A区域", "[Gmail]/Drafts": "Drafts" },
      folder_roles: { "[Gmail]/Drafts": "drafts" },
    },
  };
  show(
    <FolderDisplayContext.Provider value={[mailbox]}>
      <FolderTable
        folders={[
          {
            mailbox_id: "mail",
            mailbox_label: "Mail",
            folder: "其他文件夹/A区域",
            state: "watching",
            check: "5m",
          },
          {
            mailbox_id: "mail",
            mailbox_label: "Mail",
            folder: "[Gmail]/Drafts",
            state: "watching",
            check: "5m",
          },
        ]}
      />
    </FolderDisplayContext.Provider>,
  );
  expect(screen.getByText("A区域")).toBeTruthy();
  expect(screen.getByText("(其他文件夹/A区域)")).toBeTruthy();
  expect(screen.getByText("草稿箱")).toBeTruthy();
  expect(screen.getByText("([Gmail]/Drafts)")).toBeTruthy();
});
