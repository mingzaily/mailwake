import { afterEach, expect, test } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Time } from "./common";
import { DeliveryTable } from "./status-tables";
import { I18nContext } from "@/lib/i18n";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import type { Delivery } from "@/lib/types";

const response = {
  deliveries: [
    {
      id: "known-time",
      state: "accepted",
      attempts: 1,
      created_at: "2026-09-29T01:02:03.456Z",
      accepted_at: "2026-09-29T01:02:04.789Z",
    },
  ] satisfies Delivery[],
};
afterEach(cleanup);
test("a delivery renders its exact ISO timestamp", () => {
  const { container } = render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nContext.Provider
        value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
      >
        <DeliveryTable deliveries={response.deliveries} />
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
  const time = container.querySelector("time")!;
  expect(time.dateTime).toBe("2026-09-29T01:02:04.789Z");
  expect(time.textContent).toBe(
    new Intl.DateTimeFormat("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: false,
    }).format(new Date(response.deliveries[0]!.accepted_at!)),
  );
  expect(time.title).toContain("2026");
});
test.each([1790643723456, 1790643723, 0])(
  "Time rejects numeric timestamp %s",
  (value) => {
    expect(() => render(<Time value={value as unknown as string} />)).toThrow(
      TypeError,
    );
  },
);
