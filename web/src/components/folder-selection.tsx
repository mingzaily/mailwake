import { folderLabel } from "@/lib/folders";
import { useId } from "react";
import { Checkbox } from "./ui/checkbox";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Field, FieldLabel } from "./ui/field";
import { checks } from "@/lib/forms";
import { useI18n } from "@/lib/i18n";
import type { Check, Folder } from "@/lib/types";
import { FieldSet, FieldLegend } from "./ui/field";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import { EmptyState } from "./common";

export function FolderSelection({
  folders,
  available,
  roles = {},
  onChange,
}: {
  folders: Folder[];
  available: string[];
  roles?: Record<string, string>;
  onChange: (folders: Folder[]) => void;
}) {
  const { t } = useI18n();
  const id = useId();
  const names = [
    ...new Set([...available, ...folders.map((folder) => folder.name)]),
  ].sort((a, b) => a.localeCompare(b));
  const labels = names.map((name) => folderLabel(name, roles, t));
  const labelCounts = new Map<string, number>();
  for (const label of labels)
    labelCounts.set(label, (labelCounts.get(label) ?? 0) + 1);
  const selectedFolders = new Map(
    folders.map((folder) => [folder.name, folder]),
  );
  return (
    <FieldSet>
      <FieldLegend>{t("ui.choose_folders")}</FieldLegend>
      {names.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("ui.folder")}</TableHead>
              <TableHead>{t("ui.check")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {names.map((name, index) => {
              const translated = labels[index];
              const label =
                translated !== name && labelCounts.get(translated)! > 1
                  ? `${translated} (${name})`
                  : translated;
              const selected = selectedFolders.get(name);
              return (
                <TableRow key={name}>
                  <TableCell>
                    <Field orientation="horizontal">
                      <Checkbox
                        id={`${id}-${index}`}
                        checked={!!selected}
                        onCheckedChange={(checked) =>
                          onChange(
                            checked === true
                              ? [...folders, { name, check: "realtime" }]
                              : folders.filter(
                                  (folder) => folder.name !== name,
                                ),
                          )
                        }
                      />
                      <FieldLabel
                        htmlFor={`${id}-${index}`}
                        className="min-w-0 break-all"
                      >
                        <span title={name}>{label}</span>
                      </FieldLabel>
                    </Field>
                  </TableCell>
                  <TableCell>
                    <NativeSelect
                      className="min-w-36"
                      aria-label={`${label} ${t("ui.check")}`}
                      value={selected?.check ?? "realtime"}
                      disabled={!selected}
                      onChange={(event) =>
                        onChange(
                          folders.map((folder) =>
                            folder.name === name
                              ? {
                                  ...folder,
                                  check: event.target.value as Check,
                                }
                              : folder,
                          ),
                        )
                      }
                    >
                      {checks.map((check) => (
                        <NativeSelectOption key={check} value={check}>
                          {t(`ui.check_${check}`)}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      ) : (
        <EmptyState />
      )}
    </FieldSet>
  );
}
