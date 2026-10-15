import {
  useActionError,
  EmptyState,
  ErrorNotice,
  LoadingState,
  InlineConfirm,
  PageHeading,
  Panel,
} from "@/components/common";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, APIError } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { useMailboxes, useStatus } from "@/lib/queries";
import { navigate } from "@/lib/router";
import { connectionUsage } from "@/lib/forms";
import { MailboxEditor } from "@/components/mailbox-form";
import { FolderScanDialog } from "@/components/folder-scan-dialog";
import { Budget } from "@/components/budget";
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
  const status = useStatus();
  const client = useQueryClient();
  const [error, setError] = useActionError();
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
        <Panel>
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
      <Panel>
        {boxes.isPending && <LoadingState />}
        {boxes.data &&
          (boxes.data.mailboxes.length ? (
            <Table className="[&_th]:h-12 [&_th]:px-5 [&_td]:px-5 [&_td]:py-4">
              <TableHeader>
                <TableRow>
                  {[
                    "label",
                    "imap_host",
                    "imap_port",
                    "monitoring_connections",
                  ].map((key) => (
                    <TableHead key={key}>{t(`ui.${key}`)}</TableHead>
                  ))}
                  <TableHead className="text-center">
                    {t("ui.actions")}
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
                      {status.data
                        ? connectionUsage(
                            status.data.folders
                              .filter(
                                (folder) => folder.mailbox_id === mailbox.id,
                              )
                              .map((folder) => ({
                                name: folder.folder,
                                check: folder.check,
                              })),
                          )
                        : "—"}{" "}
                      / {mailbox.connection_limit}
                    </TableCell>
                    <TableCell className="text-center">
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
          ))}
      </Panel>
    </>
  );
}
function MailboxDetails({ id, settings }: { id: string; settings: boolean }) {
  const { t } = useI18n();
  const status = useStatus();
  const query = useMailboxes();
  const [scanning, setScanning] = useState(false);
  const mailbox = query.data?.mailboxes.find((box) => box.id === id);
  if (!mailbox)
    return (
      <>
        {query.isPending && <LoadingState />}
        {query.isSuccess && <EmptyState message={t("mailbox_not_found")} />}
        <a href="#/mailboxes">{t("ui.back_mailboxes")}</a>
      </>
    );
  const reload = async () => {
    const result = await query.refetch();
    if (result.error) throw result.error;
    const current = result.data?.mailboxes.find((box) => box.id === id);
    if (!current)
      throw new APIError(404, {
        code: "mailbox_not_found",
        message: t("mailbox_not_found"),
      });
    return current;
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
      {settings ? (
        <>
          {mailbox.label === mailbox.username &&
            mailbox.label.includes("@") && (
              <Alert>
                <AlertDescription>{t("ui.rename_hint")}</AlertDescription>
              </Alert>
            )}
          <Panel>
            <div className="p-4 md:p-5">
              <MailboxEditor
                mailbox={mailbox}
                subscriptions={
                  status.data
                    ? folders.map((folder) => ({
                        name: folder.folder,
                        check: folder.check,
                      }))
                    : undefined
                }
                reload={reload}
              />
            </div>
          </Panel>
        </>
      ) : (
        <Panel>
          {status.isPending && <LoadingState />}
          {status.data && (
            <>
              <div className="p-4 md:p-5">
                <Budget
                  folders={folders.map((folder) => ({
                    name: folder.folder,
                    check: folder.check,
                  }))}
                  limit={mailbox.connection_limit}
                />
              </div>
              {folders.length ? (
                <FolderTable folders={folders} showMailbox={false} />
              ) : (
                <EmptyState message={t("ui.scan_folders_hint")} />
              )}
            </>
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
