import { afterEach, expect, test, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { I18nContext } from "@/lib/i18n";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import { Overview } from "@/pages/overview";
import { Logs } from "@/pages/logs";
import { AuthScreen } from "./auth-screen";
import { AuthForm } from "./auth";
import { SetupWizard } from "./setup";
const mailbox = {
  id: "mbx_work",
  label: "工作邮箱",
  host: "imap.test",
  port: 993,
  username: "test",
  connection_limit: 10,
  revision: 1,
  password: { configured: true },
};
function mount(child: ReactNode) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <I18nContext.Provider
        value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
      >
        {child}
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function mockAPI(
  status: unknown = {},
  logs: unknown = { entries: [], cleared_through: 0, next: 0 },
) {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation((path: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              path.includes("/mailboxes")
                ? { mailboxes: [mailbox] }
                : path.includes("/status")
                  ? status
                  : path.includes("/logs")
                    ? logs
                    : path.includes("/deliveries")
                      ? { deliveries: [] }
                      : {},
            ),
          ),
        ),
      ),
  );
}
test("attention items name the failed delivery and affected mailbox/folder", async () => {
  mockAPI({
    channel: "bark",
    delivery: { dead: 1 },
    notices: {
      "mailbox:mbx_work": {
        code: "configuration_invalid",
        message: "配置无效",
      },
      "subscriptions:mbx_work": {
        code: "connection_budget_exceeded",
        message: "超出连接额度",
      },
    },
    folders: [
      {
        mailbox_id: "mbx_work",
        mailbox_label: "工作邮箱",
        folder: "INBOX",
        state: "auth_required",
        check: "realtime",
      },
    ],
  });
  mount(<Overview />);
  const failed = await screen.findByText("1 条通知投递失败");
  expect(failed.parentElement?.querySelector("a")?.getAttribute("href")).toBe(
    "#/deliveries",
  );
  expect(await screen.findByText("工作邮箱：配置无效")).toBeTruthy();
  expect(screen.getByText("工作邮箱：超出连接额度")).toBeTruthy();
  expect(screen.getByText(/工作邮箱 · INBOX：.*认证/)).toBeTruthy();
});
test("logs render rounded retry duration, mailbox labels and unknown IDs", async () => {
  mockAPI(
    {},
    {
      cleared_through: 0,
      next: 2,
      entries: [
        {
          seq: 1,
          time: "2026-09-29T01:02:03Z",
          level: "warn",
          message: "Reconnect",
          attrs: { backoff_seconds: 1.1309167, mailbox_id: "mbx_work" },
        },
        {
          seq: 2,
          time: "2026-09-29T01:02:04Z",
          level: "warn",
          message: "Unknown",
          attrs: { mailbox_id: "mbx_deleted" },
        },
      ],
    },
  );
  mount(<Logs />);
  expect(await screen.findByText("1.1 秒")).toBeTruthy();
  expect((await screen.findByTitle("mbx_work")).textContent).toBe("工作邮箱");
  expect(screen.getByText("mbx_deleted")).toBeTruthy();
});
test("setup shows step 1 of 3 and only the short password hint", () => {
  mount(<AuthForm setup onSuccess={() => {}} />);
  expect(screen.getByText("第 1 / 3 步")).toBeTruthy();
  expect(screen.getByText("至少 12 个字符")).toBeTruthy();
  expect(screen.queryByText(/1024/)).toBeNull();
});
test("skipping mailbox setup exits the three-step wizard", async () => {
  mockAPI();
  const done = vi.fn();
  mount(<SetupWizard done={done} />);
  expect(screen.getByText("第 2 / 3 步")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "暂时跳过" }));
  expect(done).toHaveBeenCalledOnce();
});
test("an oversized UTF-8 password shows the limit only after validation", async () => {
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  mount(<AuthForm setup onSuccess={() => {}} />);
  fireEvent.change(screen.getByLabelText("设置码"), {
    target: { value: "code" },
  });
  fireEvent.change(screen.getByLabelText("用户名"), {
    target: { value: "user" },
  });
  fireEvent.change(screen.getByLabelText("密码"), {
    target: { value: "邮".repeat(342) },
  });
  fireEvent.click(screen.getByRole("button", { name: "创建管理员" }));
  expect(await screen.findByText("密码最多 1024 个 UTF-8 字节")).toBeTruthy();
  expect(fetchMock).not.toHaveBeenCalled();
});

test("login landmarks separate account entry from appearance preferences", () => {
  mount(<AuthScreen setup={false} onSuccess={() => {}} />);
  const main = screen.getByRole("main");
  expect(
    within(main).getByRole("heading", { name: "登录 Mailwake", level: 1 }),
  ).toBeTruthy();
  expect(within(main).getByLabelText("用户名")).toBeTruthy();
  expect(within(main).getByLabelText("密码").getAttribute("autocomplete")).toBe(
    "current-password",
  );
  expect(within(main).queryByRole("combobox")).toBeNull();
  expect(screen.getByRole("button", { name: /^语言:/ })).toBeTruthy();
  expect(screen.getByRole("button", { name: /^主题:/ })).toBeTruthy();
});

test("logs localize summaries and keep original messages in collapsed technical details", async () => {
  mockAPI(
    {},
    {
      cleared_through: 0,
      next: 1,
      entries: [
        {
          seq: 1,
          time: "2026-10-10T01:00:00Z",
          level: "info",
          message: "Administrator login succeeded",
          attrs: { revision: 2 },
        },
      ],
    },
  );
  mount(<Logs />);
  expect(await screen.findByText("管理员登录成功")).toBeTruthy();
  const details = screen.getByText("技术详情").closest("details")!;
  expect(details.open).toBe(false);
  expect(details.textContent).toContain("Administrator login succeeded");
  expect(details.textContent).toContain('"revision": 2');
});
