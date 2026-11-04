import {
  FormActions,
  useActionError,
  BusyButton,
  ErrorNotice,
  TextField,
  useToast,
} from "./common";
import { FolderName } from "./folder-name";
import { Input } from "./ui/input";
import { Button } from "./ui/button";
import { ArrowUpRight, Check, ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "./ui/dropdown-menu";
import { useEffect, useId, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { api, APIError } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import {
  mailboxPayload,
  mailboxConnection,
  presetHost,
  outlookHost,
  mailboxSchema,
  type MailboxForm,
} from "@/lib/mailbox-schema";
import { connectionUsage, errorFields } from "@/lib/forms";
import type { Folder, Mailbox, FolderDiscovery } from "@/lib/types";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldSet,
  FieldLabel,
  FieldTitle,
} from "./ui/field";

type OutlookSignIn = {
  authorization_id: string;
  user_code: string;
  verification_uri: string;
};
const imapPresets = [
  { group: "common", label: "ui.provider_outlook", host: outlookHost },
  { group: "common", label: "ui.provider_qq", host: "imap.qq.com" },
  { group: "common", label: "ui.provider_163", host: "imap.163.com" },
  { group: "common", label: "ui.provider_126", host: "imap.126.com" },
  { group: "personal", label: "ui.provider_yeah", host: "imap.yeah.net" },
  { group: "common", label: "ui.provider_icloud", host: "imap.mail.me.com" },
  {
    group: "personal",
    label: "ui.provider_yahoo",
    host: "imap.mail.yahoo.com",
  },
  { group: "common", label: "ui.provider_gmail", host: "imap.gmail.com" },
  {
    group: "business",
    label: "ui.provider_exmail",
    host: "imap.exmail.qq.com",
  },
  { group: "personal", label: "ui.provider_aliyun", host: "imap.aliyun.com" },
  {
    group: "business",
    label: "ui.provider_alibaba_business",
    host: "imap.qiye.aliyun.com",
  },
];

function connectionSignature(values: MailboxForm, authorization?: string) {
  return JSON.stringify(mailboxConnection(values, authorization));
}

function mailboxDefaults(mailbox?: Mailbox): MailboxForm {
  return {
    label: mailbox?.label ?? "",
    host: mailbox?.host ?? "",
    port: mailbox?.port ?? 993,
    username: mailbox?.username ?? "",
    connection_limit: mailbox?.connection_limit ?? 10,
    password: "",
  };
}

export function MailboxEditor({
  mailbox: latest,
  onSaved,
  reload,
  subscriptions,
  showDiscoveredFolders = true,
}: {
  mailbox?: Mailbox;
  showDiscoveredFolders?: boolean;
  subscriptions?: Folder[];
  onSaved?: (
    mailbox: Mailbox,
    folders: string[],
    roles?: Record<string, string>,
    names?: Record<string, string>,
  ) => void;
  reload?: () => Promise<Mailbox>;
}) {
  const { t, language } = useI18n();
  const limitId = useId();
  const toast = useToast();
  const client = useQueryClient();
  const [error, setError] = useActionError();
  const [action, setAction] = useState<
    "sign-in" | "test" | "save" | "reload"
  >();
  const busy = action !== undefined;
  const [mailbox, setMailbox] = useState(latest);
  const [signIn, setSignIn] = useState<
    OutlookSignIn & { username: string; mailbox?: string }
  >();
  const [tested, setTested] = useState<{
    signature: string;
    folders: string[];
    roles?: Record<string, string>;
    names?: Record<string, string>;
  }>();
  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    setError: fieldError,
    formState: { errors, isSubmitting, isDirty },
  } = useForm<MailboxForm>({
    resolver: zodResolver(mailboxSchema),
    defaultValues: mailboxDefaults(mailbox),
  });
  const conflict = !!latest && !!mailbox && latest.revision > mailbox.revision;
  useEffect(() => {
    if (conflict && !isDirty && !isSubmitting && !busy) {
      setSignIn(undefined);
      reset(mailboxDefaults(latest));
      setMailbox(latest);
      setTested(undefined);
    }
  }, [conflict, isDirty, isSubmitting, busy, latest, reset]);
  async function reloadSettings() {
    if (!reload || busy || isSubmitting) return;
    setAction("reload");
    try {
      const current = await reload();
      setSignIn(undefined);
      reset(mailboxDefaults(current));
      setMailbox(current);
      setTested(undefined);
      setError(undefined);
    } catch (error) {
      setError(error);
    } finally {
      setAction(undefined);
    }
  }
  const conflictError = conflict
    ? new APIError(409, {
        code: "settings_conflict",
        message: t("settings_conflict"),
      })
    : undefined;
  const values = watch();
  const matchedHost = presetHost(values);
  const selectedPreset = imapPresets.find(
    (preset) => preset.host === matchedHost,
  );
  function applyPreset(host: string) {
    setValue("host", host, { shouldDirty: true, shouldValidate: true });
    setValue("port", 993, { shouldDirty: true, shouldValidate: true });
  }
  const used = subscriptions
    ? connectionUsage(subscriptions)
    : mailbox
      ? undefined
      : 0;
  const overBudget = used !== undefined && used > values.connection_limit - 1;
  const outlook = matchedHost === outlookHost;
  const authorization =
    outlook &&
    signIn?.username === values.username.trim() &&
    signIn.mailbox === mailbox?.id
      ? signIn
      : undefined;
  const signature = connectionSignature(
    values,
    authorization?.authorization_id,
  );
  const needsSignIn =
    outlook &&
    !authorization &&
    !(mailbox?.outlook && mailbox.username === values.username.trim());
  async function signInToOutlook() {
    const username = values.username.trim();
    if (!username) {
      fieldError(
        "username",
        { message: "config_account_required" },
        { shouldFocus: true },
      );
      return;
    }
    setAction("sign-in");
    setError(undefined);
    setTested(undefined);
    try {
      const device = await api<OutlookSignIn>("/outlook/sign-in", {
        method: "POST",
        body: { username, mailbox_id: mailbox?.id },
      });
      setSignIn({ ...device, username, mailbox: mailbox?.id });
    } catch (error) {
      setError(error);
    } finally {
      setAction(undefined);
    }
  }
  function failed(error: unknown) {
    if (
      error instanceof APIError &&
      ["outlook_reauthorize", "outlook_sign_in_interrupted"].includes(
        error.failure.code,
      )
    ) {
      setSignIn(undefined);
      setTested(undefined);
    }
    if (error instanceof APIError && errorFields[error.failure.code])
      fieldError(
        errorFields[error.failure.code] as keyof MailboxForm,
        { message: error.message },
        { shouldFocus: true },
      );
    else setError(error);
  }
  async function test(values: MailboxForm) {
    if (conflict) return;
    if (!outlook && !mailbox && !values.password) {
      fieldError(
        "password",
        { message: t("credential_required") },
        { shouldFocus: true },
      );
      return;
    }
    setError(undefined);
    setAction("test");
    setTested(undefined);
    try {
      const result = await api<FolderDiscovery>(
        mailbox ? `/mailboxes/${mailbox.id}/test` : "/mailboxes/test",
        {
          method: "POST",
          body: mailboxPayload(
            values,
            mailbox?.revision,
            authorization?.authorization_id,
          ),
        },
      );
      setTested({
        signature: connectionSignature(values, authorization?.authorization_id),
        folders: result.folders,
        roles: result.folder_roles,
        names: result.folder_names,
      });
      toast(t("ui.connection_success"));
    } catch (error) {
      failed(error);
    } finally {
      setAction(undefined);
    }
  }
  async function save(values: MailboxForm) {
    if (conflict) return;
    setError(undefined);
    if (used !== undefined && used > values.connection_limit - 1) {
      fieldError(
        "connection_limit",
        { message: t("ui.budget_exceeded") },
        { shouldFocus: true },
      );
      return;
    }
    if (
      !mailbox &&
      tested?.signature !==
        connectionSignature(values, authorization?.authorization_id)
    ) {
      return;
    }
    setAction("save");
    try {
      const result = await api<Mailbox>(
        mailbox ? `/mailboxes/${mailbox.id}` : "/mailboxes",
        {
          method: mailbox ? "PUT" : "POST",
          body: mailboxPayload(
            values,
            mailbox?.revision,
            authorization?.authorization_id,
          ),
        },
      );
      await client.cancelQueries({ queryKey: ["mailboxes"] });
      const mergeSavedMailbox = (cached?: { mailboxes: Mailbox[] }) => {
        const boxes = cached?.mailboxes ?? [];
        return {
          mailboxes: boxes.some((box) => box.id === result.id)
            ? boxes.map((box) =>
                box.id === result.id && box.revision <= result.revision
                  ? result
                  : box,
              )
            : [...boxes, result],
        };
      };
      client.setQueriesData<{ mailboxes: Mailbox[] }>(
        { queryKey: ["mailboxes"] },
        mergeSavedMailbox,
      );
      client.setQueryData<{ mailboxes: Mailbox[] }>(
        ["mailboxes", language],
        mergeSavedMailbox,
      );
      setSignIn(undefined);
      reset(mailboxDefaults(result));
      setMailbox(result);
      await client.invalidateQueries({ queryKey: ["mailboxes"] });
      toast(t("ui.saved"));
      onSaved?.(
        result,
        tested?.folders ?? [],
        result.folder_display?.folder_roles ?? tested?.roles,
        result.folder_display?.folder_names ?? tested?.names,
      );
    } catch (error) {
      failed(error);
    } finally {
      setAction(undefined);
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
      <ErrorNotice
        error={conflictError ?? error}
        reload={reload && !busy && !isSubmitting ? reloadSettings : undefined}
      />
      <FieldSet disabled={busy || isSubmitting}>
        <FieldGroup className="gap-5">
          <TextField
            label={t("ui.label")}
            placeholder={t("ui.label_example")}
            autoComplete="off"
            error={message("label")}
            {...register("label")}
          />
          <div className="grid grid-cols-1 gap-x-3 gap-y-2 @2xs/field-group:grid-cols-[minmax(0,1fr)_5rem] @md/field-group:grid-cols-[minmax(0,1fr)_7.5rem] @md/field-group:gap-x-5">
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
            <div
              role="group"
              aria-label={t("ui.imap_quick_fill")}
              className="col-span-full flex flex-wrap gap-2"
            >
              {imapPresets
                .filter((preset) => preset.group === "common")
                .map((preset) => (
                  <Button
                    key={preset.host}
                    type="button"
                    variant="preset"
                    size="xs"
                    aria-pressed={selectedPreset === preset}
                    disabled={busy || isSubmitting}
                    onClick={() => applyPreset(preset.host)}
                  >
                    {t(preset.label)}
                  </Button>
                ))}
              <DropdownMenu modal={false}>
                <DropdownMenuTrigger asChild>
                  <Button
                    type="button"
                    variant="preset"
                    size="xs"
                    disabled={busy || isSubmitting}
                  >
                    {selectedPreset && selectedPreset.group !== "common"
                      ? t(selectedPreset.label)
                      : t("ui.more_mail_providers")}
                    <ChevronDown />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  {["personal", "business"].map((group, index) => (
                    <DropdownMenuGroup key={group}>
                      {index > 0 && <DropdownMenuSeparator />}
                      <DropdownMenuLabel>
                        {t(`ui.mail_providers_${group}`)}
                      </DropdownMenuLabel>
                      {imapPresets
                        .filter((preset) => preset.group === group)
                        .map((preset) => (
                          <DropdownMenuItem
                            key={preset.host}
                            disabled={busy || isSubmitting}
                            onSelect={() => applyPreset(preset.host)}
                          >
                            {t(preset.label)}
                          </DropdownMenuItem>
                        ))}
                    </DropdownMenuGroup>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>
          <TextField
            label={t("ui.mailbox_username")}
            autoComplete="off"
            spellCheck={false}
            error={message("username")}
            {...register("username")}
          />
          {outlook ? (
            <Field>
              <FieldTitle>{t("ui.outlook_authorization")}</FieldTitle>
              {authorization && tested?.signature === signature ? (
                <div
                  role="status"
                  className="flex min-h-9 items-center gap-2 text-sm font-medium"
                >
                  <Check aria-hidden="true" className="size-4 text-primary" />
                  {t("ui.connection_success")}
                </div>
              ) : authorization ? (
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                    <output className="inline-flex min-h-9 items-center rounded-md border border-dashed bg-muted/40 px-3 font-mono text-base font-semibold tracking-wider select-all">
                      {authorization.user_code}
                    </output>
                    <Button
                      asChild
                      variant="link"
                      className="h-auto px-0 whitespace-normal text-start"
                    >
                      <a
                        href={authorization.verification_uri}
                        target="_blank"
                        rel="noreferrer"
                      >
                        {t("ui.outlook_open")}
                        <ArrowUpRight aria-hidden="true" />
                      </a>
                    </Button>
                  </div>
                  <FieldDescription>
                    {t("ui.outlook_code_hint")}
                  </FieldDescription>
                </div>
              ) : (
                <div>
                  <BusyButton
                    type="button"
                    variant="outline"
                    busy={action === "sign-in"}
                    onClick={() => void signInToOutlook()}
                  >
                    {t("ui.outlook_sign_in")}
                  </BusyButton>
                </div>
              )}
            </Field>
          ) : (
            <TextField
              label={t("ui.mailbox_password")}
              type="password"
              autoComplete="new-password"
              placeholder={
                mailbox?.password.configured
                  ? t("ui.credential_kept")
                  : undefined
              }
              error={message("password")}
              {...register("password")}
            />
          )}
          <Field data-invalid={overBudget || !!message("connection_limit")}>
            <FieldLabel htmlFor={limitId}>
              {t("ui.connection_limit")}
            </FieldLabel>
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
                {used === undefined && <> {t("ui.connection_usage_unknown")}</>}
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
      </FieldSet>
      <FormActions>
        <BusyButton
          type="button"
          variant="outline"
          busy={action === "test"}
          disabled={busy || isSubmitting || conflict || needsSignIn}
          onClick={() => void handleSubmit(test)()}
        >
          {t("ui.test_connection")}
        </BusyButton>
        <BusyButton
          type="submit"
          busy={action === "save"}
          disabled={
            busy ||
            isSubmitting ||
            conflict ||
            overBudget ||
            needsSignIn ||
            (!mailbox && tested?.signature !== signature)
          }
        >
          {t("ui.save")}
        </BusyButton>
      </FormActions>
      {showDiscoveredFolders && tested?.signature === signature && (
        <details open className="text-sm text-muted-foreground">
          <summary className="cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
            {t("ui.discovered_folder_count", { count: tested.folders.length })}
          </summary>
          <ul className="mt-3 grid max-h-40 grid-cols-1 gap-2 overflow-y-auto sm:grid-cols-2">
            {tested.folders.map((folder) => (
              <li key={folder} className="break-all" title={folder}>
                <FolderName
                  name={folder}
                  roles={tested.roles}
                  names={tested.names}
                />
              </li>
            ))}
          </ul>
        </details>
      )}
    </form>
  );
}
