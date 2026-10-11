import { folderLabel } from "@/lib/folders";
import { useI18n } from "@/lib/i18n";

export function FolderName({
  name,
  roles = {},
}: {
  name: string;
  roles?: Record<string, string>;
}) {
  const { t } = useI18n();
  const label = folderLabel(name, roles, t);
  return (
    <span title={name}>
      {label}
      {label !== name && (
        <span className="font-normal text-muted-foreground"> ({name})</span>
      )}
    </span>
  );
}
