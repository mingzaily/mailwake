import english from "../../../internal/i18n/locales/en.json";
import { Badge } from "@/components/ui/badge";
import { useEffect, useRef, useState } from "react";
import { api, downloadJSON } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { useMailboxes } from "@/lib/queries";
import type { LogEntry, LogPage } from "@/lib/types";
import {
  EmptyState,
  ErrorNotice,
  PageHeading,
  Panel,
  SelectField,
  Time,
} from "@/components/common";
import { Button } from "@/components/ui/button";
const logKeys = new Map(
  Object.entries(english)
    .filter(([key]) => key.startsWith("log."))
    .map(([key, value]) => [value, key]),
);
const attributeLabels: Record<string, string> = {
  mailbox_id: "ui.mailbox",
  folder: "ui.folder",
  check: "ui.check",
  count: "ui.log_count",
  backoff_seconds: "ui.log_retry",
};
export function Logs() {
  const { t, language } = useI18n();
  const boxes = useMailboxes();
  const [level, setLevel] = useState("info");
  const [mailbox, setMailbox] = useState("");
  return (
    <>
      <PageHeading
        title={t("ui.logs")}
        description={t("ui.logs_description")}
      />
      <div className="flex flex-wrap items-end gap-3">
        <SelectField
          fieldClassName="w-44 max-md:min-w-32 max-md:flex-1"
          label={t("ui.level")}
          value={level}
          onChange={(event) => setLevel(event.target.value)}
        >
          {["info", "warn", "error"].map((level) => (
            <option key={level} value={level}>
              {t(`ui.level_${level}`)}
            </option>
          ))}
        </SelectField>
        <SelectField
          fieldClassName="w-44 max-md:min-w-32 max-md:flex-1"
          label={t("ui.mailbox")}
          value={mailbox}
          onChange={(event) => setMailbox(event.target.value)}
        >
          <option value="">{t("ui.all_mailboxes")}</option>
          {boxes.data?.mailboxes.map((box) => (
            <option key={box.id} value={box.id}>
              {box.label}
            </option>
          ))}
        </SelectField>
      </div>
      <LogFeed
        key={`${language}/${level}/${mailbox}`}
        level={level}
        mailbox={mailbox}
        mailboxNames={
          new Map(boxes.data?.mailboxes.map((box) => [box.id, box.label]))
        }
      />
    </>
  );
}
function LogFeed({
  level,
  mailbox,
  mailboxNames,
}: {
  level: string;
  mailbox: string;
  mailboxNames: Map<string, string>;
}) {
  const { t, language } = useI18n();
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState<unknown>();
  const after = useRef(0);
  const viewport = useRef<HTMLDivElement>(null);
  const follow = useRef(true);
  const [following, setFollowing] = useState(true);
  useEffect(() => {
    if (paused) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    let active = false;
    async function refresh() {
      if (active || document.hidden || controller.signal.aborted) return;
      active = true;
      try {
        const query = new URLSearchParams({
          after: String(after.current),
          level,
          mailbox_id: mailbox,
          limit: "500",
        });
        let page = await api<LogPage>(`/logs?${query}`, {
          signal: controller.signal,
        });
        const restarted = page.next < after.current;
        if (restarted) {
          query.set("after", "0");
          page = await api<LogPage>(`/logs?${query}`, {
            signal: controller.signal,
          });
        }
        if (controller.signal.aborted) return;
        after.current = page.next;
        setEntries((previous) =>
          [...(restarted ? [] : previous), ...page.entries].slice(-2000),
        );
        setError(undefined);
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        active = false;
        if (!controller.signal.aborted)
          timer = setTimeout(() => void refresh(), 3000);
      }
    }
    const visibility = () => {
      clearTimeout(timer);
      if (!document.hidden) void refresh();
    };
    document.addEventListener("visibilitychange", visibility);
    void refresh();
    return () => {
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [level, mailbox, paused]);
  useEffect(() => {
    if (follow.current && viewport.current)
      viewport.current.scrollTop = viewport.current.scrollHeight;
  }, [entries]);
  return (
    <>
      <ErrorNotice error={error} />
      <Panel
        title={t("ui.logs")}
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPaused(!paused)}
            >
              {t(paused ? "ui.resume" : "ui.pause")}
            </Button>
            <Button
              variant="outline"
              size="sm"
              title={t("ui.log_export_notice")}
              onClick={() => downloadJSON(entries, "mailwake-logs.json")}
            >
              {t("ui.export")}
            </Button>
          </>
        }
      >
        <div
          ref={viewport}
          className="h-[55dvh] min-h-72 overflow-auto overscroll-contain px-4 py-3 font-mono text-[13px] tabular-nums"
          tabIndex={0}
          role="log"
          aria-label={t("ui.logs")}
          aria-live="off"
          onScroll={(event) => {
            const el = event.currentTarget;
            follow.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 40;
            setFollowing(follow.current);
          }}
        >
          {entries.length ? (
            entries.map((entry) => (
              <div
                key={entry.seq}
                className="border-b border-border-subtle py-1.5 wrap-anywhere [content-visibility:auto]"
                data-level={entry.level}
              >
                <Time value={entry.time} />
                <Badge
                  className="mx-3 min-w-12"
                  variant={
                    entry.level === "error"
                      ? "error"
                      : entry.level === "warn"
                        ? "warning"
                        : "neutral"
                  }
                >
                  {t(`ui.level_${entry.level}`)}
                </Badge>
                <span>
                  {logKeys.has(entry.message)
                    ? t(logKeys.get(entry.message)!)
                    : entry.message}
                </span>
                <div className="flex flex-wrap gap-x-3 text-muted-foreground">
                  {Object.entries(entry.attrs)
                    .filter(
                      ([key, value]) => value !== "" && key in attributeLabels,
                    )
                    .map(([key, value]) => (
                      <span key={key}>
                        {t(attributeLabels[key])}:
                        {key === "mailbox_id" ? (
                          <span title={String(value)}>
                            {mailboxNames.get(String(value)) ?? String(value)}
                          </span>
                        ) : key === "backoff_seconds" &&
                          typeof value === "number" ? (
                          <span>
                            {t("ui.duration_seconds", {
                              seconds: new Intl.NumberFormat(language, {
                                minimumFractionDigits: 1,
                                maximumFractionDigits: 1,
                              }).format(value),
                            })}
                          </span>
                        ) : key === "check" ? (
                          t(`ui.check_${value}`)
                        ) : typeof value === "string" ? (
                          value
                        ) : (
                          JSON.stringify(value)
                        )}
                      </span>
                    ))}
                </div>
                <details className="mt-1 text-xs text-muted-foreground">
                  <summary className="cursor-pointer">
                    {t("ui.technical_details")}
                  </summary>
                  <pre className="mt-2 whitespace-pre-wrap break-all">
                    {JSON.stringify(
                      { message: entry.message, ...entry.attrs },
                      null,
                      2,
                    )}
                  </pre>
                </details>
              </div>
            ))
          ) : (
            <EmptyState />
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t p-4">
          <p className="text-muted-foreground text-xs">
            {t("ui.log_retention")}
          </p>
          {following ? (
            <span className="text-muted-foreground text-xs">
              {t("ui.following")}
            </span>
          ) : (
            <Button
              variant="ghost"
              onClick={() => {
                follow.current = true;
                setFollowing(true);
                if (viewport.current)
                  viewport.current.scrollTop = viewport.current.scrollHeight;
              }}
            >
              {t("ui.follow")}
            </Button>
          )}
        </div>
      </Panel>
    </>
  );
}
