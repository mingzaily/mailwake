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
import * as api from "@/lib/api";
import { Diagnostics } from "./diagnostics";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test("download refreshes request events and retains errors instead of exporting stale data", async () => {
  const report = (version: string) => ({
    schema_version: 2,
    build: {
      version,
      revision: "test",
      os: "linux",
      arch: "amd64",
      go_version: "go1.25",
    },
    app_requests: [{ request_id: version }],
  });
  const initial = report("initial");
  const fresh = report("fresh");
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(initial)))
    .mockResolvedValueOnce(new Response(JSON.stringify(fresh)))
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "database_unavailable",
            message: "Export unavailable",
          },
        }),
        { status: 503 },
      ),
    );
  vi.stubGlobal("fetch", fetcher);
  const download = vi.spyOn(api, "downloadJSON").mockImplementation(() => {});
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <I18nContext.Provider
        value={{ language: "en", catalog, setLanguage: () => {} }}
      >
        <Diagnostics />
      </I18nContext.Provider>
    </QueryClientProvider>,
  );
  await screen.findByText("initial");
  const button = screen.getByRole("button", {
    name: catalog["ui.download_report"],
  });
  await waitFor(() =>
    expect((button as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(button);
  await waitFor(() =>
    expect(download).toHaveBeenCalledWith(fresh, "mailwake-diagnostics.json"),
  );
  await waitFor(() =>
    expect((button as HTMLButtonElement).disabled).toBe(false),
  );
  fireEvent.click(button);
  await screen.findByText("Export unavailable");
  expect(download).toHaveBeenCalledTimes(1);
});
