import { afterEach, expect, test, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { MailboxEditor } from "./mailbox-form";
import { I18nContext } from "@/lib/i18n";
import type { Mailbox, Status, DeliverySettings } from "@/lib/types";
import catalog from "../../../internal/i18n/locales/zh-CN.json";
import { Mailboxes } from "@/pages/mailboxes";
import { Overview } from "@/pages/overview";
import { Deliveries } from "@/pages/deliveries";
import { Settings } from "@/pages/settings";
import { DeliveryEditor } from "./delivery-form";
import { Shell } from "./shell";
import { ToastProvider } from "./common";
const box: Mailbox = {
  id: "review",
  label: "原邮箱名",
  revision: 1,
  host: "imap.example.test",
  port: 993,
  username: "test",
  password: { configured: true },
  connection_limit: 10,
  connections_in_use: 0,
};
const status: Status = {
  folders: [
    {
      mailbox_id: box.id,
      mailbox_label: box.label,
      folder: "INBOX",
      check: "5m",
      state: "watching",
    },
  ],
  channel: "bark",
  notices: {},
  delivery: { pending: 0, accepted: 0, dead: 0 },
};
const delivery: DeliverySettings = {
  revision: 1,
  channel: "bark",
  native_available: false,
  preview: "off",
  retry_count: 0,
  language: "zh-CN",
  bark: { endpoint: "https://api.day.app", key: { configured: true } },
  pushover: { token: { configured: false }, user: { configured: false } },
  webhook: { url: { configured: false }, secret: { configured: false } },
};
const clients: QueryClient[] = [];
function client() {
  const c = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  clients.push(c);
  return c;
}
function seeded() {
  const c = client();
  c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [box] });
  c.setQueryData(["status", "zh-CN"], status);
  return c;
}
function mount(node: ReactNode, c = client()) {
  render(
    <QueryClientProvider client={c}>
      <I18nContext.Provider
        value={{ language: "zh-CN", catalog, setLanguage: () => {} }}
      >
        <ToastProvider>{node}</ToastProvider>
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
  return c;
}
function mailboxPage(settings = false) {
  return (
    <Shell page="mailboxes" mailboxId={box.id} logout={() => {}}>
      <Mailboxes id={box.id} view={settings ? "settings" : undefined} />
    </Shell>
  );
}
function json(value: unknown) {
  return new Response(JSON.stringify(value));
}
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((c) => c.clear());
  vi.unstubAllGlobals();
});

