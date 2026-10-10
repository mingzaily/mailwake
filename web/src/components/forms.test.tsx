import { afterEach, expect, test, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nContext } from "@/lib/i18n";
import { AuthForm } from "./auth";
import { SetupWizard } from "./setup";
import { MailboxEditor } from "./mailbox-form";
import { SubscriptionsEditor } from "./subscriptions-form";
import { Settings } from "@/pages/settings";
import catalog from "../../../internal/i18n/locales/en.json";
import type { ReactNode } from "react";
import type { Mailbox } from "@/lib/types";
const box: Mailbox = {
  id: "mbx_test",
  label: "Work",
  host: "imap.test",
  port: 993,
  username: "user@test",
  password: { configured: true },
  revision: 3,
  connection_limit: 3,
  connections_in_use: 0,
};
function mount(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nContext.Provider
        value={{ language: "en", catalog, setLanguage: () => {} }}
      >
        {children}
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status });
}
test("incorrect setup code keeps administrator input and focuses the setup code", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      response(
        {
          error: { code: "setup_code_invalid", message: "Wrong setup code" },
        },
        400,
      ),
    ),
  );
  const success = vi.fn();
  mount(<AuthForm setup onSuccess={success} />);
  fireEvent.change(screen.getByLabelText("Setup code"), {
    target: { value: "WRONG" },
  });
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "retained-user" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "long-test-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create administrator" }));
  await waitFor(() =>
    expect(screen.getAllByText("Wrong setup code").length).toBeGreaterThan(0),
  );
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe(
    "retained-user",
  );
  expect(document.activeElement).toBe(screen.getByLabelText("Setup code"));
  expect(success).not.toHaveBeenCalled();
});
test("lowering a mailbox limit blocks saving an over-budget subscription set", () => {
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  mount(
    <MailboxEditor
      mailbox={box}
      subscriptions={[
        { name: "A", check: "realtime" },
        { name: "B", check: "5m" },
      ]}
      onSaved={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText("Connection limit"), {
    target: { value: "2" },
  });
  expect(
    screen
      .getByRole("button", { name: "Save changes" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(screen.getByText(catalog["ui.budget_exceeded"])).toBeTruthy();
  expect(fetchMock).not.toHaveBeenCalled();
});
test("subscription conflict preserves the chosen check method and offers reload", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((_url, options) =>
      Promise.resolve(
        options.method === "PUT"
          ? response(
              {
                error: {
                  code: "subscriptions_conflict",
                  message: "Changed elsewhere",
                },
              },
              409,
            )
          : response({
              revision: 4,
              folders: [{ name: "Bank", check: "realtime" }],
            }),
      ),
    ),
  );
  mount(<SubscriptionsEditor mailbox={box} />);
  const select = await screen.findByLabelText("Bank Check method");
  fireEvent.change(select, { target: { value: "15m" } });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await screen.findByText("Changed elsewhere");
  expect((select as HTMLSelectElement).value).toBe("15m");
  expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
});
test("a wrong current password stays in the settings form", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url) =>
      Promise.resolve(
        String(url).endsWith("/tokens")
          ? response({ tokens: [] })
          : response(
              {
                error: {
                  code: "current_password_invalid",
                  message: "Wrong current password",
                },
              },
              400,
            ),
      ),
    ),
  );
  mount(<Settings />);
  fireEvent.change(screen.getByLabelText("Current password"), {
    target: { value: "wrong" },
  });
  fireEvent.change(screen.getByLabelText("New password"), {
    target: { value: "new-long-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Change password" }));
  await waitFor(() =>
    expect(
      screen.getAllByText("Wrong current password").length,
    ).toBeGreaterThan(0),
  );
  expect(
    (screen.getByLabelText("New password") as HTMLInputElement).value,
  ).toBe("new-long-password");
  expect(screen.getByRole("heading", { name: "API tokens" })).toBeTruthy();
});

test("renaming sends the mailbox contract and retains the configured credential", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(response({ ...box, label: "Renamed", revision: 4 }));
  vi.stubGlobal("fetch", fetchMock);
  const saved = vi.fn();
  mount(<MailboxEditor mailbox={box} onSaved={saved} />);
  fireEvent.change(screen.getByLabelText("Display name"), {
    target: { value: "Renamed" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
    revision: 3,
    label: "Renamed",
    host: "imap.test",
    port: 993,
    username: "user@test",
    connection_limit: 3,
  });
  expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/mailboxes/mbx_test");
});

