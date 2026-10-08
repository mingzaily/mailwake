import { Badge } from "./ui/badge";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { api, APIError } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import {
  connectionUsage,
  errorFields,
  mailboxPayload,
  mailboxSchema,
  type MailboxForm,
} from "@/lib/forms";
import type { Folder, Mailbox } from "@/lib/types";
import { FieldGroup } from "./ui/field";
import { BusyButton, ErrorNotice, TextField, useToast } from "./common";

export function MailboxEditor({
  mailbox,
  onSaved,
  reload,
  subscriptions = [],
}: {
  mailbox?: Mailbox;
  subscriptions?: Folder[];
  onSaved: (mailbox: Mailbox, folders: string[]) => void;
  reload?: () => void;
}) {
  const { t } = useI18n();
  const toast = useToast();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [tested, setTested] = useState<{
    signature: string;
    folders: string[];
  }>();
  const {
    register,
    handleSubmit,
    watch,
    setError: fieldError,
    formState: { errors, isSubmitting },
  } = useForm<MailboxForm>({
    resolver: zodResolver(mailboxSchema),
    defaultValues: mailbox
      ? {
          label: mailbox.label,
          host: mailbox.host,
          port: mailbox.port,
          username: mailbox.username,
          connection_limit: mailbox.connection_limit,
          password: "",
        }
      : {
          label: "",
          host: "",
          port: 993,
          username: "",
          password: "",
          connection_limit: 10,
        },
  });
  const values = watch();
  const overBudget =
    connectionUsage(subscriptions) > values.connection_limit - 1;
  const signature = JSON.stringify(mailboxPayload(values));
  function failed(error: unknown) {
    setError(error);
    if (error instanceof APIError && errorFields[error.failure.code])
      fieldError(
        errorFields[error.failure.code] as keyof MailboxForm,
        { message: error.message },
        { shouldFocus: true },
      );
  }
  async function test(values: MailboxForm) {
    if (!mailbox && !values.password) {
      fieldError(
        "password",
        { message: t("credential_required") },
        { shouldFocus: true },
      );
      return;
    }
    setError(undefined);
    setBusy(true);
    try {
      const result = await api<{ folders: string[] }>(
        mailbox ? `/mailboxes/${mailbox.id}/test` : "/mailboxes/test",
        { method: "POST", body: mailboxPayload(values, mailbox?.revision) },
      );
      setTested({
        signature: JSON.stringify(mailboxPayload(values)),
        folders: result.folders,
      });
      toast(t("ui.connection_success"));
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  }
  async function save(values: MailboxForm) {
    setError(undefined);
    if (connectionUsage(subscriptions) > values.connection_limit - 1) {
      fieldError(
        "connection_limit",
        { message: t("ui.budget_exceeded") },
        { shouldFocus: true },
      );
      return;
    }
    if (
      !mailbox &&
      tested?.signature !== JSON.stringify(mailboxPayload(values))
    ) {
      await test(values);
      return;
    }
    try {
      const result = await api<Mailbox>(
        mailbox ? `/mailboxes/${mailbox.id}` : "/mailboxes",
        {
          method: mailbox ? "PUT" : "POST",
          body: mailboxPayload(values, mailbox?.revision),
        },
      );
      await client.invalidateQueries({ queryKey: ["mailboxes"] });
      toast(t("ui.saved"));
      onSaved(result, tested?.folders ?? []);
    } catch (error) {
      failed(error);
    }
  }
  const message = (name: keyof MailboxForm) =>
    errors[name]?.message ? t(errors[name].message!) : undefined;
  return (
    <form
      className="max-w-[760px] flex flex-col gap-4"
      noValidate
      onSubmit={handleSubmit(save)}
    >
      <ErrorNotice error={error} reload={reload} />
      <FieldGroup>
        <TextField
          label={t("ui.label")}
          placeholder={t("ui.label_example")}
          autoComplete="off"
          error={message("label")}
          {...register("label")}
        />
        <div className="grid grid-cols-1 gap-5 @md/field-group:grid-cols-2">
          <TextField
            label={t("ui.imap_host")}
            className="font-mono text-[13px] tabular-nums"
            autoComplete="off"
            spellCheck={false}
            error={message("host")}
            {...register("host")}
          />
          <TextField
            label={t("ui.imap_port")}
            className="font-mono text-[13px] tabular-nums"
            type="number"
            min={1}
            max={65535}
            error={message("port")}
            {...register("port", { valueAsNumber: true })}
          />
        </div>
        <TextField
          label={t("ui.mailbox_username")}
          autoComplete="off"
          spellCheck={false}
          error={message("username")}
          {...register("username")}
        />
        <TextField
          label={t("ui.mailbox_password")}
          type="password"
          autoComplete="new-password"
          hint={
            mailbox?.password.configured ? t("ui.credential_kept") : undefined
          }
          error={message("password")}
          {...register("password")}
        />
        <TextField
          label={t("ui.connection_limit")}
          type="number"
          className="font-mono text-[13px] tabular-nums"
          min={2}
          max={100}
          hint={t("ui.reserved_connection")}
          error={
            overBudget ? t("ui.budget_exceeded") : message("connection_limit")
          }
          {...register("connection_limit", { valueAsNumber: true })}
        />
      </FieldGroup>
      <div className="flex flex-wrap items-center gap-3">
        <BusyButton
          type="button"
          variant="outline"
          busy={busy}
          disabled={isSubmitting}
          onClick={() => void handleSubmit(test)()}
        >
          {t("ui.test_connection")}
        </BusyButton>
        <BusyButton
          type="submit"
          busy={isSubmitting}
          disabled={busy || overBudget}
        >
          {t(
            !mailbox && tested?.signature !== signature
              ? "ui.continue"
              : "ui.save",
          )}
        </BusyButton>
        {tested?.signature === signature && (
          <Badge variant="success">{t("ui.connection_success")}</Badge>
        )}
      </div>
      {tested && (
        <p className="text-muted-foreground text-xs">
          {tested.folders.join(" · ")}
        </p>
      )}
    </form>
  );
}
