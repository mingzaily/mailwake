import { z } from "zod";

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
