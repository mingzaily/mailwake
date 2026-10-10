import { FolderName } from "./folder-name";
import { useState } from "react";
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
      <Table className="min-w-5xl table-fixed">
        <TableHeader>
          <TableRow>
            {[
              { key: "subject", width: undefined },
              { key: "delivery_source", width: "w-36" },
              { key: "state", width: "w-28" },
              { key: "attempts", width: "w-20" },
              { key: "error", width: "w-44" },
              { key: "time", width: "w-40" },
            ].map(({ key, width }) => (
              <TableHead key={key} className={width}>
                {t(`ui.${key}`)}
              </TableHead>
            ))}
            <TableHead className="w-24 text-center">
              {t("ui.actions")}
            </TableHead>
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
                <div
                  className="font-mono text-[13px] tabular-nums text-xs text-muted-foreground truncate"
                  title={item.id}
                >
                  {item.id.length > 16 ? `${item.id.slice(0, 16)}…` : item.id}
                </div>
              </TableCell>
              <TableCell>
                {item.message?.test ? (
                  "—"
                ) : item.message ? (
                  <div className="flex flex-col gap-0.5">
                    <span className="truncate" title={item.message.mailbox_id}>
                      {item.message.mailbox_label ||
                        item.message.mailbox_id ||
                        t("ui.source_unavailable")}
                    </span>
                    <span
                      className="text-xs text-muted-foreground truncate"
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
                {item.devices?.map((device) => (
                  <div
                    key={device.pairing_id}
                    className="text-xs truncate"
                    title={`${device.device_name || t("ui.phone_unnamed")}: ${t(`state.${device.status}`)}${device.error_code ? ` · ${t(device.error_code)}` : ""}`}
                  >
                    <span>{device.device_name || t("ui.phone_unnamed")}: </span>
                    <State value={device.status} />
                    {device.error_code && (
                      <span> · {t(device.error_code)}</span>
                    )}
                  </div>
                ))}
              </TableCell>
              <TableCell className="font-mono text-[13px] tabular-nums">
                {item.attempts}
              </TableCell>
              <TableCell>
                <div className="whitespace-normal break-words">
                  {item.last_error?.message ?? "—"}
                </div>
              </TableCell>
              <TableCell>
                <Time value={item.accepted_at ?? item.created_at} />
              </TableCell>
              <TableCell className="text-center">
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
    </>
  );
}
