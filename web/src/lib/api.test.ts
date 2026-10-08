import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { api, APIError, configureAPI, setCSRF } from "./api";
const fetchMock = vi.fn();
const expired = vi.fn();
beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  configureAPI({ language: "zh-CN", onExpired: expired });
  setCSRF("memory-only");
});
afterEach(() => vi.unstubAllGlobals());
test("cookie mutations carry CSRF and negotiated language", async () => {
  fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
  await api("/admin/password", {
    method: "PUT",
    body: { current_password: "x", password: "long-password" },
  });
  const [, options] = fetchMock.mock.calls[0];
  expect(options.headers.get("X-CSRF-Token")).toBe("memory-only");
  expect(options.headers.get("Accept-Language")).toBe("zh-CN");
  expect(options.credentials).toBe("same-origin");
});
test.each([400, 409, 429])(
  "%i retains the session and exposes localized errors",
  async (status) => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          error: {
            code:
              status === 409 ? "settings_conflict" : "current_password_invalid",
            message: "本地化错误",
          },
        }),
        { status, headers: { "Retry-After": "17" } },
      ),
    );
    try {
      await api("/admin/password", { method: "PUT" });
      expect.fail("must reject");
    } catch (error) {
      expect(error).toBeInstanceOf(APIError);
      expect((error as APIError).message).toBe("本地化错误");
      expect((error as APIError).conflict).toBe(status === 409);
      expect((error as APIError).retryAfter).toBe(17);
    }
    expect(expired).not.toHaveBeenCalled();
  },
);
test("only protected 401 expires session; login failure stays in its form", async () => {
  fetchMock.mockImplementation(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({ error: { code: "unauthorized", message: "expired" } }),
        { status: 401 },
      ),
    ),
  );
  await expect(
    api("/session", { method: "POST", public: true }),
  ).rejects.toBeInstanceOf(APIError);
  expect(expired).not.toHaveBeenCalled();
  await expect(api("/status")).rejects.toBeInstanceOf(APIError);
  expect(expired).toHaveBeenCalledOnce();
  fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
  await api("/session", { method: "DELETE" });
  expect(fetchMock.mock.lastCall?.[1].headers.has("X-CSRF-Token")).toBe(false);
});
test("network failure stays a recoverable form error", async () => {
  fetchMock.mockRejectedValue(new TypeError("offline"));
  await expect(
    api("/mailboxes", { method: "POST", body: { label: "Work" } }),
  ).rejects.toBeInstanceOf(TypeError);
  expect(expired).not.toHaveBeenCalled();
});

test.each([
  [502, "<html>Bad gateway</html>"],
  [504, ""],
  [502, '{"proxy":"down"}'],
])(
  "proxy %s becomes a structured request_failed error",
  async (status, body) => {
    fetchMock.mockResolvedValue(
      new Response(body as string, { status: status as number }),
    );
    const result = api("/mailboxes", {
      method: "PUT",
      body: { label: "Work" },
    });
    await expect(result).rejects.toBeInstanceOf(APIError);
    await expect(result).rejects.toMatchObject({
      status,
      failure: { code: "request_failed" },
    });
    expect(expired).not.toHaveBeenCalled();
  },
);
