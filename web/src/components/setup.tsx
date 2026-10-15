import { useState } from "react";
import { useI18n } from "@/lib/i18n";
import type { Mailbox } from "@/lib/types";
import { MailboxEditor } from "./mailbox-form";
import { SubscriptionsEditor } from "./subscriptions-form";
import { Button } from "./ui/button";
import { SetupLayout } from "./setup-layout";
export function SetupWizard({ done }: { done: () => void }) {
  const { t } = useI18n();
  const [step, setStep] = useState(0);
  const [mailbox, setMailbox] = useState<Mailbox>();
  const [names, setNames] = useState<Record<string, string>>({});
  const [roles, setRoles] = useState<Record<string, string>>({});
  const [folders, setFolders] = useState<string[]>([]);
  return (
    <SetupLayout
      step={step + 1}
      footer={
        <div>
          <Button variant="ghost" onClick={done}>
            {t("ui.skip_step")}
          </Button>
        </div>
      }
    >
      {step === 0 && (
        <MailboxEditor
          showDiscoveredFolders={false}
          onSaved={(mailbox, folders, roles, names) => {
            setNames(names ?? {});
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
          names={names}
          onSaved={done}
        />
      )}
    </SetupLayout>
  );
}
