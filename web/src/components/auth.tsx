import { useState } from "react";
import { useForm } from "react-hook-form";
import { api, APIError, setCSRF } from "@/lib/api";
import { passwordValidation } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import { FieldGroup } from "./ui/field";
import { BusyButton, ErrorNotice, TextField } from "./common";

type Values = { username: string; password: string; code: string };
export function AuthForm({
  setup,
  loadingError,
  onSuccess,
}: {
  setup: boolean;
  loadingError?: unknown;
  onSuccess: () => void;
}) {
  const { t } = useI18n();
  const [error, setError] = useState<unknown>();
  const [submitted, setSubmitted] = useState(false);
  const {
    register,
    handleSubmit,
    setError: fieldError,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    defaultValues: { username: "", password: "", code: "" },
  });
  async function submit(values: Values) {
    setSubmitted(true);
    setError(undefined);
    try {
      const result = await api<{ csrf_token: string }>(
        setup ? "/setup" : "/session",
        {
          method: "POST",
          public: true,
          body: setup
            ? values
            : { username: values.username, password: values.password },
        },
      );
      setCSRF(result.csrf_token);
      onSuccess();
    } catch (error) {
      if (
        error instanceof APIError &&
        error.failure.code === "setup_code_invalid"
      )
        fieldError("code", { message: error.message }, { shouldFocus: true });
      else setError(error);
    }
  }
  return (
    <form onSubmit={handleSubmit(submit)} noValidate>
      {setup && (
        <p className="text-muted-foreground mb-5">
          {t("ui.setup_step", { step: 1, total: 3 })}
        </p>
      )}
      <FieldGroup>
        <ErrorNotice error={submitted ? error : loadingError} />
        {setup && (
          <TextField
            label={t("ui.setup_code")}
            hint={t("ui.setup_code_hint")}
            autoComplete="off"
            spellCheck={false}
            error={errors.code?.message}
            {...register("code", { required: t("ui.required") })}
          />
        )}
        <TextField
          label={t("ui.username")}
          autoComplete="username"
          spellCheck={false}
          error={errors.username?.message}
          {...register("username", { required: t("ui.required") })}
        />
        <TextField
          label={t("ui.password")}
          type="password"
          autoComplete={setup ? "new-password" : "current-password"}
          error={errors.password?.message}
          hint={setup ? t("ui.password_length") : undefined}
          {...register("password", {
            required: t("ui.required"),
            ...(setup
              ? {
                  validate: (value) => {
                    const error = passwordValidation(value);
                    return error ? t(error) : true;
                  },
                }
              : {}),
          })}
        />
        <BusyButton type="submit" busy={isSubmitting}>
          {t(setup ? "ui.create_admin" : "ui.login")}
        </BusyButton>
      </FieldGroup>
    </form>
  );
}