test.each(["connecting", "reconnecting", "auth_required", "paused", "stopped"])(
  "overview reports %s instead of healthy",
  (state) => {
    const c = seeded();
    c.setQueryData(["status", "zh-CN"], {
      ...status,
      folders: [{ ...status.folders[0], state }],
    });
    c.setQueryData(["deliveries", "zh-CN"], { deliveries: [] });
    mount(<Overview />, c);
    expect(screen.queryByText(catalog["ui.healthy"])).toBeNull();
    expect(
      screen
        .getByRole("link", { name: catalog["ui.resolve"] })
        .getAttribute("href"),
    ).toBe(
      `#/mailboxes/${box.id}${state === "auth_required" ? "/settings" : ""}`,
    );
  },
);
test("overview names a failed connection and its folder", () => {
  const c = seeded();
  c.setQueryData(["status", "zh-CN"], {
    ...status,
    folders: [
      {
        ...status.folders[0],
        state: "reconnecting",
        last_error: {
          code: "imap_connection_failed",
          message: "IMAP 连接失败",
        },
      },
    ],
  });
  c.setQueryData(["deliveries", "zh-CN"], { deliveries: [] });
  mount(<Overview />, c);
  expect(screen.getByText("原邮箱名 · INBOX：IMAP 连接失败")).toBeTruthy();
  expect(screen.queryByText(catalog["ui.healthy"])).toBeNull();
});
const pages = [
  {
    name: "mailboxes",
    node: (
      <Shell page="mailboxes" logout={() => {}}>
        <Mailboxes />
      </Shell>
    ),
    empty: catalog["ui.mailbox_missing"],
    data: { mailboxes: [] },
  },
  {
    name: "deliveries",
    node: <Deliveries />,
    empty: catalog["ui.no_deliveries_hint"],
    data: { deliveries: [] },
  },
  {
    name: "tokens",
    node: <Settings />,
    empty: catalog["ui.no_tokens"],
    data: { tokens: [] },
  },
];
for (const page of pages) {
  test(`${page.name} shows loading until its first response`, () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise(() => {})),
    );
    mount(page.node);
    expect(screen.queryByText(page.empty)).toBeNull();
    expect(screen.getByRole("status")).toBeTruthy();
  });
  test(`${page.name} reports a failed first read without an empty result`, async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("offline"))),
    );
    mount(page.node);
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.queryByText(page.empty)).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
  });
  test(`${page.name} shows an empty result after a successful read`, async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((path: string) =>
        Promise.resolve(
          json(
            path.endsWith("/status") ? { ...status, folders: [] } : page.data,
          ),
        ),
      ),
    );
    mount(page.node);
    expect(await screen.findByText(page.empty)).toBeTruthy();
    expect(screen.queryByRole("status")).toBeNull();
  });
}
test("overview keeps unresolved folder and delivery panels in loading state", () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise(() => {})),
  );
  mount(<Overview />);
  expect(screen.queryByText(catalog["ui.mailbox_missing"])).toBeNull();
  expect(screen.queryByText(catalog["ui.no_deliveries_hint"])).toBeNull();
});
test("mailbox status failure never implies zero demand or a need to scan", async () => {
  const c = client();
  c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [box] });
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new TypeError("offline"))),
  );
  mount(mailboxPage(), c);
  expect(screen.getByRole("status")).toBeTruthy();
  expect(screen.queryByText("0 / 9")).toBeNull();
  await screen.findByRole("alert");
  expect(screen.queryByText(catalog["ui.scan_folders_hint"])).toBeNull();
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.queryByText("0 / 9")).toBeNull();
});
test("failed mailbox read ends loading and offers the list link", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new TypeError("offline"))),
  );
  mount(mailboxPage());
  await screen.findByRole("alert");
  expect(screen.queryByRole("status")).toBeNull();
  expect(
    screen.getByRole("link", { name: catalog["ui.back_mailboxes"] }),
  ).toBeTruthy();
});
test("mailbox detail follows refreshed list name and connection limit", async () => {
  const c = seeded();
  mount(mailboxPage(), c);
  await act(async () => {
    c.setQueryData(["mailboxes", "zh-CN"], {
      mailboxes: [
        { ...box, label: "新邮箱名", connection_limit: 20, revision: 2 },
      ],
    });
  });
  expect(await screen.findByRole("heading", { name: "新邮箱名" })).toBeTruthy();
  expect(screen.getByText("1 / 19")).toBeTruthy();
  expect(screen.queryByText("1 / 9")).toBeNull();
});
test("a mailbox removed externally has an explicit missing state", async () => {
  const c = seeded();
  mount(mailboxPage(), c);
  await act(async () => {
    c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [] });
  });
  expect(await screen.findByText(catalog.mailbox_not_found)).toBeTruthy();
  expect(screen.queryByRole("heading", { name: box.label })).toBeNull();
});
test("pristine mailbox settings follow external updates", async () => {
  const c = seeded();
  mount(mailboxPage(true), c);
  await act(async () => {
    c.setQueryData(["mailboxes", "zh-CN"], {
      mailboxes: [
        { ...box, label: "新邮箱名", connection_limit: 20, revision: 2 },
      ],
    });
  });
  await waitFor(() =>
    expect(
      (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
    ).toBe("新邮箱名"),
  );
  expect(
    (screen.getByLabelText(catalog["ui.connection_limit"]) as HTMLInputElement)
      .value,
  ).toBe("20");
  expect(screen.queryByRole("alert")).toBeNull();
});
test("external mailbox edits preserve the local draft until explicit reload", async () => {
  const c = seeded();
  const changed = { ...box, label: "新邮箱名", revision: 2 };
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(json({ mailboxes: [changed] }))),
  );
  mount(mailboxPage(true), c);
  fireEvent.change(screen.getByLabelText(catalog["ui.label"]), {
    target: { value: "我的草稿" },
  });
  await act(async () => {
    c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [changed] });
  });
  expect(
    (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
  ).toBe("我的草稿");
  expect(await screen.findByText(catalog.settings_conflict)).toBeTruthy();
  expect(
    (
      screen.getByRole("button", {
        name: catalog["ui.save"],
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.reload"] }));
  await waitFor(() =>
    expect(
      (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
    ).toBe("新邮箱名"),
  );
  expect(screen.queryByRole("alert")).toBeNull();
});
test("delivery refresh failure remains visible and preserves an unsaved secret", async () => {
  const c = client();
  c.setQueryData(["delivery-settings"], delivery);
  let fails = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      fails
        ? Promise.reject(new TypeError("offline"))
        : Promise.resolve(json(delivery)),
    ),
  );
  mount(<DeliveryEditor />, c);
  fireEvent.change(screen.getByLabelText(catalog["ui.bark_key"]), {
    target: { value: "synthetic-draft" },
  });
  await act(async () => {
    await c.invalidateQueries({ queryKey: ["delivery-settings"] });
  });
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(
    (screen.getByLabelText(catalog["ui.bark_key"]) as HTMLInputElement).value,
  ).toBe("synthetic-draft");
  fails = false;
  await act(async () => {
    await c.invalidateQueries({ queryKey: ["delivery-settings"] });
  });
  await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  expect(
    (screen.getByLabelText(catalog["ui.bark_key"]) as HTMLInputElement).value,
  ).toBe("synthetic-draft");
});
test("delivery settings failure ends loading when no cache exists", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new TypeError("offline"))),
  );
  mount(<DeliveryEditor />);
  expect(screen.getByRole("status")).toBeTruthy();
  await screen.findByRole("alert");
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.queryByLabelText(catalog["ui.channel"])).toBeNull();
});

