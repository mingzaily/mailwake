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
import { I18nContext } from "@/lib/i18n";
import catalog from "../../../internal/i18n/locales/en.json";
import { ClearHistoryButton } from "./clear-history-button";
import { Logs } from "@/pages/logs";
import { Deliveries } from "@/pages/deliveries";
import { setCSRF } from "@/lib/api";
import type { ReactNode } from "react";

function mount(element: ReactNode) {
  vi.spyOn(HTMLDialogElement.prototype, "showModal").mockImplementation(
    function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    },
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nContext.Provider
        value={{ language: "en", catalog, setLanguage: () => {} }}
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
  setCSRF("");
});
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });

test("clearing requires confirmation and preserves the dialog on failure", async () => {
  const cleared = vi.fn();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      response(
        {
          error: {
            code: "database_unavailable",
            message: catalog.database_unavailable,
          },
        },
        503,
      ),
    )
    .mockResolvedValueOnce(response({ deleted: 4 }));
  vi.stubGlobal("fetch", fetcher);
  setCSRF("session-csrf");
  mount(<ClearHistoryButton kind="deliveries" onCleared={cleared} />);
  const trigger = screen.getByRole("button", {
    name: catalog["ui.clear_deliveries"],
  });
  trigger.focus();
  fireEvent.click(trigger);
  expect(fetcher).not.toHaveBeenCalled();
  expect(screen.getByRole("dialog").textContent).toContain(
    catalog["ui.clear_deliveries_confirm"],
  );
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.cancel"] }));
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.activeElement).toBe(trigger);
  fireEvent.click(trigger);
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.clear"] }));
  await screen.findByText(catalog.database_unavailable);
  expect(cleared).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.clear"] }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(cleared).toHaveBeenCalledOnce();
  expect(fetcher.mock.calls[0][0]).toBe("/api/v1/deliveries");
  expect(fetcher.mock.calls[0][1].method).toBe("DELETE");
  expect(fetcher.mock.calls[0][1].headers.get("X-CSRF-Token")).toBe(
    "session-csrf",
  );
});

test("delivery cleanup refreshes the list while keeping pending work visible", async () => {
  let deleted = false;
  const record = (id: string, state: string) => ({
    id,
    state,
    attempts: 1,
    created_at: "2026-10-11T00:00:00Z",
    message: {
      mailbox_id: "",
      mailbox_label: "",
      folder: "",
      subject: id,
      test: false,
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_input, options) => {
      if (options?.method === "DELETE") {
        deleted = true;
        return response({ deleted: 1 });
      }
      return response({
        deliveries: deleted
          ? [record("Pending mail", "pending")]
          : [
              record("Finished mail", "accepted"),
              record("Pending mail", "pending"),
            ],
      });
    }),
  );
  mount(<Deliveries />);
  await screen.findByText("Finished mail");
  fireEvent.click(
    screen.getByRole("button", { name: catalog["ui.clear_deliveries"] }),
  );
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.clear"] }));
  await waitFor(() => expect(screen.queryByText("Finished mail")).toBeNull());
  expect(screen.getByText("Pending mail")).toBeTruthy();
});

test("clearing paused logs removes the feed and resumes from new logs", async () => {
  let cleared = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input, options) => {
      if (String(input).includes("/mailboxes"))
        return response({ mailboxes: [] });
      if (options?.method === "DELETE") {
        cleared = true;
        return response({ cleared_through: 1 });
      }
      return response({
        entries: [
          {
            seq: cleared ? 2 : 1,
            time: "2026-10-11T00:00:00Z",
            level: "info",
            message: cleared ? "New log" : "Old log",
            attrs: {},
          },
        ],
        next: cleared ? 2 : 1,
        cleared_through: cleared ? 1 : 0,
      });
    }),
  );
  mount(<Logs />);
  await screen.findByText("Old log");
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.pause"] }));
  fireEvent.click(
    screen.getByRole("button", { name: catalog["ui.clear_logs"] }),
  );
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.clear"] }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.queryByText("Old log")).toBeNull();
  expect(screen.queryByText("New log")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.resume"] }));
  await screen.findByText("New log");
  expect(screen.queryByText("Old log")).toBeNull();
});

test("a late log response cannot restore entries after clearing", async () => {
  let getCount = 0;
  let release!: (value: Response) => void;
  const page = (seq: number, message: string, floor: number) => ({
    entries: [
      { seq, time: "2026-10-11T00:00:00Z", level: "info", message, attrs: {} },
    ],
    next: seq,
    cleared_through: floor,
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input, options) => {
      if (String(input).includes("/mailboxes"))
        return response({ mailboxes: [] });
      if (options?.method === "DELETE") return response({ cleared_through: 1 });
      getCount++;
      if (getCount === 1) return response(page(1, "Old log", 0));
      if (getCount === 2)
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      return response(page(2, "New log", 1));
    }),
  );
  mount(<Logs />);
  await screen.findByText("Old log");
  fireEvent(document, new Event("visibilitychange"));
  fireEvent.click(
    screen.getByRole("button", { name: catalog["ui.clear_logs"] }),
  );
  fireEvent.click(screen.getByRole("button", { name: catalog["ui.clear"] }));
  await screen.findByText("New log");
  await act(async () => {
    release(response(page(1, "Old log", 0)));
  });
  expect(screen.queryByText("Old log")).toBeNull();
});

test("the log feed removes entries cleared by another session", async () => {
  let cleared = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input) => {
      if (String(input).includes("/mailboxes"))
        return response({ mailboxes: [] });
      return response({
        entries: cleared
          ? []
          : [
              {
                seq: 1,
                time: "2026-10-11T00:00:00Z",
                level: "info",
                message: "Old log",
                attrs: {},
              },
            ],
        next: 1,
        cleared_through: cleared ? 1 : 0,
      });
    }),
  );
  mount(<Logs />);
  await screen.findByText("Old log");
  cleared = true;
  fireEvent(document, new Event("visibilitychange"));
  await waitFor(() => expect(screen.queryByText("Old log")).toBeNull());
});
