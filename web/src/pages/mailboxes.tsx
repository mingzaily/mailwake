import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { useMailboxes, useStatus } from "@/lib/queries";
import { navigate } from "@/lib/router";
import type { Mailbox } from "@/lib/types";
import {
  EmptyState,
  ErrorNotice,
  InlineConfirm,
  PageHeading,
  Panel,
} from "@/components/common";
import { MailboxEditor } from "@/components/mailbox-form";
import { FolderScanDialog } from "@/components/folder-scan-dialog";
import { Budget } from "@/components/shell";
import { FolderTable } from "@/components/status-tables";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function Mailboxes({ id, view }: { id?: string; view?: string }) {
  const { t } = useI18n();
  const boxes = useMailboxes();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState("");
  async function remove(id: string) {
    setBusy(id);
    setError(undefined);
    try {
      await api(`/mailboxes/${id}`, { method: "DELETE" });
      await Promise.all([
        client.invalidateQueries({ queryKey: ["mailboxes"] }),
        client.invalidateQueries({ queryKey: ["status"] }),
      ]);
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  if (id === "new")
    return (
      <>
        <PageHeading
          title={t("ui.add_mailbox")}
          actions={<a href="#/mailboxes">{t("ui.back_mailboxes")}</a>}
        />
        <Panel title={t("ui.mailbox")}>
          <div className="p-4 md:p-5">
            <MailboxEditor
              onSaved={(mailbox) => navigate(`mailboxes/${mailbox.id}`)}
            />
          </div>
        </Panel>
      </>
    );
  if (id)
    return (
      <MailboxDetails
        key={`${id}/${view ?? "status"}`}
        id={id}
        settings={view === "settings"}
      />
    );
  return (
    <>
      <PageHeading
        title={t("ui.mailboxes")}
        description={t("ui.mailboxes_description")}
        actions={
          <Button asChild>
            <a href="#/mailboxes/new">{t("ui.add_mailbox")}</a>
          </Button>
        }
      />
      <ErrorNotice error={error} />
      <Panel title={t("ui.mailboxes")}>
        {boxes.data?.mailboxes.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                {["label", "imap_host", "imap_port", "connection_limit"].map(
                  (key) => (
                    <TableHead key={key}>{t(`ui.${key}`)}</TableHead>
                  ),
                )}
                <TableHead>
                  <span className="sr-only">{t("ui.delete")}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {boxes.data.mailboxes.map((mailbox) => (
                <TableRow key={mailbox.id}>
                  <TableCell>
                    <a href={`#/mailboxes/${mailbox.id}`}>{mailbox.label}</a>
                  </TableCell>
                  <TableCell className="font-mono text-[13px] tabular-nums">
                    {mailbox.host}
                  </TableCell>
                  <TableCell className="font-mono text-[13px] tabular-nums">
                    {mailbox.port}
                  </TableCell>
                  <TableCell className="font-mono text-[13px] tabular-nums">
                    {mailbox.connections_in_use} / {mailbox.connection_limit}
                  </TableCell>
                  <TableCell>
                    <InlineConfirm
                      label={t("ui.delete")}
                      question={t("ui.delete_question", {
                        name: mailbox.label,
                      })}
                      confirm={t("ui.confirm_delete")}
                      busy={busy === mailbox.id}
                      onConfirm={() => remove(mailbox.id)}
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <EmptyState message={t("ui.mailbox_missing")} />
        )}
      </Panel>
    </>
  );
}
function MailboxDetails({ id, settings }: { id: string; settings: boolean }) {
  const { t } = useI18n();
  const status = useStatus();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["mailbox", id],
    queryFn: () => api<Mailbox>(`/mailboxes/${id}`),
    refetchOnWindowFocus: false,
  });
  const [scanning, setScanning] = useState(false);
  const mailbox = query.data;
  if (!mailbox)
    return (
      <>
        <ErrorNotice error={query.error} />
        <p>{t("ui.loading")}</p>
      </>
    );
  const reload = () => {
    void query.refetch();
    void status.refetch();
  };
  const folders =
    status.data?.folders.filter((folder) => folder.mailbox_id === id) ?? [];
  return (
    <>
      <PageHeading
        title={mailbox.label}
        description={`${mailbox.host}:${mailbox.port}`}
        actions={
          <Button onClick={() => setScanning(true)}>{t("ui.discover")}</Button>
        }
      />
      <nav className="flex gap-6 border-b" aria-label={t("ui.mailbox")}>
        <a
          className="border-b-2 border-transparent pb-3 text-muted-foreground aria-[current=page]:border-primary aria-[current=page]:text-foreground"
          href={`#/mailboxes/${id}`}
          aria-current={!settings ? "page" : undefined}
        >
          {t("ui.mailbox_status")}
        </a>
        <a
          className="border-b-2 border-transparent pb-3 text-muted-foreground aria-[current=page]:border-primary aria-[current=page]:text-foreground"
          href={`#/mailboxes/${id}/settings`}
          aria-current={settings ? "page" : undefined}
        >
          {t("ui.mailbox_settings")}
        </a>
      </nav>
      <ErrorNotice error={query.error ?? status.error} reload={reload} />
      {settings ? (
        <>
          {mailbox.label === mailbox.username &&
            mailbox.label.includes("@") && (
              <Alert>
                <AlertDescription>{t("ui.rename_hint")}</AlertDescription>
              </Alert>
            )}
          <Panel title={t("ui.mailbox_settings")}>
            <div className="p-4 md:p-5">
              <MailboxEditor
                key={mailbox.revision}
                mailbox={mailbox}
                subscriptions={folders.map((folder) => ({
                  name: folder.folder,
                  check: folder.check,
                }))}
                reload={reload}
                onSaved={(saved) => client.setQueryData(["mailbox", id], saved)}
              />
            </div>
          </Panel>
        </>
      ) : (
        <Panel title={t("ui.mailbox_status")}>
          <div className="p-4 md:p-5">
            <Budget
              folders={folders.map((folder) => ({
                name: folder.folder,
                check: folder.check,
              }))}
              limit={mailbox.connection_limit}
            />
          </div>
          {status.isPending ? (
            <div className="p-4 md:p-5">{t("ui.loading")}</div>
          ) : folders.length ? (
            <FolderTable folders={folders} />
          ) : (
            <EmptyState message={t("ui.scan_folders_hint")} />
          )}
        </Panel>
      )}
      {scanning && (
        <FolderScanDialog
          mailbox={mailbox}
          onClose={() => setScanning(false)}
        />
      )}
    </>
  );
}
