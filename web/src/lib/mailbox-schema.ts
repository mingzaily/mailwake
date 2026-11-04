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
    .trim()
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
export const outlookHost = "outlook.office365.com";
export function presetHost({ host, port }: Pick<MailboxForm, "host" | "port">) {
  return port === 993 ? host.trim().toLowerCase() : undefined;
}
export function mailboxConnection(values: MailboxForm, authorization?: string) {
  const outlook = presetHost(values) === outlookHost;
  return {
    host: outlook ? outlookHost : values.host.trim(),
    port: values.port,
    username: values.username.trim(),
    password: outlook ? undefined : values.password || undefined,
    ...(outlook && authorization
      ? { outlook_authorization: authorization }
      : {}),
  };
}
export function mailboxPayload(
  values: MailboxForm,
  revision?: number,
  authorization?: string,
) {
  return {
    ...values,
    ...mailboxConnection(values, authorization),
    ...(revision !== undefined ? { revision } : {}),
  };
}
