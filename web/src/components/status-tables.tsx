import { FolderName } from "./folder-name";
import { useState } from "react";
import { Button } from "./ui/button";
import { DeliveryDetails } from "./delivery-details";
import { useColumnWidths } from "./use-column-widths";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Delivery, FolderStatus } from "@/lib/types";
import { Badge } from "./ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import { BusyButton, EmptyState, ErrorNotice, Time, useToast } from "./common";
function stateVariant(value: string) {
  if (["watching", "idle", "scheduled", "accepted"].includes(value))
    return "success";
  if (["auth_required", "dead", "error"].includes(value)) return "error";
  if (["reconnecting", "paused"].includes(value)) return "warning";
  return "neutral";
}
export function State({ value }: { value: string }) {
  const { t } = useI18n();
  return (
    <Badge variant={stateVariant(value)}>
      <span
        className="size-1.5 shrink-0 rounded-full bg-current"
        aria-hidden="true"
      />
      {t(`state.${value}`)}
    </Badge>
  );
}
export function FolderTable({
  folders,
  showMailbox = true,
}: {
  folders: FolderStatus[];
  showMailbox?: boolean;
}) {
  const { t } = useI18n();
  if (!folders.length) return <EmptyState message={t("ui.mailbox_missing")} />;
  return (
    <Table className="[&_th]:h-12 [&_th]:px-5 [&_td]:px-5 [&_td]:py-4">
      <TableHeader>
        <TableRow>
          {[
            ...(showMailbox ? ["mailbox"] : []),
            "folder",
            "state",
            "check",
            "last_check",
            "next_check",
            "error",
          ].map((key) => (
            <TableHead key={key}>{t(`ui.${key}`)}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {folders.map((folder) => (
          <TableRow key={`${folder.mailbox_id}/${folder.folder}`}>
            {showMailbox && (
              <TableCell>
                <a href={`#/mailboxes/${folder.mailbox_id}`}>
                  {folder.mailbox_label}
                </a>
              </TableCell>
            )}
            <TableCell>
              <FolderName name={folder.folder} />
            </TableCell>
            <TableCell>
              <State value={folder.state} />
            </TableCell>
            <TableCell>{t(`ui.check_${folder.check}`)}</TableCell>
            <TableCell>
              <Time value={folder.last_check} />
            </TableCell>
            <TableCell>
              <Time value={folder.next_check} />
            </TableCell>
            <TableCell className="min-w-44 max-w-72 whitespace-normal">
              {folder.last_error?.message ?? folder.notice?.message ?? "—"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
export function DeliveryTable({ deliveries }: { deliveries: Delivery[] }) {
  const { t } = useI18n();
  const toast = useToast();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState("");
  const [selected, setSelected] = useState<string>();
  const columns = useColumnWidths(
    [260, 180, 130, 100, 260, 180, 176],
    [120, 120, 130, 100, 120, 180, 176],
  );
  const selectedDelivery = deliveries.find((item) => item.id === selected);
  async function retry(id: string) {
    setBusy(id);
    setError(undefined);
    try {
      await api(`/deliveries/${encodeURIComponent(id)}/retry`, {
        method: "POST",
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: ["deliveries"] }),
        client.invalidateQueries({ queryKey: ["status"] }),
      ]);
      toast(t("ui.saved"));
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  if (!deliveries.length)
    return <EmptyState message={t("ui.no_deliveries_hint")} />;
  return (
    <>
      <ErrorNotice error={error} />
      <Table
        ref={columns.ref}
        style={{ width: columns.width }}
        className="min-w-full table-fixed [&_th]:h-12 [&_th]:px-5 [&_td]:px-5 [&_td]:py-4"
      >
        <colgroup>
          {columns.widths.map((width, index) => (
            <col key={index} style={{ width }} />
          ))}
        </colgroup>
        <TableHeader>
          <TableRow>
            {[
              "subject",
              "delivery_source",
              "state",
              "attempts",
              "error",
              "time",
              "actions",
            ].map((key, index) => (
              <TableHead
                key={key}
                aria-label={t(`ui.${key}`)}
                className={index === 6 ? "relative text-center" : "relative"}
              >
                <span className="block truncate">{t(`ui.${key}`)}</span>
                <button
                  type="button"
                  aria-label={t("ui.resize_column", { column: t(`ui.${key}`) })}
                  title={t("ui.resize_column_hint")}
                  className="absolute inset-y-2 right-0 w-2 cursor-col-resize touch-none rounded-sm hover:bg-primary/20 focus-visible:bg-primary/20 focus-visible:outline-2 focus-visible:outline-ring"
                  {...columns.handle(index)}
                >
                  <span
                    aria-hidden="true"
                    className="mx-auto block h-4 w-px bg-border"
                  />
                </button>
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {deliveries.map((item) => (
            <TableRow key={item.id}>
              <TableCell>
                <div
                  className="truncate"
                  title={item.message?.subject ?? undefined}
                >
                  {item.message?.test
                    ? t("ui.delivery_test")
                    : item.message?.subject == null
                      ? t("ui.subject_unavailable")
                      : item.message.subject || t("ui.subject_empty")}
                </div>
              </TableCell>
              <TableCell>
                {item.message?.test ? (
                  "—"
                ) : item.message ? (
                  <div className="truncate">
                    <span className="truncate" title={item.message.mailbox_id}>
                      {item.message.mailbox_label ||
                        item.message.mailbox_id ||
                        t("ui.source_unavailable")}
                    </span>
                    <span> / </span>
                    <span
                      className="text-muted-foreground"
                      title={item.message.folder}
                    >
                      {item.message.folder || "—"}
                    </span>
                  </div>
                ) : (
                  <span className="text-muted-foreground">
                    {t("ui.source_unavailable")}
                  </span>
                )}
              </TableCell>
              <TableCell>
                <State value={item.state} />
              </TableCell>
              <TableCell className="font-mono text-[13px] tabular-nums">
                {item.attempts}
              </TableCell>
              <TableCell>
                <div className="truncate" title={item.last_error?.message}>
                  {item.last_error?.message ?? "—"}
                </div>
              </TableCell>
              <TableCell>
                <Time value={item.accepted_at ?? item.created_at} />
              </TableCell>
              <TableCell className="text-center">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSelected(item.id)}
                >
                  {t("ui.details")}
                </Button>
                {item.state === "dead" && item.channel !== "native" && (
                  <BusyButton
                    variant="ghost"
                    size="sm"
                    busy={busy === item.id}
                    disabled={!!busy}
                    onClick={() => void retry(item.id)}
                  >
                    {t("ui.retry")}
                  </BusyButton>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {selectedDelivery && (
        <DeliveryDetails
          item={selectedDelivery}
          onClose={() => setSelected(undefined)}
        />
      )}
    </>
  );
}
