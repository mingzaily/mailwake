import { folderLabel } from "@/lib/folders";
import { Input } from "./ui/input";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import { useId, useState } from "react";
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
import type { Folder, Mailbox, FolderDiscovery } from "@/lib/types";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "./ui/field";
import { BusyButton, ErrorNotice, TextField, useToast } from "./common";

const imapPresets = [
  { label: "ui.provider_qq", host: "imap.qq.com" },
  { label: "ui.provider_163", host: "imap.163.com" },
  { label: "ui.provider_yahoo", host: "imap.mail.yahoo.com" },
  { label: "ui.provider_gmail", host: "imap.gmail.com" },
];

export function MailboxEditor({
  mailbox,
  onSaved,
  reload,
  subscriptions = [],
}: {
  mailbox?: Mailbox;
  subscriptions?: Folder[];
  onSaved: (
    mailbox: Mailbox,
    folders: string[],
    roles?: Record<string, string>,
  ) => void;
  reload?: () => void;
}) {
  const { t } = useI18n();
  const limitId = useId();
  const toast = useToast();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [tested, setTested] = useState<{
    signature: string;
    folders: string[];
    roles?: Record<string, string>;
  }>();
  const {
    register,
    handleSubmit,
    watch,
    setValue,
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
      const result = await api<FolderDiscovery>(
        mailbox ? `/mailboxes/${mailbox.id}/test` : "/mailboxes/test",
        { method: "POST", body: mailboxPayload(values, mailbox?.revision) },
      );
      setTested({
        signature: JSON.stringify(mailboxPayload(values)),
        folders: result.folders,
        roles: result.folder_roles,
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
      onSaved(result, tested?.folders ?? [], tested?.roles);
    } catch (error) {
      failed(error);
    }
  }
  const message = (name: keyof MailboxForm) =>
    errors[name]?.message ? t(errors[name].message!) : undefined;
  return (
    <form
      className="w-full flex flex-col gap-5"
      noValidate
      onSubmit={handleSubmit(save)}
    >
      <ErrorNotice error={error} reload={reload} />
      <FieldGroup className="gap-5">
        <TextField
          label={t("ui.label")}
          placeholder={t("ui.label_example")}
          autoComplete="off"
          error={message("label")}
          {...register("label")}
        />
        <div className="grid grid-cols-1 gap-5 @md/field-group:grid-cols-[minmax(0,1fr)_120px]">
          <div className="flex flex-col gap-2">
            <TextField
              label={t("ui.imap_host")}
              className="font-mono text-[13px] tabular-nums"
              autoComplete="off"
              spellCheck={false}
              error={message("host")}
              {...register("host")}
            />
            <div
              role="group"
              aria-label={t("ui.imap_quick_fill")}
              className="flex flex-wrap gap-2"
            >
              {imapPresets.map((preset) => (
                <Button
                  key={preset.host}
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy || isSubmitting}
                  onClick={() => {
                    setValue("host", preset.host, {
                      shouldDirty: true,
                      shouldValidate: true,
                    });
                    setValue("port", 993, {
                      shouldDirty: true,
                      shouldValidate: true,
                    });
                  }}
                >
                  {t(preset.label)}
                </Button>
              ))}
            </div>
          </div>
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
        <div className="grid grid-cols-1 gap-5 @md/field-group:grid-cols-2">
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
        </div>
        <Field data-invalid={overBudget || !!message("connection_limit")}>
          <FieldLabel htmlFor={limitId}>{t("ui.connection_limit")}</FieldLabel>
          <div className="flex flex-col gap-3 @md/field-group:flex-row @md/field-group:items-center">
            <Input
              id={limitId}
              type="number"
              className="w-30 shrink-0"
              min={2}
              max={100}
              aria-invalid={overBudget || !!message("connection_limit")}
              aria-describedby={`${limitId}-hint${overBudget || message("connection_limit") ? ` ${limitId}-error` : ""}`}
              {...register("connection_limit", { valueAsNumber: true })}
            />
            <FieldDescription id={`${limitId}-hint`}>
              {t("ui.reserved_connection")}
            </FieldDescription>
          </div>
          {(overBudget || message("connection_limit")) && (
            <FieldError id={`${limitId}-error`}>
              {overBudget
                ? t("ui.budget_exceeded")
                : message("connection_limit")}
            </FieldError>
          )}
        </Field>
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
      {tested?.signature === signature && (
        <details className="text-sm text-muted-foreground">
          <summary className="cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
            {t("ui.discovered_folder_count", { count: tested.folders.length })}
          </summary>
          <ul className="mt-3 grid max-h-40 grid-cols-1 gap-2 overflow-y-auto sm:grid-cols-2">
            {tested.folders.map((folder) => (
              <li key={folder} className="break-all" title={folder}>
                {folderLabel(folder, tested.roles ?? {}, t)}
              </li>
            ))}
          </ul>
        </details>
      )}
    </form>
  );
}
