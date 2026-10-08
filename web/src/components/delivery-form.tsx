import { useState } from "react";
import { useForm } from "react-hook-form";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { previewOptions } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import type { DeliverySettings } from "@/lib/types";
import { FieldGroup, FieldLegend, FieldSet } from "./ui/field";
import {
  BusyButton,
  ErrorNotice,
  SelectField,
  TextField,
  useToast,
} from "./common";

type Values = {
  channel: string;
  preview: "off" | "subject";
  retry_count: number;
  language: string;
  endpoint: string;
  bark_key: string;
  pushover_token: string;
  pushover_user: string;
  webhook_url: string;
  webhook_secret: string;
};
export function deliveryPayload(values: Values, revision: number) {
  return {
    revision,
    channel: values.channel,
    preview: values.preview,
    retry_count: values.retry_count,
    language: values.language,
    bark: { endpoint: values.endpoint, key: values.bark_key || undefined },
    pushover: {
      token: values.pushover_token || undefined,
      user: values.pushover_user || undefined,
    },
    webhook: {
      url: values.webhook_url || undefined,
      secret: values.webhook_secret || undefined,
    },
  };
}
export function DeliveryEditor({ onSaved }: { onSaved?: () => void }) {
  const query = useQuery({
    queryKey: ["delivery-settings"],
    queryFn: () => api<DeliverySettings>("/settings/delivery"),
    refetchOnWindowFocus: false,
  });
  const { t } = useI18n();
  if (!query.data)
    return (
      <>
        <ErrorNotice error={query.error} />
        <p>{t("ui.loading")}</p>
      </>
    );
  return (
    <DeliveryFields
      key={query.data.revision}
      initial={query.data}
      reload={() => void query.refetch()}
      onSaved={onSaved}
    />
  );
}
function DeliveryFields({
  initial,
  reload,
  onSaved,
}: {
  initial: DeliverySettings;
  reload: () => void;
  onSaved?: () => void;
}) {
  const { t } = useI18n();
  const toast = useToast();
  const client = useQueryClient();
  const [error, setError] = useState<unknown>();
  const [testing, setTesting] = useState(false);
  const {
    register,
    watch,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    defaultValues: {
      channel: initial.channel || "bark",
      preview: initial.preview || "off",
      retry_count: initial.retry_count,
      language: initial.language || "en",
      endpoint: initial.bark.endpoint || "https://api.day.app",
      bark_key: "",
      pushover_token: "",
      pushover_user: "",
      webhook_url: "",
      webhook_secret: "",
    },
  });
  const channel = watch("channel");
  async function submit(values: Values, test = false) {
    setError(undefined);
    if (test) setTesting(true);
    try {
      await api(`/settings/delivery${test ? "/test" : ""}`, {
        method: test ? "POST" : "PUT",
        body: deliveryPayload(values, initial.revision),
      });
      if (!test) {
        await Promise.all([
          client.invalidateQueries({ queryKey: ["delivery-settings"] }),
          client.invalidateQueries({ queryKey: ["status"] }),
        ]);
        onSaved?.();
      }
      toast(t(test ? "ui.test_sent" : "ui.saved"));
    } catch (error) {
      setError(error);
    } finally {
      setTesting(false);
    }
  }
  const credential = (
    name:
      | "bark_key"
      | "pushover_token"
      | "pushover_user"
      | "webhook_url"
      | "webhook_secret",
    configured: boolean,
  ) => (
    <TextField
      key={name}
      label={t(`ui.${name}`)}
      type="password"
      autoComplete="new-password"
      hint={configured ? t("ui.credential_kept") : t("ui.credential_private")}
      error={errors[name]?.message}
      {...register(name, {
        validate: (value) =>
          value.trim() || configured ? true : t("credential_required"),
      })}
    />
  );
  return (
    <form
      className="max-w-[760px] flex flex-col gap-4"
      noValidate
      onSubmit={handleSubmit((values) => submit(values))}
    >
      <ErrorNotice error={error} reload={reload} />
      <FieldGroup>
        <SelectField label={t("ui.channel")} {...register("channel")}>
          {[
            "bark",
            "pushover",
            "webhook",
            ...(initial.native_available ? ["native"] : []),
          ].map((value) => (
            <option key={value} value={value}>
              {t(`ui.channel_${value}`)}
            </option>
          ))}
        </SelectField>
        {channel === "bark" && (
          <>
            <TextField
              label={t("ui.endpoint")}
              type="url"
              autoComplete="off"
              {...register("endpoint")}
            />
            {credential("bark_key", initial.bark.key.configured)}
          </>
        )}
        {channel === "pushover" && (
          <>
            {credential("pushover_token", initial.pushover.token.configured)}
            {credential("pushover_user", initial.pushover.user.configured)}
          </>
        )}
        {channel === "webhook" && (
          <>
            {credential("webhook_url", initial.webhook.url.configured)}
            {credential("webhook_secret", initial.webhook.secret.configured)}
          </>
        )}
        {channel === "native" ? (
          <p className="text-muted-foreground">{t("ui.native_encrypted")}</p>
        ) : (
          <FieldSet>
            <FieldLegend>{t("ui.preview")}</FieldLegend>
            <div className="flex flex-col gap-3">
              {previewOptions.map((value) => (
                <label key={value} className="flex min-h-8 items-center gap-3">
                  <input
                    className="size-4 accent-primary"
                    type="radio"
                    value={value}
                    {...register("preview")}
                  />
                  {t(`ui.preview_${value}`)}
                </label>
              ))}
            </div>
            <p className="text-muted-foreground text-xs">
              {t("ui.preview_hint")}
            </p>
          </FieldSet>
        )}
        <div className="grid grid-cols-1 gap-5 @md/field-group:grid-cols-2">
          <TextField
            label={t("ui.retry_count")}
            type="number"
            min={0}
            max={9}
            error={errors.retry_count?.message}
            {...register("retry_count", {
              valueAsNumber: true,
              min: { value: 0, message: t("config_retry_count_invalid") },
              max: { value: 9, message: t("config_retry_count_invalid") },
              validate: Number.isInteger,
            })}
          />
          <SelectField
            label={t("ui.notification_language")}
            {...register("language")}
          >
            <option value="en">{t("ui.language_en")}</option>
            <option value="zh-CN">{t("ui.language_zh-CN")}</option>
          </SelectField>
        </div>
      </FieldGroup>
      <div className="flex flex-wrap items-center gap-3">
        <BusyButton type="submit" busy={isSubmitting} disabled={testing}>
          {t(onSaved ? "ui.finish" : "ui.save")}
        </BusyButton>
        <BusyButton
          type="button"
          variant="outline"
          busy={testing}
          disabled={isSubmitting}
          onClick={() => void handleSubmit((values) => submit(values, true))()}
        >
          {t("ui.test_notification")}
        </BusyButton>
      </div>
    </form>
  );
}
