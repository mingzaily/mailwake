import { useState } from "react";
import { useI18n } from "@/lib/i18n";
import type { Mailbox } from "@/lib/types";
import { AppearanceControls } from "./appearance-controls";
import { Brand } from "./shell";
import { MailboxEditor } from "./mailbox-form";
import { SubscriptionsEditor } from "./subscriptions-form";
import { Button } from "./ui/button";
import { Panel } from "./common";
export function SetupWizard({ done }: { done: () => void }) {
  const { t } = useI18n();
  const [step, setStep] = useState(0);
  const [mailbox, setMailbox] = useState<Mailbox>();
  const [roles, setRoles] = useState<Record<string, string>>({});
  const [folders, setFolders] = useState<string[]>([]);
  const titles = ["ui.setup_mailbox", "ui.setup_folders"];
  return (
    <main className="flex min-h-svh items-start justify-center p-4 sm:p-6 md:p-10">
      <div className="flex w-full max-w-[960px] flex-col gap-6">
        <Brand />
        <AppearanceControls compact />
        <h1 className="text-2xl font-semibold tracking-tight">
          {t("ui.setup")}
        </h1>
        <p className="text-muted-foreground" role="status">
          {t("ui.setup_step", { step: step + 2, total: 3 })}
        </p>
        <div
          className="flex flex-wrap gap-2"
          aria-label={t("ui.setup_progress")}
        >
          {["ui.create_admin", ...titles].map((title, index) => (
            <span
              className="flex-1 border-b-2 border-border pb-2 text-xs text-muted-foreground aria-[current=step]:border-primary aria-[current=step]:text-primary"
              key={title}
              aria-current={index === step + 1 ? "step" : undefined}
            >
              {t(title)}
            </span>
          ))}
        </div>
        <Panel title={t(titles[step])}>
          <div className="p-4 md:p-5">
            {step === 0 && (
              <MailboxEditor
                onSaved={(mailbox, folders, roles) => {
                  setRoles(roles ?? {});
                  setMailbox(mailbox);
                  setFolders(folders);
                  setStep(1);
                }}
              />
            )}
            {step === 1 && mailbox && (
              <SubscriptionsEditor
                mailbox={mailbox}
                discovered={folders}
                roles={roles}
                onSaved={done}
              />
            )}
          </div>
        </Panel>
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="ghost" onClick={done}>
            {t("ui.skip_step")}
          </Button>
        </div>
      </div>
    </main>
  );
}
