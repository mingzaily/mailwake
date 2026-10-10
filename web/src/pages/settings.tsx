import { useId, useState } from "react";
import { useForm } from "react-hook-form";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, APIError } from "@/lib/api";
import { passwordValidation } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import { AppearanceControls } from "@/components/appearance-controls";
import type { Token } from "@/lib/types";
import {
  BusyButton,
  ErrorNotice,
  InlineConfirm,
  PageHeading,
  Panel,
  TextField,
  Time,
  useToast,
} from "@/components/common";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
export function Settings() {
  const { t } = useI18n();
  return (
    <div className="mx-auto flex w-full max-w-[960px] flex-col gap-5">
      <PageHeading
        title={t("ui.settings")}
        description={t("ui.settings_description")}
      />
      <Panel title={t("ui.preferences")}>
        <div className="p-5 md:p-6">
          <AppearanceControls />
        </div>
      </Panel>
      <Panel title={t("ui.change_password")}>
        <div className="p-4 md:p-5">
          <PasswordForm />
        </div>
      </Panel>
      <Panel title={t("ui.api_tokens")}>
        <div className="p-4 md:p-5">
          <Tokens />
        </div>
      </Panel>
    </div>
  );
}
function PasswordForm() {
  const { t } = useI18n();
  const toast = useToast();
  const [error, setError] = useState<unknown>();
  const {
    register,
    handleSubmit,
    reset,
    setError: fieldError,
    formState: { errors, isSubmitting },
  } = useForm<{ current_password: string; password: string }>();
  async function submit(values: {
    current_password: string;
    password: string;
  }) {
    setError(undefined);
    try {
      await api("/admin/password", { method: "PUT", body: values });
      reset();
      toast(t("ui.password_changed"));
    } catch (error) {
      setError(error);
      if (
        error instanceof APIError &&
        error.failure.code === "current_password_invalid"
      )
        fieldError(
          "current_password",
          { message: error.message },
          { shouldFocus: true },
        );
    }
  }
  return (
    <form
      className="w-full flex flex-col gap-4"
      noValidate
      onSubmit={handleSubmit(submit)}
    >
      <ErrorNotice error={error} />
      <FieldGroup>
        <TextField
          label={t("ui.current_password")}
          type="password"
          autoComplete="current-password"
          error={errors.current_password?.message}
          {...register("current_password", { required: t("ui.required") })}
        />
        <TextField
          label={t("ui.new_password")}
          type="password"
          autoComplete="new-password"
          error={errors.password?.message}
          hint={t("ui.password_length")}
          {...register("password", {
            required: t("ui.required"),
            validate: (value) => {
              const error = passwordValidation(value);
              return error ? t(error) : true;
            },
          })}
        />
      </FieldGroup>
      <BusyButton className="self-start" type="submit" busy={isSubmitting}>
        {t("ui.change_password")}
      </BusyButton>
    </form>
  );
}
function Tokens() {
  const nameID = useId();
  const { t } = useI18n();
  const toast = useToast();
  const client = useQueryClient();
  const query = useQuery({
    queryKey: ["tokens"],
    queryFn: () => api<{ tokens: Token[] }>("/tokens"),
  });
  const [secret, setSecret] = useState("");
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState("");
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<{ name: string }>();
  async function create(values: { name: string }) {
    setError(undefined);
    try {
      const result = await api<Token & { token: string }>("/tokens", {
        method: "POST",
        body: values,
      });
      setSecret(result.token);
      reset();
      await client.invalidateQueries({ queryKey: ["tokens"] });
    } catch (error) {
      setError(error);
    }
  }
  async function revoke(id: string) {
    setBusy(id);
    setError(undefined);
    try {
      await api(`/tokens/${id}`, { method: "DELETE" });
      await client.invalidateQueries({ queryKey: ["tokens"] });
      toast(t("ui.token_revoked"));
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(secret);
      toast(t("ui.copied"));
    } catch {
      toast(t("ui.copy_manually"));
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">{t("ui.tokens_hint")}</p>
      <ErrorNotice error={error ?? query.error} />
      {secret ? (
        <div className="flex flex-col gap-4">
          <p>{t("ui.token_once")}</p>
          <code
            className="select-all break-all rounded-sm border bg-background p-3"
            tabIndex={0}
          >
            {secret}
          </code>
          <div className="flex flex-wrap items-center gap-3">
            <Button onClick={() => void copy()}>{t("ui.copy")}</Button>
            <Button variant="outline" onClick={() => setSecret("")}>
              {t("ui.token_saved")}
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="w-full max-w-xl"
          noValidate
          onSubmit={handleSubmit(create)}
        >
          <Field data-invalid={!!errors.name}>
            <FieldLabel htmlFor={nameID}>{t("ui.token_name")}</FieldLabel>
            <div className="flex flex-col gap-3 sm:flex-row">
              <Input
                id={nameID}
                placeholder={t("ui.token_name_example")}
                autoComplete="off"
                aria-invalid={!!errors.name}
                aria-describedby={errors.name ? `${nameID}-error` : undefined}
                {...register("name", { required: t("ui.required") })}
              />
              <BusyButton
                className="self-start"
                type="submit"
                busy={isSubmitting}
              >
                {t("ui.create_token")}
              </BusyButton>
            </div>
            {errors.name && (
              <FieldError id={`${nameID}-error`}>
                {errors.name.message}
              </FieldError>
            )}
          </Field>
        </form>
      )}
      {query.data?.tokens.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("ui.token_name")}</TableHead>
              <TableHead>{t("ui.created_at")}</TableHead>
              <TableHead>{t("ui.last_used_at")}</TableHead>
              <TableHead className="text-center">{t("ui.actions")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {query.data.tokens.map((token) => (
              <TableRow key={token.id}>
                <TableCell className="max-w-64 whitespace-normal break-words">
                  {token.name}
                </TableCell>
                <TableCell>
                  <Time value={token.created_at} />
                </TableCell>
                <TableCell>
                  {token.last_used_at ? (
                    <Time value={token.last_used_at} />
                  ) : (
                    <span className="text-muted-foreground">
                      {t("ui.never")}
                    </span>
                  )}
                </TableCell>
                <TableCell className="text-center">
                  <div className="inline-flex text-left">
                    <InlineConfirm
                      label={t("ui.revoke")}
                      question={t("ui.revoke_question", { name: token.name })}
                      confirm={t("ui.confirm_revoke")}
                      busy={busy === token.id}
                      onConfirm={() => revoke(token.id)}
                    />
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <p className="text-sm text-muted-foreground">{t("ui.no_tokens")}</p>
      )}
    </div>
  );
}
