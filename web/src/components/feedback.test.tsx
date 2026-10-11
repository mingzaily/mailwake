import { afterEach, expect, test, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { I18nContext } from "@/lib/i18n";
import { APIError } from "@/lib/api";
import catalog from "../../../internal/i18n/locales/en.json";
import { ErrorNotice, ToastProvider, useActionError, useToast } from "./common";
function Actions() {
  const notify = useToast();
  const [error, fail] = useActionError();
  return (
    <>
      <button onClick={() => notify("Saved")}>Save</button>
      <button
        onClick={() =>
          fail(
            new APIError(
              429,
              { code: "rate_limited", message: "Too many requests" },
              30,
            ),
          )
        }
      >
        Fail
      </button>
      <button
        onClick={() =>
          fail(
            new APIError(409, {
              code: "settings_conflict",
              message: "Configuration changed",
            }),
          )
        }
      >
        Conflict
      </button>
      <ErrorNotice error={error} reload={() => fail(undefined)} />
    </>
  );
}
function mount() {
  render(
    <I18nContext.Provider
      value={{ language: "en", catalog, setLanguage: () => {} }}
    >
      <ToastProvider>
        <Actions />
      </ToastProvider>
    </I18nContext.Provider>,
  );
}
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
test("success feedback is announced and can be dismissed", async () => {
  mount();
  fireEvent.click(screen.getByText("Save"));
  expect(await screen.findByText("Saved")).toBeTruthy();
  expect(screen.getByRole("region").getAttribute("aria-live")).toBe("polite");
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  await waitFor(() => expect(screen.queryByText("Saved")).toBeNull());
});
test("operation errors expose the retry delay", async () => {
  mount();
  fireEvent.click(screen.getByText("Fail"));
  expect(await screen.findByText(/Too many requests/)).toBeTruthy();
  expect(screen.getByRole("region").textContent).toContain("30");
});
test("configuration conflicts remain visible until the reload action resolves them", () => {
  vi.useFakeTimers();
  mount();
  fireEvent.click(screen.getByText("Conflict"));
  act(() => vi.advanceTimersByTime(10000));
  expect(screen.getByRole("alert").textContent).toContain(
    "Configuration changed",
  );
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.reload"] }));
  expect(screen.queryByRole("alert")).toBeNull();
});
