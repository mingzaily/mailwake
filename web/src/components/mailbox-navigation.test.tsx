import { afterEach, expect, test, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { I18nContext } from "@/lib/i18n";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import { Mailboxes } from "@/pages/mailboxes";
import { Shell } from "./shell";
import { FolderScanDialog } from "./folder-scan-dialog";
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
function mockAPI(failSave = false) {
  const fetchMock = vi
    .fn()
    .mockImplementation((path: string, options: RequestInit) => {
      const data = path.endsWith("/subscriptions")
        ? options.method === "PUT" && failSave
          ? { error: { code: "subscriptions_conflict", message: "配置已更新" } }
          : { revision: 1, folders: [{ name: "收件箱", check: "realtime" }] }
        : path.endsWith("/folders")
          ? { folders: ["INBOX", "Bank"] }
          : path.endsWith("/status")
            ? {
                folders: [
                  {
                    mailbox_id: mailbox.id,
                    mailbox_label: mailbox.label,
                    folder: "INBOX",
                    state: "watching",
                    check: "realtime",
                  },
                ],
                delivery: { pending: 0, dead: 0 },
              }
            : path.endsWith("/mailboxes")
              ? { mailboxes: [mailbox] }
              : mailbox;
      return Promise.resolve(
        new Response(JSON.stringify(data), {
          status: failSave && options.method === "PUT" ? 409 : 200,
        }),
      );
    });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
test("mailbox shortcuts belong under the clickable mailbox list entry", async () => {
  mockAPI();
  const { container } = mount(
    <Shell page="mailboxes" mailboxId={mailbox.id} logout={() => {}}>
      <Mailboxes />
    </Shell>,
  );
  await screen.findAllByText("工作邮箱");
  const navigation = container.querySelector('[data-slot="sidebar"] nav')!;
  for (const nav of container.querySelectorAll("nav")) {
    expect(within(nav).queryByRole("link", { name: "添加邮箱" })).toBeNull();
  }
  expect(
    screen.getByRole("link", { name: "添加邮箱" }).getAttribute("href"),
  ).toBe("#/mailboxes/new");
  expect(
    within(navigation as HTMLElement).getByRole("link", { name: "工作邮箱" }),
  ).toBeTruthy();
  expect(
    within(navigation as HTMLElement)
      .getByRole("link", { name: "邮箱" })
      .getAttribute("href"),
  ).toBe("#/mailboxes");
  expect(
    within(navigation as HTMLElement).queryByRole("link", { name: "全部邮箱" }),
  ).toBeNull();
});
test("mailbox opens a dedicated monitoring page with a separate settings link", async () => {
  mockAPI();
  mount(<Mailboxes id={mailbox.id} />);
  await screen.findByRole("heading", { name: "工作邮箱" });
  expect(
    screen.getByRole("link", { name: "监听状态" }).getAttribute("aria-current"),
  ).toBe("page");
  expect(
    screen.getByRole("link", { name: "邮箱设置" }).getAttribute("href"),
  ).toBe("#/mailboxes/mbx_work/settings");
  expect(screen.queryByLabelText("IMAP 地址")).toBeNull();
  expect(screen.queryByRole("checkbox")).toBeNull();
});
test("scan dialog scans folders, preserves edits on conflict, and cancels without saving", async () => {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
    function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  );
  vi.spyOn(HTMLDialogElement.prototype, "close").mockImplementation(function (
    this: HTMLDialogElement,
  ) {
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  });
  const fetchMock = mockAPI(true);
  mount(<Mailboxes id={mailbox.id} />);
  fireEvent.click(await screen.findByRole("button", { name: "扫描文件夹" }));
  const dialog = await screen.findByRole("dialog", { name: "扫描文件夹" });
  fireEvent.click(
    await within(dialog).findByRole("button", { name: "开始扫描" }),
  );
  fireEvent.click(
    await within(dialog).findByRole("checkbox", { name: "Bank" }),
  );
  fireEvent.change(within(dialog).getByLabelText("Bank 检查方式"), {
    target: { value: "15m" },
  });
  expect(within(dialog).getByText("第 2 / 3 步")).toBeTruthy();
  expect(within(dialog).queryByRole("button", { name: "确认保存" })).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
  expect(within(dialog).getByText("第 3 / 3 步")).toBeTruthy();
  expect(
    fetchMock.mock.calls.filter(([, options]) => options.method === "PUT"),
  ).toHaveLength(0);
  fireEvent.click(within(dialog).getByRole("button", { name: "确认保存" }));
  await within(dialog).findByText("配置已更新");
  fireEvent.click(within(dialog).getByRole("button", { name: "返回" }));
  expect(
    (within(dialog).getByLabelText("Bank 检查方式") as HTMLSelectElement).value,
  ).toBe("15m");
  expect(
    JSON.parse(
      fetchMock.mock.calls.find(([, options]) => options.method === "PUT")![1]
        .body,
    ),
  ).toEqual({
    revision: 1,
    folders: [
      { name: "收件箱", check: "realtime" },
      { name: "Bank", check: "15m" },
    ],
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(
    fetchMock.mock.calls.filter(([, options]) => options.method === "PUT"),
  ).toHaveLength(1);
});

test("mailbox settings contains connection inputs and omits the monitoring table", async () => {
  mockAPI();
  mount(<Mailboxes id={mailbox.id} view="settings" />);
  await screen.findByLabelText("IMAP 地址");
  expect(screen.queryByRole("columnheader", { name: "下次检查" })).toBeNull();
  expect(
    screen.getByRole("link", { name: "监听状态" }).getAttribute("href"),
  ).toBe("#/mailboxes/mbx_work");
});
test("saving scan selections closes the dialog and invalidates monitoring status", async () => {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
    function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  );
  vi.spyOn(HTMLDialogElement.prototype, "close").mockImplementation(function (
    this: HTMLDialogElement,
  ) {
    this.removeAttribute("open");
    this.dispatchEvent(new Event("close"));
  });
  const fetchMock = mockAPI();
  mount(<Mailboxes id={mailbox.id} />);
  fireEvent.click(await screen.findByRole("button", { name: "扫描文件夹" }));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).getByText("第 1 / 3 步")).toBeTruthy();
  expect(within(dialog).queryByRole("checkbox")).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "开始扫描" }));
  await within(dialog).findByRole("checkbox", { name: "收件箱 (INBOX)" });
  fireEvent.click(within(dialog).getByRole("button", { name: "下一步" }));
  fireEvent.click(
    await within(dialog).findByRole("button", { name: "确认保存" }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(
    fetchMock.mock.calls.filter(([path]) => path.endsWith("/status")).length,
  ).toBeGreaterThan(1);
});

