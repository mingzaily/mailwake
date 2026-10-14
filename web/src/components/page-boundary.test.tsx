import { lazy, type ReactNode, type ComponentType } from "react";
import { afterEach, expect, test, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { I18nContext } from "@/lib/i18n";
import catalog from "../../../internal/i18n/locales/en.json";
import { PageBoundary } from "./page-boundary";

function localized(children: ReactNode) {
  return (
    <I18nContext.Provider
      value={{ language: "en", catalog, setLanguage: () => {} }}
    >
      {children}
    </I18nContext.Provider>
  );
}
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

test("pending page keeps surrounding navigation visible and announces loading", async () => {
  let resolve!: (module: { default: ComponentType }) => void;
  const page = new Promise<{ default: ComponentType }>((done) => {
    resolve = done;
  });
  const LazyPage = lazy(() => page);
  render(
    localized(
      <>
        <nav>Navigation</nav>
        <PageBoundary>
          <LazyPage />
        </PageBoundary>
      </>,
    ),
  );
  expect(screen.getByText("Navigation")).toBeTruthy();
  expect(screen.getByRole("status").textContent).toBe(catalog["ui.loading"]);
  await act(async () => {
    resolve({ default: () => <h1>Loaded page</h1> });
    await page;
  });
  expect(screen.getByRole("heading", { name: "Loaded page" })).toBeTruthy();
  expect(screen.queryByRole("status")).toBeNull();
});

test("failed page offers explicit reload and navigation can recover independently", async () => {
  const reload = vi
    .spyOn(window.location, "reload")
    .mockImplementation(() => {});
  vi.spyOn(console, "error").mockImplementation(() => {});
  const Broken = lazy(() => Promise.reject(new Error("private chunk URL")));
  const view = render(
    localized(
      <PageBoundary key="failed">
        <Broken />
      </PageBoundary>,
    ),
  );
  expect((await screen.findByRole("alert")).textContent).toContain(
    catalog["ui.page_load_failed"],
  );
  expect(screen.queryByText("private chunk URL")).toBeNull();
  expect(reload).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.reload"] }));
  expect(reload).toHaveBeenCalledOnce();
  view.rerender(
    localized(
      <PageBoundary key="another-page">
        <h1>Another page</h1>
      </PageBoundary>,
    ),
  );
  expect(screen.getByRole("heading", { name: "Another page" })).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});
