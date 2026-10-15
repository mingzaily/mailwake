import { FolderName } from "./folder-name";
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
  names = {},
  disabled = false,
  onChange,
}: {
  folders: Folder[];
  available: string[];
  roles?: Record<string, string>;
  names?: Record<string, string>;
  disabled?: boolean;
  onChange: (folders: Folder[]) => void;
}) {
  const { t } = useI18n();
  const id = useId();
  const paths = [
    ...new Set([...available, ...folders.map((folder) => folder.name)]),
  ].sort((a, b) => a.localeCompare(b));
  const selectedFolders = new Map(
    folders.map((folder) => [folder.name, folder]),
  );
  return (
    <FieldSet disabled={disabled}>
      <FieldLegend>{t("ui.choose_folders")}</FieldLegend>
      {paths.length ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("ui.folder")}</TableHead>
              <TableHead>{t("ui.check")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {paths.map((name, index) => {
              const translated = folderLabel(name, roles, t, names);
              const label =
                translated === name ? name : `${translated} (${name})`;
              const selected = selectedFolders.get(name);
              return (
                <TableRow key={name}>
                  <TableCell>
                    <Field orientation="horizontal">
                      <Checkbox
                        id={`${id}-${index}`}
                        disabled={disabled}
                        checked={!!selected}
                        onCheckedChange={(checked) =>
                          onChange(
                            checked === true
                              ? [...folders, { name, check: "5m" }]
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
                        <FolderName name={name} roles={roles} names={names} />
                      </FieldLabel>
                    </Field>
                  </TableCell>
                  <TableCell>
                    <NativeSelect
                      className="min-w-36"
                      aria-label={`${label} ${t("ui.check")}`}
                      value={selected?.check ?? "5m"}
                      disabled={disabled || !selected}
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
