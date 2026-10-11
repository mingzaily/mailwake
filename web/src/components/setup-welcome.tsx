import { useI18n } from "@/lib/i18n";
import { SetupLayout } from "./setup-layout";
import { Button } from "./ui/button";

export function SetupWelcome({ onContinue }: { onContinue: () => void }) {
  const { t } = useI18n();
  const steps = [
    ["ui.create_admin", "ui.welcome_admin"],
    ["ui.setup_mailbox", "ui.welcome_mailbox"],
    ["ui.setup_folders", "ui.welcome_folders"],
  ];
  return (
    <SetupLayout>
      <div className="setup-welcome-enter flex flex-col gap-8">
        <div className="flex flex-col gap-3">
          <h2 className="text-2xl font-semibold tracking-tight">
            {t("ui.welcome_title")}
          </h2>
          <p className="max-w-prose text-sm leading-7 text-muted-foreground">
            {t("ui.welcome_description")}
          </p>
        </div>
        <ol className="flex flex-col gap-6">
          {steps.map(([title, description], index) => (
            <li key={title} className="flex items-start gap-4">
              <span
                aria-hidden="true"
                className="flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-sm font-medium text-primary"
              >
                {index + 1}
              </span>
              <div className="flex flex-col gap-1">
                <h3 className="text-sm font-medium">{t(title)}</h3>
                <p className="text-sm leading-6 text-muted-foreground">
                  {t(description)}
                </p>
              </div>
            </li>
          ))}
        </ol>
        <div className="flex flex-col items-start gap-5 border-t pt-6">
          <p className="text-sm leading-6 text-muted-foreground">
            {t("ui.welcome_prepare")}
          </p>
          <Button className="self-end" onClick={onContinue}>
            {t("ui.continue")}
          </Button>
        </div>
      </div>
    </SetupLayout>
  );
}
