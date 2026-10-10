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
import catalog from "../../../internal/i18n/locales/en.json";
import {
  ManagementDevices,
  PairingDialog,
  type NativePairing,
} from "./native-devices";
import { DeliveryEditor } from "./delivery-form";
import type { ReactNode } from "react";
function mount(element: ReactNode, language: "en" | "zh-CN" = "en") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nContext.Provider
        value={{ language, catalog, setLanguage: () => {} }}
      >
        {element}
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
}
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
function created(): NativePairing {
  return {
    id: "pairing",
    status: "waiting",
    device_name: "",
    expires_at: new Date(Date.now() + 300000).toISOString(),
    uri: "mailwake://pair?v=1&relay=https%3A%2F%2Frelay.test&aud=test&pid=pairing&core=public&t=private&exp=9999999999",
    fingerprint: "7F3A 9C21 E4B0 55D2",
  };
}
function dialogSupport() {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
    function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  );
}
test("pairing dialog renders CSP-compatible SVG, fingerprint and one-time warning", async () => {
  dialogSupport();
  const data = created();
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ ...data, uri: undefined })),
      ),
  );
  mount(<PairingDialog created={data} onClose={() => {}} />);
  const image = await screen.findByAltText(catalog["ui.pairing_qr"]);
  expect(image.getAttribute("src")).toMatch(/^data:image\/svg\+xml/);
  expect(image.hasAttribute("style")).toBe(false);
  expect(screen.getByText(data.fingerprint!)).toBeTruthy();
  expect(screen.getByText(catalog["ui.pairing_warning"])).toBeTruthy();
  expect(screen.getByRole("status").textContent).toBe(
    catalog["ui.pairing_waiting"],
  );
});
test.each(["active", "expired", "failed"] as const)(
  "pairing dialog displays %s and clears QR",
  async (status) => {
    dialogSupport();
    const data = created();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: data.id,
            status,
            device_name: "My iPhone",
            expires_at: data.expires_at,
          }),
        ),
      ),
    );
    mount(<PairingDialog created={data} onClose={() => {}} />);
    await waitFor(() =>
      expect(screen.getByRole("status").textContent).toContain(
        catalog[`ui.pairing_${status}`],
      ),
    );
    expect(screen.queryByAltText(catalog["ui.pairing_qr"])).toBeNull();
    if (status === "active")
      expect(screen.getByRole("status").textContent).toContain("My iPhone");
  },
);
test("channel switches retain preview and hide native when unavailable", async () => {
  const data = {
    revision: 1,
    channel: "bark",
    preview: "subject",
    language: "en",
    retry_count: 0,
    native_available: true,
    bark: { endpoint: "https://api.day.app", key: { configured: true } },
    pushover: { token: { configured: false }, user: { configured: false } },
    webhook: { url: { configured: false }, secret: { configured: false } },
  };
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(data)));
  vi.stubGlobal("fetch", fetcher);
  mount(<DeliveryEditor />);
  const channel = await screen.findByLabelText(catalog["ui.channel"]);
  fireEvent.change(channel, { target: { value: "native" } });
  expect(screen.queryByRole("radio")).toBeNull();
  expect(screen.getByText(catalog["ui.native_encrypted"])).toBeTruthy();
  expect(
    screen
      .getByRole("link", { name: catalog["ui.app_pairing_link"] })
      .getAttribute("href"),
  ).toBe("#/app_settings");
  fireEvent.change(channel, { target: { value: "webhook" } });
  expect(
    (
      screen.getByRole("radio", {
        name: catalog["ui.preview_subject"],
      }) as HTMLInputElement
    ).checked,
  ).toBe(true);
  cleanup();
  fetcher.mockResolvedValue(
    new Response(JSON.stringify({ ...data, native_available: false })),
  );
  mount(<DeliveryEditor />);
  await screen.findByLabelText(catalog["ui.channel"]);
  expect(screen.queryByRole("option", { name: "Mailwake App" })).toBeNull();
});

