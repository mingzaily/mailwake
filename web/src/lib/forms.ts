import type { Folder } from "./types";
export const checks = ["realtime", "5m", "15m"] as const;
export const previewOptions = ["off", "subject"] as const;
export function connectionUsage(folders: Folder[]) {
  return (
    folders.filter((folder) => folder.check === "realtime").length +
    Number(folders.some((folder) => folder.check !== "realtime"))
  );
}
export function subscriptionsPayload(folders: Folder[], revision: number) {
  return {
    revision,
    folders: folders.map(({ name, check }) => ({ name, check })),
  };
}
export const errorFields: Record<string, string> = {
  mailbox_label_invalid: "label",
  config_imap_address_invalid: "host",
  config_account_required: "username",
  credential_required: "password",
  connection_limit_invalid: "connection_limit",
  current_password_invalid: "current_password",
  setup_code_invalid: "code",
};

export function passwordValidation(value: string) {
  if (Array.from(value).length < 12) return "ui.password_length";
  if (new TextEncoder().encode(value).length > 1024)
    return "ui.password_too_long";
}
