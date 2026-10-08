import { useId } from "react";
import { Brand } from "./shell";
import { useI18n } from "@/lib/i18n";
import { AppearanceControls } from "./appearance-controls";
import { AuthForm } from "./auth";
import loginIllustration from "@/assets/login-mail.webp";
import loginDarkIllustration from "@/assets/login-mail-dark.webp";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "./ui/card";

export function AuthScreen({
  setup,
  error,
  onSuccess,
}: {
  setup: boolean;
  error?: unknown;
  onSuccess: () => void;
}) {
  const { t } = useI18n();
  const title = useId();
  if (!setup) {
    return (
      <div className="grid min-h-svh lg:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-4 p-6 md:p-10">
          <header className="flex justify-center md:justify-start">
            <Brand />
          </header>
          <main className="flex flex-1 items-center justify-center py-6">
            <div className="flex w-full max-w-xs flex-col gap-6">
              <div className="flex flex-col items-center gap-2 text-center">
                <h1 className="text-2xl font-semibold">
                  {t("ui.login_title")}
                </h1>
                <p className="text-sm text-balance text-muted-foreground">
                  {t("ui.login_description")}
                </p>
              </div>
              <AuthForm
                setup={false}
                loadingError={error}
                onSuccess={onSuccess}
              />
            </div>
          </main>
          <AppearanceControls compact />
        </div>
        <div className="relative hidden bg-muted lg:block" aria-hidden="true">
          <img
            src={loginIllustration}
            alt=""
            className="absolute inset-0 size-full object-cover dark:hidden"
          />
          <img
            src={loginDarkIllustration}
            alt=""
            className="absolute inset-0 hidden size-full object-cover dark:block"
          />
        </div>
      </div>
    );
  }
  return (
    <div className="flex min-h-svh items-center justify-center p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <header className="flex justify-center">
          <Brand />
        </header>
        <main>
          <Card role="region" aria-labelledby={title}>
            <CardHeader className="text-center">
              <CardTitle className="text-2xl">
                <h1 id={title}>{t("ui.setup")}</h1>
              </CardTitle>
              <CardDescription>
                {t("ui.setup_description")}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <AuthForm
                setup={setup}
                loadingError={error}
                onSuccess={onSuccess}
              />
            </CardContent>
          </Card>
        </main>
        <AppearanceControls compact />
      </div>
    </div>
  );
}
