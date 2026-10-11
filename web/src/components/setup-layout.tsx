import type { ReactNode } from "react";
import { useI18n } from "@/lib/i18n";
import { AppearanceControls } from "./appearance-controls";
import { Brand } from "./shell";
import { Panel } from "./common";

export function SetupLayout({
  step,
  children,
  footer,
}: {
  step?: number;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const { t } = useI18n();
  const titles = [
    "ui.setup_intro",
    "ui.create_admin",
    "ui.setup_mailbox",
    "ui.setup_folders",
  ];
  const activeIndex = step === undefined ? 0 : step + 1;
  return (
    <SetupFrame>
      <div className="flex flex-col gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">
          {t("ui.setup")}
        </h1>
        {step !== undefined && (
          <p className="text-sm text-muted-foreground" role="status">
            {t("ui.setup_step", { step: step + 1, total: 3 })}
          </p>
        )}
        <ol className="flex gap-3" aria-label={t("ui.setup_progress")}>
          {titles.map((title, index) => (
            <li
              key={title}
              aria-current={index === activeIndex ? "step" : undefined}
              className="min-w-0 flex-1 border-b-2 border-border pb-3 text-sm text-muted-foreground aria-[current=step]:border-primary aria-[current=step]:text-primary"
            >
              {t(title)}
            </li>
          ))}
        </ol>
      </div>
      <Panel title={step === undefined ? undefined : t(titles[activeIndex])}>
        <div className="p-4 sm:p-6">{children}</div>
      </Panel>
      {footer}
    </SetupFrame>
  );
}

export function SetupFrame({ children }: { children: ReactNode }) {
  return (
    <main className="flex min-h-svh justify-center p-4 sm:p-6 md:p-10">
      <div className="flex w-full max-w-3xl flex-col gap-6">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <Brand />
          <AppearanceControls compact />
        </header>
        {children}
      </div>
    </main>
  );
}
