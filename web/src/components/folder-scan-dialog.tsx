import { folderLabel } from "@/lib/folders";
import type { FolderDiscovery } from "@/lib/types";
import { useEffect, useId, useRef, useState } from "react";
import { Search, X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { connectionUsage, subscriptionsPayload } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import type { Folder, Mailbox, Subscriptions } from "@/lib/types";
import { Spinner } from "./ui/spinner";
import { Button } from "./ui/button";
import { Alert, AlertDescription } from "./ui/alert";
import { BusyButton, ErrorNotice, useToast } from "./common";
import { FolderSelection } from "./folder-selection";
import { Budget } from "./shell";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";

const steps = ["scan", "select", "review"] as const;
type Step = (typeof steps)[number];

export function FolderScanDialog({
  mailbox,
  onClose,
}: {
  mailbox: Mailbox;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const client = useQueryClient();
  const toast = useToast();
  const dialog = useRef<HTMLDialogElement>(null);
  const dialogTitle = useRef<HTMLHeadingElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const request = useRef<AbortController | null>(null);
  const title = useId();
  const description = useId();
  const [step, setStep] = useState<Step>("scan");
  const [busy, setBusy] = useState<"scan" | "save" | "reload" | null>(null);
  const [error, setError] = useState<unknown>();
  const [subscriptions, setSubscriptions] = useState<Subscriptions>();
  const [roles, setRoles] = useState<Record<string, string>>({});
  const [available, setAvailable] = useState<string[]>([]);
  const [folders, setFolders] = useState<Folder[]>([]);
  const over = connectionUsage(folders) > mailbox.connection_limit - 1;
  const saving = busy === "save";
  useEffect(() => {
    dialog.current!.showModal();
    return () => request.current?.abort();
  }, []);
  useEffect(() => {
    (step === "scan" ? dialogTitle : heading).current?.focus();
  }, [step]);

  async function scan() {
    const controller = new AbortController();
    request.current = controller;
    setBusy("scan");
    setError(undefined);
    try {
      const [result, saved] = await Promise.all([
        api<FolderDiscovery>(`/mailboxes/${mailbox.id}/folders`, {
          signal: controller.signal,
        }),
        subscriptions ??
          api<Subscriptions>(`/mailboxes/${mailbox.id}/subscriptions`, {
            signal: controller.signal,
          }),
      ]);
      if (controller.signal.aborted) return;
      setAvailable(result.folders);
      setRoles(result.folder_roles ?? {});
      if (!subscriptions) {
        setSubscriptions(saved);
        setFolders(saved.folders);
      }
      setStep("select");
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
      controller.abort();
    } finally {
      setBusy(null);
    }
  }
  async function reload() {
    setBusy("reload");
    setError(undefined);
    const controller = new AbortController();
    request.current = controller;
    try {
      const saved = await api<Subscriptions>(
        `/mailboxes/${mailbox.id}/subscriptions`,
        { signal: controller.signal },
      );
      if (controller.signal.aborted) return;
      setSubscriptions(saved);
      setFolders(saved.folders);
      setStep("select");
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      setBusy(null);
    }
  }
  async function save() {
    if (!subscriptions || over) return;
    setBusy("save");
    setError(undefined);
    try {
      await api(`/mailboxes/${mailbox.id}/subscriptions`, {
        method: "PUT",
        body: subscriptionsPayload(folders, subscriptions.revision),
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: ["subscriptions", mailbox.id] }),
        client.invalidateQueries({ queryKey: ["status"] }),
      ]);
      toast(t("ui.saved"));
      dialog.current?.close();
    } catch (error) {
      setError(error);
    } finally {
      setBusy(null);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="m-auto h-[min(680px,calc(100dvh-2rem))] max-h-[calc(100dvh-2rem)] w-[min(48rem,calc(100vw-2rem))] overflow-hidden rounded-xl border bg-card p-0 text-card-foreground backdrop:bg-[var(--dialog-backdrop)]"
      aria-labelledby={title}
      aria-describedby={description}
      onClose={onClose}
      onCancel={(event) => {
        if (saving) event.preventDefault();
      }}
    >
      <div className="flex h-full min-h-0 flex-col">
        <header className="shrink-0 border-b px-4 py-3 sm:px-6 sm:py-4">
          <div className="flex items-center justify-between gap-4">
            <h2
              id={title}
              ref={dialogTitle}
              tabIndex={-1}
              className="text-lg font-semibold"
            >
              {t("ui.discover")}
            </h2>
            <Button
              variant="ghost"
              size="icon"
              disabled={saving}
              aria-label={t("ui.close")}
              onClick={() => dialog.current?.close()}
            >
              <X aria-hidden="true" />
            </Button>
          </div>
          <p id={description} className="text-muted-foreground text-xs">
            {mailbox.label} · {mailbox.host}
          </p>
          <ol
            className="mt-4 flex list-none gap-3 p-0 sm:gap-4"
            aria-label={t("ui.setup_progress")}
          >
            {steps.map((item, index) => (
              <li
                className="group flex items-center gap-1.5 text-xs text-muted-foreground aria-[current=step]:font-semibold aria-[current=step]:text-primary"
                key={item}
                aria-current={step === item ? "step" : undefined}
              >
                <span
                  className="grid size-5.5 shrink-0 place-items-center rounded-full border group-aria-[current=step]:border-primary group-aria-[current=step]:bg-sidebar-accent"
                  aria-hidden="true"
                >
                  {index + 1}
                </span>
                {t(`ui.scan_step_${item}`)}
              </li>
            ))}
          </ol>
        </header>
        <div
          className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4 [scrollbar-gutter:stable] sm:px-6 sm:py-5"
          key={step}
        >
          <div className="flex flex-col gap-4">
            <div>
              <p className="text-muted-foreground text-xs">
                {t("ui.setup_step", {
                  step: steps.indexOf(step) + 1,
                  total: 3,
                })}
              </p>
              <h3
                ref={heading}
                tabIndex={-1}
                className="mt-2 text-lg font-semibold"
              >
                {t(`ui.scan_step_${step}`)}
              </h3>
            </div>
            <ErrorNotice error={error} reload={() => void reload()} />
            {step === "scan" ? (
              <div className="flex flex-col items-center gap-5 px-4 py-8 text-center text-muted-foreground">
                {busy === "scan" ? (
                  <Spinner className="size-8 text-primary" />
                ) : (
                  <Search aria-hidden="true" className="size-8 text-primary" />
                )}
                <p role="status">
                  {busy === "scan"
                    ? t("ui.scan_pending")
                    : t("ui.scan_intro", { name: mailbox.label })}
                </p>
              </div>
            ) : (
              <>
                <Budget folders={folders} limit={mailbox.connection_limit} />
                {over && (
                  <Alert variant="destructive">
                    <AlertDescription>
                      {t("ui.budget_exceeded")}
                    </AlertDescription>
                  </Alert>
                )}
                {step === "select" ? (
                  <>
                    <p className="text-muted-foreground text-xs">
                      {t("ui.scan_found", { count: available.length })}
                    </p>
                    <FolderSelection
                      folders={folders}
                      available={available}
                      roles={roles}
                      onChange={setFolders}
                    />
                  </>
                ) : (
                  <>
                    <p>
                      {t(
                        folders.length
                          ? "ui.scan_review"
                          : "ui.scan_review_empty",
                        { count: folders.length },
                      )}
                    </p>
                    {folders.length > 0 && (
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>{t("ui.folder")}</TableHead>
                            <TableHead>{t("ui.check")}</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {folders.map((folder) => (
                            <TableRow key={folder.name}>
                              <TableCell title={folder.name}>
                                {folderLabel(folder.name, roles, t)}
                              </TableCell>
                              <TableCell>
                                {t(`ui.check_${folder.check}`)}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    )}
                  </>
                )}
              </>
            )}
          </div>
        </div>
        <footer className="flex shrink-0 justify-between gap-3 border-t px-4 py-3 sm:px-6 sm:py-4">
          <Button
            variant="outline"
            disabled={!!busy && step !== "scan"}
            onClick={() =>
              step === "scan"
                ? dialog.current?.close()
                : setStep(step === "review" ? "select" : "scan")
            }
          >
            {t(step === "scan" ? "ui.cancel" : "ui.back")}
          </Button>
          {step === "scan" && (
            <BusyButton busy={!!busy} onClick={() => void scan()}>
              {t("ui.scan_start")}
            </BusyButton>
          )}
          {step === "select" && (
            <Button disabled={!!busy || over} onClick={() => setStep("review")}>
              {t("ui.next")}
            </Button>
          )}
          {step === "review" && (
            <BusyButton
              busy={!!busy}
              disabled={over}
              onClick={() => void save()}
            >
              {t("ui.scan_confirm")}
            </BusyButton>
          )}
        </footer>
      </div>
    </dialog>
  );
}
