import { afterEach, expect, test, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nContext } from "./lib/i18n";
import catalog from "../../internal/i18n/locales/en.json";
import { ConsolePage } from "./console-page";

vi.mock("./pages/overview", () => ({ Overview: () => <h1>Overview page</h1> }));
vi.mock("./pages/mailboxes", () => ({
  Mailboxes: ({ id, view }: { id?: string; view?: string }) => (
    <>
      <h1>
        Mailbox {id} {view}
      </h1>
      <input aria-label="Mailbox draft" defaultValue="Saved" />
    </>
  ),
}));
vi.mock("./pages/app-settings", () => ({
  AppSettings: () => <h1>App settings page</h1>,
}));
vi.mock("./pages/notifications", () => ({
  Notifications: () => <h1>Notifications page</h1>,
}));
vi.mock("./pages/deliveries", () => ({
  Deliveries: () => <h1>Deliveries page</h1>,
}));
vi.mock("./pages/logs", () => ({ Logs: () => <h1>Logs page</h1> }));
vi.mock("./pages/settings", () => ({ Settings: () => <h1>Settings page</h1> }));
vi.mock("./pages/diagnostics", () => ({
  Diagnostics: () => <h1>Diagnostics page</h1>,
}));

function route(page: string, id?: string, view?: string) {
  return (
    <I18nContext.Provider
      value={{ language: "en", catalog, setLanguage: () => {} }}
    >
      <ConsolePage page={page} id={id} view={view} />
    </I18nContext.Provider>
  );
}
afterEach(cleanup);

test("lazy routes preserve all destinations and the overview fallback", async () => {
  const view = render(route("overview"));
  for (const [path, title] of [
    ["overview", "Overview"],
    ["app_settings", "App settings"],
    ["notifications", "Notifications"],
    ["deliveries", "Deliveries"],
    ["logs", "Logs"],
    ["settings", "Settings"],
    ["diagnostics", "Diagnostics"],
    ["unknown", "Overview"],
  ]) {
    view.rerender(route(path));
    expect(
      await screen.findByRole("heading", { name: `${title} page` }),
    ).toBeTruthy();
  }
});

test("mailbox routes pass identity and view while ordinary rerenders preserve drafts", async () => {
  const view = render(route("mailboxes", "work", "settings"));
  expect(
    await screen.findByRole("heading", { name: "Mailbox work settings" }),
  ).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Mailbox draft"), {
    target: { value: "Unsaved draft" },
  });
  view.rerender(route("mailboxes", "work", "settings"));
  expect(screen.getByDisplayValue("Unsaved draft")).toBeTruthy();
  view.rerender(route("mailboxes", "personal", "settings"));
  expect(
    await screen.findByRole("heading", { name: "Mailbox personal settings" }),
  ).toBeTruthy();
  expect(screen.getByDisplayValue("Saved")).toBeTruthy();
});
