import { z } from "zod";
import type { Folder } from "./types";
export const mailboxSchema = z.object({
  label: z
    .string()
    .trim()
    .min(1, "mailbox_label_invalid")
    .refine(
      (value) => Array.from(value).length <= 64 && !/[\r\n]/.test(value),
      "mailbox_label_invalid",
    ),
  host: z
    .string()
    .min(1, "config_imap_address_invalid")
    .regex(/^[^/\s]+$/, "config_imap_address_invalid"),
  port: z
    .number()
    .int()
    .min(1, "config_imap_address_invalid")
    .max(65535, "config_imap_address_invalid"),
  username: z.string().trim().min(1, "config_account_required"),
  password: z.string(),
  connection_limit: z
    .number()
    .int()
    .min(2, "connection_limit_invalid")
    .max(100, "connection_limit_invalid"),
});
export type MailboxForm = z.infer<typeof mailboxSchema>;
export function mailboxPayload(values: MailboxForm, revision?: number) {
  return {
    ...values,
    password: values.password || undefined,
    ...(revision !== undefined ? { revision } : {}),
  };
}
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