test("a proxy HTML error preserves login input and explains how to reconnect", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response("<html>Bad gateway</html>", { status: 502 }),
      ),
  );
  mount(<AuthForm setup={false} onSuccess={() => {}} />);
  fireEvent.change(screen.getByLabelText("Username"), {
    target: { value: "retained-user" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "retained-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByText(
    "Unable to connect to Mailwake. Check your network or reverse proxy.",
  );
  expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe(
    "retained-user",
  );
  expect((screen.getByLabelText("Password") as HTMLInputElement).value).toBe(
    "retained-password",
  );
});

test.each([
  ["QQ Mail", "imap.qq.com"],
  ["163 Mail", "imap.163.com"],
  ["126 Mail", "imap.126.com"],
  ["Yeah Mail", "imap.yeah.net"],
  ["iCloud", "imap.mail.me.com"],
  ["Tencent Exmail", "imap.exmail.qq.com"],
  ["Alibaba Personal Mail", "imap.aliyun.com"],
  ["Alibaba Business Mail", "imap.qiye.aliyun.com"],
  ["Yahoo", "imap.mail.yahoo.com"],
  ["Gmail", "imap.gmail.com"],
])(
  "%s shortcut preserves mailbox details and saves the selected IMAP endpoint",
  async (provider, host) => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(response({ ...box, host, port: 993 }));
    vi.stubGlobal("fetch", fetchMock);
    const saved = vi.fn();
    mount(<MailboxEditor mailbox={{ ...box, port: 1993 }} onSaved={saved} />);
    const shortcut = screen.queryByRole("button", { name: provider });
    if (shortcut) {
      fireEvent.click(shortcut);
      expect(shortcut.getAttribute("aria-pressed")).toBe("true");
    } else {
      fireEvent.keyDown(
        screen.getByRole("button", { name: "More mail providers" }),
        { key: "ArrowDown" },
      );
      fireEvent.click(await screen.findByRole("menuitem", { name: provider }));
    }
    expect(fetchMock).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(saved).toHaveBeenCalled());
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({
      revision: box.revision,
      label: box.label,
      host,
      port: 993,
      username: box.username,
      connection_limit: box.connection_limit,
    });
  },
);

test("IMAP shortcut keeps entered credentials and allows a custom endpoint", () => {
  mount(<MailboxEditor onSaved={() => {}} />);
  const input = (key: keyof typeof catalog) =>
    screen.getByLabelText(catalog[key]) as HTMLInputElement;
  fireEvent.change(input("ui.mailbox_username"), {
    target: { value: "user@example.com" },
  });
  fireEvent.change(input("ui.mailbox_password"), {
    target: { value: "synthetic-app-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Gmail" }));
  fireEvent.change(input("ui.imap_host"), {
    target: { value: "mail.example.com" },
  });
  fireEvent.change(input("ui.imap_port"), { target: { value: "1993" } });
  expect(input("ui.imap_host").value).toBe("mail.example.com");
  expect(input("ui.imap_port").value).toBe("1993");
  expect(input("ui.mailbox_username").value).toBe("user@example.com");
  expect(input("ui.mailbox_password").value).toBe("synthetic-app-password");
});

test("connection results expand folder names and disappear when the endpoint changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(response({ folders: ["INBOX", "Archive"] })),
  );
  mount(<MailboxEditor mailbox={box} onSaved={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
  const summary = await screen.findByText("Found 2 folders");
  const details = summary.closest("details")!;
  expect(details.open).toBe(true);
  fireEvent.click(summary);
  expect(details.open).toBe(false);
  expect(screen.getByText("Archive")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Gmail" }));
  expect(screen.queryByText("Found 2 folders")).toBeNull();
});

test("setup finishes after mailbox and folder selection without a notification step", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/mailboxes/test"))
        return response({ folders: ["INBOX"] });
      if (url.endsWith("/subscriptions"))
        return response({ revision: 1, folders: [] });
      return response(box);
    }),
  );
  const done = vi.fn();
  mount(<SetupWizard done={done} />);
  expect(screen.getByText("Step 2 / 3")).toBeTruthy();
  expect(screen.queryByText(catalog["ui.setup_notifications"])).toBeNull();
  for (const [label, value] of [
    ["Display name", "Personal"],
    [catalog["ui.mailbox_username"], "user@example.com"],
    [catalog["ui.mailbox_password"], "synthetic-password"],
  ]) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } });
  }
  fireEvent.click(screen.getByRole("button", { name: "QQ Mail" }));
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.continue"] }));
  fireEvent.click(
    await screen.findByRole("button", { name: catalog["ui.save"] }),
  );
  expect(await screen.findByText("Step 3 / 3")).toBeTruthy();
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "Inbox (INBOX)" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save and finish" }));
  await waitFor(() => expect(done).toHaveBeenCalledOnce());
});
