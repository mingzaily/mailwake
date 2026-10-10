import { afterEach, expect, test } from "vitest";
import {
  cleanup,
  render,
  screen,
  fireEvent,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DeliveryTable } from "./status-tables";
import { I18nContext } from "@/lib/i18n";
import type { Delivery } from "@/lib/types";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import response from "../../../internal/httpapi/testdata/delivery-message.json";
afterEach(cleanup);
function mount(deliveries: Delivery[]) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nContext.Provider
        value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
      >
        <DeliveryTable deliveries={deliveries} />
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
}
test("SQLite-backed delivery response shows subject and original mailbox/folder", () => {
  mount(response.deliveries);
  expect(screen.getByText("季度报告已更新").getAttribute("title")).toBe(
    "季度报告已更新",
  );
  expect(screen.getByRole("columnheader", { name: "操作" }).textContent).toBe(
    "操作",
  );
  expect(screen.getByText("工作邮箱").getAttribute("title")).toBe("mbx_work");
  expect(screen.getByText("Clients")).toBeTruthy();
  expect(screen.queryByText("message-contract")).toBeNull();
});
test("distinguishes cleared history, private titles, empty subjects and test notifications", () => {
  const base = response.deliveries[0];
  const { message, ...legacy } = base;
  mount([
    { ...legacy, id: "legacy" },
    { ...base, id: "private", message: { ...message, subject: null } },
    { ...base, id: "empty", message: { ...message, subject: "" } },
    {
      ...base,
      id: "test",
      message: {
        ...message,
        test: true,
        mailbox_label: "",
        folder: "",
        subject: null,
      },
    },
  ]);
  expect(screen.getAllByText("标题未保留")).toHaveLength(2);
  expect(screen.getByText("来源未保留")).toBeTruthy();
  expect(screen.getByText("（无主题）")).toBeTruthy();
  expect(screen.getByText("测试通知")).toBeTruthy();
});

test("details reveal the full delivery ID, error and device result", () => {
  mount([
    {
      ...response.deliveries[0],
      devices: [
        {
          pairing_id: "phone-1",
          device_name: "iPhone 17",
          status: "dead",
          error_code: "native_target_unavailable",
        },
      ],
      last_error: {
        code: "native_target_unavailable",
        message: "完整的错误详情",
      },
    },
  ]);
  expect(screen.queryByText("iPhone 17")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "详情" }));
  const dialog = screen.getByRole("dialog");
  expect(within(dialog).getByText("message-contract")).toBeTruthy();
  expect(within(dialog).getByText("完整的错误详情")).toBeTruthy();
  expect(within(dialog).getByText("iPhone 17")).toBeTruthy();
  fireEvent.click(within(dialog).getByRole("button", { name: "关闭" }));
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("column resizing follows pointer movement and enforces the minimum width", () => {
  mount(response.deliveries);
  const table = screen.getByRole("table");
  const headings = screen.getAllByRole("columnheader");
  const widths = [260, 180, 130, 100, 260, 180, 176];
  headings.forEach((heading, index) => {
    heading.getBoundingClientRect = () => ({ width: widths[index] }) as DOMRect;
  });
  const handle = screen.getByRole("button", { name: "调整邮件标题列宽" });
  handle.setPointerCapture = () => {};
  fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 260 });
  fireEvent.pointerMove(handle, { pointerId: 1, clientX: 320 });
  expect(table.querySelector("col")?.style.width).toBe("320px");
  fireEvent.pointerMove(handle, { pointerId: 1, clientX: -500 });
  expect(table.querySelector("col")?.style.width).toBe("120px");
  fireEvent.pointerUp(handle, { pointerId: 1 });
  fireEvent.pointerMove(handle, { pointerId: 1, clientX: 500 });
  expect(table.querySelector("col")?.style.width).toBe("120px");
});