test("a Native-only invitation omits management origin and scopes", async () => {
  dialogSupport();
  let accepted: unknown;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/settings/delivery"))
        return new Response(JSON.stringify({ native_available: true }));
      if (url.endsWith("/management/devices"))
        return new Response(JSON.stringify({ devices: [] }));
      if (url.endsWith("/device-invitations") && init?.method === "POST") {
        accepted = JSON.parse(String(init.body));
        return new Response(
          JSON.stringify({
            invitation_id: "invitation",
            pairing_id: "pairing",
            expires_at: new Date(Date.now() + 300000).toISOString(),
            uri: "mailwake://connect?v=3&core=synthetic&pair=synthetic",
            fingerprint: "ABCD",
          }),
        );
      }
      return new Response(
        JSON.stringify({
          id: "pairing",
          status: "waiting",
          device_name: "",
          expires_at: new Date(Date.now() + 300000).toISOString(),
        }),
      );
    }),
  );
  mount(<ManagementDevices />);
  const native = await screen.findByRole("checkbox", {
    name: catalog["ui.enable_native_push"],
  });
  await waitFor(() =>
    expect((native as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(
    screen.getByRole("checkbox", { name: catalog["ui.allow_app_management"] }),
  );
  expect(native.getAttribute("aria-checked")).toBe("true");
  fireEvent.click(
    screen.getByRole("button", { name: catalog["ui.add_phone"] }),
  );
  await waitFor(() =>
    expect(accepted).toEqual({
      core_origin: "",
      scopes: [],
      management: false,
      native: true,
    }),
  );
  expect(screen.queryByLabelText(catalog["ui.core_https_origin"])).toBeNull();
});

test("management revocation sends only the independent management request", async () => {
  dialogSupport();
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "DELETE") {
        requests.push(url);
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/settings/delivery"))
        return new Response(JSON.stringify({ native_available: true }));
      return new Response(
        JSON.stringify({
          devices: [
            {
              controller_id: "ctrl_phone",
              device_id: "device",
              device_name: "Owner phone",
              scopes: ["mailboxes"],
              created_at: "2026-10-01T00:00:00Z",
            },
          ],
        }),
      );
    }),
  );
  mount(<ManagementDevices />);
  fireEvent.click(
    await screen.findByRole("button", {
      name: catalog["ui.revoke_management"],
    }),
  );
  expect(
    screen.getByText((value) =>
      value.includes(catalog["ui.revoke_management_confirm"]),
    ),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.revoke"] }));
  await waitFor(() =>
    expect(requests).toEqual(["/api/v1/management/devices/ctrl_phone"]),
  );
});

test.each(["waiting", "revoked", "active"])(
  "native test requires an active push pairing: %s",
  async (status) => {
    const fetcher = vi.fn(
      async (path: string) =>
        new Response(
          JSON.stringify(
            path.endsWith("/native/devices")
              ? { devices: [{ ...created(), status }] }
              : {
                  revision: 1,
                  channel: "native",
                  preview: "off",
                  language: "en",
                  retry_count: 0,
                  native_available: true,
                  bark: { endpoint: "", key: { configured: false } },
                  pushover: {
                    token: { configured: false },
                    user: { configured: false },
                  },
                  webhook: {
                    url: { configured: false },
                    secret: { configured: false },
                  },
                },
          ),
        ),
    );
    vi.stubGlobal("fetch", fetcher);
    mount(<DeliveryEditor />);
    const button = await screen.findByRole("button", {
      name: catalog["ui.test_notification"],
    });
    if (status === "active") {
      await waitFor(() => expect(button.hasAttribute("disabled")).toBe(false));
      fireEvent.click(button);
      await waitFor(() =>
        expect(
          fetcher.mock.calls.some(([path]) => path.endsWith("/delivery/test")),
        ).toBe(true),
      );
    } else {
      await screen.findByText(catalog["ui.pair_before_test"]);
      expect(button.hasAttribute("disabled")).toBe(true);
      expect(
        screen
          .getByRole("button", { name: catalog["ui.save"] })
          .hasAttribute("disabled"),
      ).toBe(true);
      expect(
        fetcher.mock.calls.some(([path]) => path.endsWith("/delivery/test")),
      ).toBe(false);
    }
  },
);

test.each([
  ["", "zh-CN"],
  ["bark", "en"],
])(
  "notification language uses the site default and preserves a saved choice: %s",
  async (channel, expected) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              revision: 0,
              channel,
              language: "en",
              retry_count: 0,
              preview: "off",
              native_available: false,
              bark: { key: { configured: true } },
              pushover: {
                token: { configured: false },
                user: { configured: false },
              },
              webhook: {
                url: { configured: false },
                secret: { configured: false },
              },
            }),
          ),
      ),
    );
    mount(<DeliveryEditor />, "zh-CN");
    const select = await screen.findByLabelText(
      catalog["ui.notification_language"],
    );
    expect((select as HTMLSelectElement).value).toBe(expected);
  },
);