function showScanDialog() {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
    function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  );
}
test("failed scans stay on step one and retry advances to selection", async () => {
  showScanDialog();
  const fetchMock = mockAPI();
  fetchMock.mockImplementationOnce(() =>
    Promise.resolve(new Response("<html>502</html>", { status: 502 })),
  );
  mount(
    <FolderScanDialog
      mailbox={{ ...mailbox, connections_in_use: 0 }}
      onClose={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await screen.findByText("无法连接 Mailwake，请检查网络或反向代理");
  expect(screen.getByText("第 1 / 3 步")).toBeTruthy();
  expect(screen.queryByRole("checkbox")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await screen.findByRole("checkbox", { name: "Bank" });
  expect(screen.getByText("第 2 / 3 步")).toBeTruthy();
});
test("closing during scanning aborts the read requests", async () => {
  showScanDialog();
  let signal: AbortSignal | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((_path, options) => {
      signal = options.signal;
      return new Promise((_resolve, reject) =>
        options.signal.addEventListener("abort", () =>
          reject(new DOMException("Aborted", "AbortError")),
        ),
      );
    }),
  );
  const { unmount } = mount(
    <FolderScanDialog
      mailbox={{ ...mailbox, connections_in_use: 0 }}
      onClose={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await screen.findByText("正在连接邮箱并扫描文件夹…");
  expect(
    screen.getByRole("button", { name: "取消" }).hasAttribute("disabled"),
  ).toBe(false);
  unmount();
  expect(signal?.aborted).toBe(true);
});
test("selection enforces the budget and empty confirmation explains stopping all folders", async () => {
  showScanDialog();
  mockAPI();
  mount(
    <FolderScanDialog
      mailbox={{ ...mailbox, connection_limit: 2, connections_in_use: 0 }}
      onClose={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: "Bank" }));
  expect(
    screen.getByRole("button", { name: "下一步" }).hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: "Bank" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "收件箱" }));
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  expect(screen.getByText("保存后将停止监听此邮箱的所有文件夹。")).toBeTruthy();
});

test("scan opens on its title and saving blocks Escape and close", async () => {
  showScanDialog();
  const fetchMock = mockAPI();
  const original = fetchMock.getMockImplementation()!;
  let finishSave: (response: Response) => void = () => {};
  fetchMock.mockImplementation((path, options) =>
    options.method === "PUT"
      ? new Promise<Response>((resolve) => {
          finishSave = resolve;
        })
      : original(path, options),
  );
  mount(
    <FolderScanDialog
      mailbox={{ ...mailbox, connections_in_use: 0 }}
      onClose={() => {}}
    />,
  );
  const dialog = screen.getByRole("dialog");
  expect(document.activeElement).toBe(
    within(dialog).getByRole("heading", { name: "扫描文件夹" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "开始扫描" }));
  await screen.findByRole("checkbox", { name: "收件箱 (INBOX)" });
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  fireEvent.click(screen.getByRole("button", { name: "确认保存" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "关闭" }).hasAttribute("disabled"),
    ).toBe(true),
  );
  const cancel = new Event("cancel", { bubbles: true, cancelable: true });
  fireEvent(dialog, cancel);
  expect(cancel.defaultPrevented).toBe(true);
  finishSave(new Response("{}"));
  await waitFor(() => expect(dialog.hasAttribute("open")).toBe(false));
});
