import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { connectionUsage, subscriptionsPayload } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import type {
  Folder,
  Mailbox,
  Subscriptions,
  FolderDiscovery,
} from "@/lib/types";
import { Budget } from "./shell";
import { Button } from "./ui/button";
import { FolderSelection } from "./folder-selection";
import { Alert, AlertDescription } from "./ui/alert";
import { BusyButton, ErrorNotice, useToast } from "./common";

export function SubscriptionsEditor({
  mailbox,
  discovered = [],
  roles = {},
  onSaved,
}: {
  mailbox: Mailbox;
  discovered?: string[];
  roles?: Record<string, string>;
  onSaved?: () => void;
}) {
  const { t } = useI18n();
  const [available, setAvailable] = useState<string[]>(discovered);
  const query = useQuery({
    queryKey: ["subscriptions", mailbox.id],
    queryFn: () => api<Subscriptions>(`/mailboxes/${mailbox.id}/subscriptions`),
    refetchOnWindowFocus: false,
  });
  if (!query.data)
    return (
      <>
        <ErrorNotice error={query.error} />
        <p>{t("ui.loading")}</p>
      </>
    );
  return (
    <SubscriptionFields
      key={`${mailbox.id}-${query.data.revision}`}
      mailbox={mailbox}
      initial={query.data}
      discovered={[...new Set([...discovered, ...available])]}
      roles={roles}
      onDiscovered={setAvailable}
      onSaved={onSaved}
      reload={() => void query.refetch()}
    />
  );
}
function SubscriptionFields({
  mailbox,
  initial,
  discovered,
  roles: initialRoles,
  onDiscovered,
  onSaved,
  reload,
}: {
  mailbox: Mailbox;
  initial: Subscriptions;
  discovered: string[];
  roles: Record<string, string>;
  onDiscovered: (folders: string[]) => void;
  onSaved?: () => void;
  reload: () => void;
}) {
  const { t } = useI18n();
  const toast = useToast();
  const client = useQueryClient();
  const [roles, setRoles] = useState(initialRoles);
  const [folders, setFolders] = useState<Folder[]>(initial.folders);
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const over = connectionUsage(folders) > mailbox.connection_limit - 1;
  async function discover() {
    setError(undefined);
    setBusy(true);
    try {
      const result = await api<FolderDiscovery>(
        `/mailboxes/${mailbox.id}/folders`,
      );
      onDiscovered(result.folders);
      setRoles(result.folder_roles ?? {});
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (over) return;
    setError(undefined);
    setBusy(true);
    try {
      await api(`/mailboxes/${mailbox.id}/subscriptions`, {
        method: "PUT",
        body: subscriptionsPayload(folders, initial.revision),
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: ["subscriptions", mailbox.id] }),
        client.invalidateQueries({ queryKey: ["status"] }),
      ]);
      toast(t("ui.saved"));
      onSaved?.();
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <ErrorNotice error={error} reload={reload} />
      <Budget folders={folders} limit={mailbox.connection_limit} />
      {over && (
        <Alert variant="destructive">
          <AlertDescription>{t("ui.budget_exceeded")}</AlertDescription>
        </Alert>
      )}
      <FolderSelection
        folders={folders}
        available={discovered}
        roles={roles}
        onChange={setFolders}
      />
      <div className="flex flex-wrap items-center gap-3">
        <BusyButton
          variant="outline"
          busy={busy}
          onClick={() => void discover()}
        >
          {t("ui.discover")}
        </BusyButton>
        <Button disabled={busy || over} onClick={() => void save()}>
          {t(onSaved ? "ui.save_finish" : "ui.save")}
        </Button>
      </div>
    </div>
  );
}
