import { AccountMenu } from "./account-menu";
import { SidebarProvider } from "./ui/sidebar";
import { afterEach, expect, test, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { I18nContext } from "@/lib/i18n";
import { APIError } from "@/lib/api";
import catalog from "../../../internal/i18n/locales/en.json";
import { AuthScreen } from "./auth-screen";
import { AppearanceControls } from "./appearance-controls";
import { BusyButton, TextField, InlineConfirm } from "./common";
import type { ReactNode } from "react";
function mount(child: ReactNode, setLanguage = vi.fn()) {
  return render(
    <I18nContext.Provider value={{ language: "en", catalog, setLanguage }}>
      {child}
    </I18nContext.Provider>,
  );
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
});
test("field errors and descriptions have separate semantics and input association", () => {
  mount(<TextField label="Password" hint="Hint" error="Invalid" />);
  const error = screen.getByRole("alert");
  expect(error.getAttribute("data-slot")).toBe("field-error");
  expect(screen.getByText("Hint").getAttribute("data-slot")).toBe(
    "field-description",
  );
  expect(
    screen.getByLabelText("Password").getAttribute("aria-describedby"),
  ).toBe(error.id);
});
test("loading action keeps the accessible label, disabled state and spinner", () => {
  mount(<BusyButton busy>Save</BusyButton>);
  const button = screen.getByRole("button", { name: /Save/ });
  expect(button.getAttribute("aria-busy")).toBe("true");
  expect(button.hasAttribute("disabled")).toBe(true);
  expect(
    button
      .querySelector('[data-icon="inline-start"]')
      ?.getAttribute("aria-label"),
  ).toBe(catalog["ui.loading"]);
});
test("language menu works with keyboard and keeps the existing preference callback", async () => {
  const setLanguage = vi.fn();
  mount(<AppearanceControls compact />, setLanguage);
  const trigger = screen.getByRole("button", { name: /Language:/ });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: "ArrowDown" });
  const chinese = await screen.findByRole("menuitemradio", {
    name: catalog["ui.language_zh-CN"],
  });
  fireEvent.click(chinese);
  expect(setLanguage).toHaveBeenCalledWith("zh-CN");
});
test("submit errors supersede loading errors while keeping login input", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response("<html>bad gateway</html>", { status: 502 }),
      ),
  );
  mount(
    <AuthScreen
      setup={false}
      error={
        new APIError(503, {
          code: "unavailable",
          message: "Earlier loading failure",
        })
      }
      onSuccess={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "retained" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "retained-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.login"] }));
  await waitFor(() =>
    expect(screen.queryByText("Earlier loading failure")).toBeNull(),
  );
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe(
    "retained",
  );
});

test("confirmation moves focus to cancel and returns it to its trigger", () => {
  mount(
    <InlineConfirm
      label="Delete"
      question="Delete mailbox?"
      confirm="Confirm deletion"
      busy={false}
      onConfirm={async () => {}}
    />,
  );
  const trigger = screen.getByRole("button", { name: "Delete" });
  trigger.focus();
  fireEvent.click(trigger);
  const cancel = screen.getByRole("button", { name: "Cancel" });
  expect(document.activeElement).toBe(cancel);
  expect(cancel.getAttribute("aria-describedby")).toBe(
    screen.getByText("Delete mailbox?").id,
  );
  fireEvent.click(cancel);
  expect(document.activeElement).toBe(
    screen.getByRole("button", { name: "Delete" }),
  );
});

test("appearance settings expose language and theme choices directly", () => {
  const setLanguage = vi.fn();
  mount(<AppearanceControls />, setLanguage);
  fireEvent.click(
    screen.getByRole("radio", { name: catalog["ui.language_zh-CN"] }),
  );
  expect(setLanguage).toHaveBeenCalledWith("zh-CN");
  fireEvent.click(
    screen.getByRole("radio", { name: catalog["ui.theme_dark"] }),
  );
  expect(localStorage.getItem("mailwake.theme")).toBe("dark");
  expect(document.documentElement.dataset.theme).toBe("dark");
  fireEvent.click(
    screen.getByRole("radio", { name: catalog["ui.theme_light"] }),
  );
});

test.each([false, true])(
  "account menu shows the signed-in name and supports logout inside drawer: %s",
  async (drawer) => {
    const logout = vi.fn();
    const menu = (
      <SidebarProvider>
        <AccountMenu username="review-admin" logout={logout} />
      </SidebarProvider>
    );
    mount(drawer ? <dialog open>{menu}</dialog> : menu);
    expect(screen.getByText("review-admin")).toBeTruthy();
    fireEvent.keyDown(
      screen.getByRole("button", { name: catalog["ui.account_menu"] }),
      { key: "ArrowDown" },
    );
    const action = await screen.findByRole("menuitem", {
      name: catalog["ui.logout"],
    });
    expect(!!action.closest("dialog")).toBe(drawer);
    fireEvent.click(action);
    expect(logout).toHaveBeenCalledOnce();
  },
);