test("mailbox save keeps the returned revision when the subsequent list refresh fails", async () => {
  const c = seeded();
  const revisions: number[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((_path: string, options: RequestInit) => {
      if (options.method === "PUT") {
        const body = JSON.parse(options.body as string);
        revisions.push(body.revision);
        return Promise.resolve(
          json({ ...box, label: body.label, revision: body.revision + 1 }),
        );
      }
      return Promise.reject(new TypeError("offline"));
    }),
  );
  mount(mailboxPage(true), c);
  for (const label of ["第一次保存", "第二次保存"]) {
    fireEvent.change(screen.getByLabelText(catalog["ui.label"]), {
      target: { value: label },
    });
    fireEvent.click(screen.getByRole("button", { name: catalog["ui.save"] }));
    await screen.findByRole("heading", { name: label });
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: catalog["ui.save"],
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
  }
  expect(revisions).toEqual([1, 2]);
  expect(screen.getByRole("alert")).toBeTruthy();
});

test.each(["save", "test_connection"] as const)(
  "external updates preserve mailbox input during %s",
  async (action) => {
    const c = seeded();
    let complete!: (response: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            complete = resolve;
          }),
      ),
    );
    mount(mailboxPage(true), c);
    fireEvent.change(screen.getByLabelText(catalog["ui.label"]), {
      target: { value: "待处理草稿" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: catalog[`ui.${action}`] }),
    );
    await waitFor(() =>
      expect(
        screen
          .getByLabelText(catalog["ui.label"])
          .closest("fieldset[disabled]"),
      ).not.toBeNull(),
    );
    await act(async () => {
      c.setQueryData(["mailboxes", "zh-CN"], {
        mailboxes: [{ ...box, label: "外部更新", revision: 2 }],
      });
    });
    await screen.findByRole("heading", { name: "外部更新" });
    expect(
      (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
    ).toBe("待处理草稿");
    expect(
      screen.queryByRole("button", { name: catalog["ui.reload"] }),
    ).toBeNull();
    complete(
      action === "save"
        ? new Response(
            JSON.stringify({
              error: {
                code: "settings_conflict",
                message: catalog.settings_conflict,
              },
            }),
            { status: 409 },
          )
        : json({ folders: ["INBOX"] }),
    );
    await screen.findByRole("button", { name: catalog["ui.reload"] });
    expect(
      (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
    ).toBe("待处理草稿");
    expect(
      (
        screen.getByRole("button", {
          name: catalog["ui.save"],
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  },
);

test("mailbox settings remain editable through status loading, failure and recovery", async () => {
  const c = client();
  c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [box] });
  let fail!: (reason: Error) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((_resolve, reject) => {
          fail = reject;
        }),
    ),
  );
  mount(mailboxPage(true), c);
  expect(
    screen.getByText(catalog["ui.connection_usage_unknown"], { exact: false }),
  ).toBeTruthy();
  fireEvent.change(screen.getByLabelText(catalog["ui.label"]), {
    target: { value: "保留草稿" },
  });
  fireEvent.change(screen.getByLabelText(catalog["ui.connection_limit"]), {
    target: { value: "2" },
  });
  expect(
    (
      screen.getByRole("button", {
        name: catalog["ui.save"],
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(false);
  fail(new TypeError("offline"));
  await screen.findByRole("alert");
  expect(
    (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
  ).toBe("保留草稿");
  await act(async () => {
    c.setQueryData(["status", "zh-CN"], {
      ...status,
      folders: [
        { ...status.folders[0], check: "realtime" },
        { ...status.folders[0], folder: "Archive", check: "realtime" },
      ],
    });
  });
  await screen.findByText(catalog["ui.budget_exceeded"]);
  expect(
    screen.queryByText(catalog["ui.connection_usage_unknown"], {
      exact: false,
    }),
  ).toBeNull();
  expect(
    (screen.getByLabelText(catalog["ui.label"]) as HTMLInputElement).value,
  ).toBe("保留草稿");
  expect(
    (
      screen.getByRole("button", {
        name: catalog["ui.save"],
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

test("Core validates a mailbox save while monitoring demand is unavailable", async () => {
  const c = client();
  c.setQueryData(["mailboxes", "zh-CN"], { mailboxes: [box] });
  const updates: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((_path: string, options: RequestInit) => {
      if (options.method === "PUT") {
        updates.push(JSON.parse(options.body as string));
        return Promise.resolve(
          new Response(
            JSON.stringify({
              error: {
                code: "connection_budget_exceeded",
                message: catalog.connection_budget_exceeded,
              },
            }),
            { status: 400 },
          ),
        );
      }
      return Promise.reject(new TypeError("offline"));
    }),
  );
  mount(mailboxPage(true), c);
  await screen.findByRole("alert");
  fireEvent.change(screen.getByLabelText(catalog["ui.connection_limit"]), {
    target: { value: "2" },
  });
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.save"] }));
  await screen.findByText(catalog.connection_budget_exceeded);
  expect(updates).toEqual([
    expect.objectContaining({ revision: 1, connection_limit: 2 }),
  ]);
  expect(
    (screen.getByLabelText(catalog["ui.connection_limit"]) as HTMLInputElement)
      .value,
  ).toBe("2");
  expect(
    screen.getByText(catalog["ui.connection_usage_unknown"], { exact: false }),
  ).toBeTruthy();
});

function CreateThenView() {
  const [created, setCreated] = useState(false);
  return created ? (
    <Mailboxes id={box.id} />
  ) : (
    <MailboxEditor onSaved={() => setCreated(true)} />
  );
}
test.each(["empty", "existing", "unavailable", "concurrent"])(
  "a saved mailbox remains visible after list refresh failure with %s cache",
  async (initial) => {
    const c = client();
    const other = { ...box, id: "other", label: "另一个邮箱" };
    if (initial !== "unavailable")
      c.setQueryData(["mailboxes", "zh-CN"], {
        mailboxes: initial === "existing" ? [other] : [],
      });
    c.setQueryData(["status", "zh-CN"], status);
    let created = false;
    vi.stubGlobal(
      "fetch",
      vi.fn((path: string, options: RequestInit) => {
        if (options.method === "POST") {
          if (path.endsWith("/test"))
            return Promise.resolve(json({ folders: ["INBOX"] }));
          created = true;
          if (initial === "concurrent")
            c.setQueryData(["mailboxes", "zh-CN"], {
              mailboxes: [{ ...box, label: "轮询到的新版本", revision: 2 }],
            });
          return Promise.resolve(json(box));
        }
        return Promise.reject(new TypeError("offline"));
      }),
    );
    mount(
      <Shell page="mailboxes" logout={() => {}}>
        <CreateThenView />
      </Shell>,
      c,
    );
    for (const [label, value] of [
      ["ui.label", box.label],
      ["ui.imap_host", box.host],
      ["ui.mailbox_username", box.username],
      ["ui.mailbox_password", "synthetic-password"],
    ])
      fireEvent.change(
        screen.getByLabelText(catalog[label as keyof typeof catalog]),
        { target: { value } },
      );
    fireEvent.click(
      screen.getByRole("button", { name: catalog["ui.test_connection"] }),
    );
    await waitFor(() =>
      expect(
        (
          screen.getByRole("button", {
            name: catalog["ui.save"],
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    );
    fireEvent.click(screen.getByRole("button", { name: catalog["ui.save"] }));
    const name = initial === "concurrent" ? "轮询到的新版本" : box.label;
    await screen.findByRole("heading", { name });
    expect(created).toBe(true);
    expect(
      screen.getByRole("button", { name: catalog["ui.discover"] }),
    ).toBeTruthy();
    expect(screen.getAllByRole("link", { name })).toHaveLength(1);
    if (initial === "existing")
      expect(screen.getByRole("link", { name: other.label })).toBeTruthy();
  },
);
