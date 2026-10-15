import { createContext, useContext } from "react";
import type { Mailbox } from "@/lib/types";
import { folderLabel } from "@/lib/folders";
import { useI18n } from "@/lib/i18n";

export const FolderDisplayContext = createContext<
  Pick<Mailbox, "id" | "folder_display">[]
>([]);

export function FolderName({
  name,
  roles,
  names,
  mailboxId,
}: {
  name: string;
  roles?: Record<string, string>;
  names?: Record<string, string>;
  mailboxId?: string;
}) {
  const { t } = useI18n();
  const mailboxes = useContext(FolderDisplayContext);
  const display = mailboxes.find((box) => box.id === mailboxId)?.folder_display;
  const label = folderLabel(
    name,
    roles ?? display?.folder_roles ?? {},
    t,
    names ?? display?.folder_names,
  );
  return (
    <span title={name}>
      {label}
      {label !== name && (
        <span className="font-normal text-muted-foreground"> ({name})</span>
      )}
    </span>
  );
}
