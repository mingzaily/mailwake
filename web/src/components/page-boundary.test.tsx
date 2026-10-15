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

test.each([false, true])(
  "pending page announces loading in its full-screen context: %s",
  async (fullScreen) => {
    let resolve!: (module: { default: ComponentType }) => void;
    const page = new Promise<{ default: ComponentType }>((done) => {
      resolve = done;
    });
    const LazyPage = lazy(() => page);
    render(
      localized(
        <>
          {!fullScreen && <nav>Navigation</nav>}
          <PageBoundary fullScreen={fullScreen}>
            <LazyPage />
          </PageBoundary>
        </>,
      ),
    );
    if (fullScreen)
      expect(
        screen.getByRole("main").contains(screen.getByRole("status")),
      ).toBe(true);
    else expect(screen.getByText("Navigation")).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe(catalog["ui.loading"]);
    await act(async () => {
      resolve({ default: () => <h1>Loaded page</h1> });
      await page;
    });
    expect(screen.getByRole("heading", { name: "Loaded page" })).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.queryByRole("main")).toBeNull();
  },
);

test.each([false, true])(
  "failed page offers reload and recovers in its full-screen context: %s",
  async (fullScreen) => {
    const reload = vi
      .spyOn(window.location, "reload")
      .mockImplementation(() => {});
    vi.spyOn(console, "error").mockImplementation(() => {});
    const Broken = lazy(() => Promise.reject(new Error("private chunk URL")));
    const view = render(
      localized(
        <PageBoundary key="failed" fullScreen={fullScreen}>
          <Broken />
        </PageBoundary>,
      ),
    );
    expect((await screen.findByRole("alert")).textContent).toContain(
      catalog["ui.page_load_failed"],
    );
    expect(screen.queryByText("private chunk URL")).toBeNull();
    if (fullScreen)
      expect(screen.getByRole("main").contains(screen.getByRole("alert"))).toBe(
        true,
      );
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
    expect(screen.queryByRole("main")).toBeNull();
  },
);
