import { afterEach, expect, test } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nContext } from "@/lib/i18n";
import type { Mailbox, Status } from "@/lib/types";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import { Budget, Shell } from "./shell";

const mailboxes: Mailbox[] = [
  { id: "work", label: "工作邮箱", connection_limit: 10 },
  { id: "personal", label: "个人邮箱", connection_limit: 5 },
].map((box) => ({
  ...box,
  revision: 1,
  host: "imap.test",
  port: 993,
  username: box.id,
  password: { configured: true },
  connections_in_use: 0,
}));
const status: Status = {
  folders: [
    {
      mailbox_id: "work",
      mailbox_label: "工作邮箱",
      folder: "INBOX",
      check: "realtime",
      state: "watching",
    },
    {
      mailbox_id: "personal",
      mailbox_label: "个人邮箱",
      folder: "INBOX",
      check: "realtime",
      state: "watching",
    },
    {
      mailbox_id: "personal",
      mailbox_label: "个人邮箱",
      folder: "Bank",
      check: "5m",
      state: "scheduled",
    },
    {
      mailbox_id: "personal",
      mailbox_label: "个人邮箱",
      folder: "News",
      check: "15m",
      state: "scheduled",
    },
  ],
  delivery: { pending: 0, accepted: 0, dead: 0 },
  channel: "bark",
  notices: {},
};
function mount(page: string, mailboxId?: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity } },
  });
  client.setQueryData(["mailboxes", "zh-CN"], { mailboxes });
  client.setQueryData(["status", "zh-CN"], status);
  const { container } = render(
    <QueryClientProvider client={client}>
      <I18nContext.Provider
        value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
      >
        <Shell page={page} mailboxId={mailboxId} logout={() => {}}>
          Content
        </Shell>
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
  return Array.from(
    container.querySelectorAll<HTMLElement>('[data-slot="sidebar-footer"]'),
  );
}
afterEach(cleanup);

test.each([
  ["mailboxes", "work"],
  ["overview", undefined],
  ["notifications", undefined],
  ["deliveries", "personal"],
  ["logs", undefined],
  ["settings", undefined],
  ["diagnostics", undefined],
  ["mailboxes", undefined],
  ["mailboxes", "new"],
  ["mailboxes", "missing"],
])(
  "both sidebars keep connection budgets in page content: %s / %s",
  (page, id) => {
    const footers = mount(page, id);
    expect(footers).toHaveLength(2);
    for (const footer of footers) {
      expect(within(footer).queryByText("连接额度")).toBeNull();
      expect(
        within(footer).getByRole("button", { name: "账户菜单", hidden: true }),
      ).toBeTruthy();
    }
  },
);

test("budget help opens a popover explaining the provider limit", () => {
  render(
    <I18nContext.Provider
      value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
    >
      <Budget folders={[]} limit={10} />
    </I18nContext.Provider>,
  );
  const trigger = screen.getByRole("button", {
    name: "连接额度说明",
    hidden: true,
  });
  expect(trigger.getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByText(/邮件服务商会限制/)).toBeNull();
  fireEvent.click(trigger);
  expect(trigger.getAttribute("aria-expanded")).toBe("true");
  const help = screen.getByText(/邮件服务商会限制/);
  expect(help.closest('[data-slot="popover-content"]')).toBeTruthy();
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  expect(screen.queryByText(/邮件服务商会限制/)).toBeNull();
});

test("budget help renders inside a native dialog so it stays visible", () => {
  render(
    <I18nContext.Provider
      value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
    >
      <dialog open data-testid="scan-dialog">
        <Budget folders={[]} limit={10} />
      </dialog>
    </I18nContext.Provider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "连接额度说明" }));
  const help = screen.getByText(/邮件服务商会限制/);
  expect(screen.getByTestId("scan-dialog").contains(help)).toBe(true);
});

test("mailbox breadcrumbs link back to the mailbox list", () => {
  mount("mailboxes", "work");
  const breadcrumb = screen.getByRole("navigation", { name: "当前位置" });
  expect(
    within(breadcrumb).getByRole("link", { name: "邮箱" }).getAttribute("href"),
  ).toBe("#/mailboxes");
  expect(
    within(breadcrumb).getByText("工作邮箱").getAttribute("aria-current"),
  ).toBe("page");
});

test("GitHub is available above the account menu in both sidebars", () => {
  const footers = mount("settings");
  for (const footer of footers) {
    const link = within(footer).getByRole("link", {
      name: "GitHub",
      hidden: true,
    });
    expect(link.getAttribute("href")).toBe(
      "https://github.com/mingzaily/mailwake",
    );
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  }
});
