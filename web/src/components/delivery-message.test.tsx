import { afterEach, expect, test } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
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
  expect(screen.getByText("message-contract").getAttribute("title")).toBe(
    "message-contract",
  );
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