test("management-only invitation acceptance ends the countdown and hides its QR", async () => {
  dialogSupport();
  const data = { ...created(), managementOnly: true };
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({ ...data, status: "active", device_name: "Phone" }),
        ),
    ),
  );
  mount(<PairingDialog created={data} onClose={() => {}} />);
  await waitFor(() =>
    expect(screen.getByRole("status").textContent).toContain(
      catalog["ui.pairing_active"],
    ),
  );
  expect(screen.queryByAltText(catalog["ui.pairing_qr"])).toBeNull();
  expect(screen.queryByText(/seconds/)).toBeNull();
});

test("unavailable push stays unchecked and management invitations omit push", async () => {
  dialogSupport();
  let request: unknown;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/settings/delivery"))
        return new Response(JSON.stringify({ native_available: false }));
      if (url.endsWith("/management/devices"))
        return new Response(JSON.stringify({ devices: [] }));
      if (init?.method === "POST") {
        request = JSON.parse(String(init.body));
        return new Response(
          JSON.stringify({
            invitation_id: "inv",
            expires_at: created().expires_at,
          }),
        );
      }
      return new Response(JSON.stringify({ ...created(), id: "inv" }));
    }),
  );
  mount(<ManagementDevices />);
  const checkbox = await screen.findByRole("checkbox", {
    name: catalog["ui.enable_native_push"],
  });
  expect(checkbox.getAttribute("aria-checked")).toBe("false");
  expect((checkbox as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText(catalog["ui.core_https_origin"]), {
    target: { value: "https://core.example.test" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: catalog["ui.add_phone"] }),
  );
  await waitFor(() =>
    expect(request).toMatchObject({
      management: true,
      native: false,
      scopes: expect.arrayContaining(["content"]),
    }),
  );
});

test("an unavailable saved target stays selected instead of falling back to another device", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("/native/devices")
              ? {
                  devices: [
                    {
                      ...created(),
                      id: "other",
                      status: "active",
                      device_name: "Other phone",
                    },
                  ],
                }
              : {
                  revision: 1,
                  channel: "native",
                  native_pairing_id: "revoked",
                  preview: "off",
                  retry_count: 0,
                  language: "en",
                  native_available: true,
                  bark: { endpoint: "", key: { configured: false } },
                  pushover: {
                    token: { configured: false },
                    user: { configured: false },
                  },
                  webhook: {
                    url: { configured: false },
                    secret: { configured: false },
                  },
                },
          ),
        ),
    ),
  );
  mount(<DeliveryEditor />);
  const select = await screen.findByLabelText(catalog["ui.receiving_device"]);
  await screen.findByRole("option", { name: "Other phone · other" });
  expect((select as HTMLSelectElement).value).toBe("revoked");
  expect(
    screen
      .getByRole("button", { name: catalog["ui.save"] })
      .hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.change(select, { target: { value: "other" } });
  expect(
    screen
      .getByRole("button", { name: catalog["ui.save"] })
      .hasAttribute("disabled"),
  ).toBe(false);
});

test("push revocation is independent and both permissions share one device row", async () => {
  dialogSupport();
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "DELETE") {
        requests.push(url);
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/settings/delivery"))
        return new Response(JSON.stringify({ native_available: true }));
      return new Response(
        JSON.stringify({
          devices: url.endsWith("/native/devices")
            ? [
                {
                  ...created(),
                  device_id: "same",
                  device_name: "Test phone",
                  status: "active",
                },
              ]
            : [
                {
                  controller_id: "controller",
                  device_id: "same",
                  device_name: "Test phone",
                  scopes: ["mailboxes"],
                },
              ],
        }),
      );
    }),
  );
  mount(<ManagementDevices />);
  const button = await screen.findByRole("button", {
    name: catalog["ui.revoke_push"],
  });
  expect(screen.getAllByText("Test phone")).toHaveLength(1);
  expect(
    screen.getByRole("button", { name: catalog["ui.revoke_management"] }),
  ).toBeTruthy();
  fireEvent.click(button);
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.cancel"] }));
  expect(requests).toHaveLength(0);
  fireEvent.click(button);
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.revoke"] }));
  await waitFor(() =>
    expect(requests).toEqual(["/api/v1/native/devices/pairing"]),
  );
});
