export type Language = "en" | "zh-CN";
export type Check = "realtime" | "5m" | "15m";
export type Folder = { name: string; check: Check };
export type Subscriptions = { revision: number; folders: Folder[] };
export type Credential = { configured: boolean };
export type Mailbox = {
  id: string;
  label: string;
  revision: number;
  host: string;
  port: number;
  username: string;
  password: Credential;
  connection_limit: number;
  connections_in_use: number;
};
export type Failure = {
  code: string;
  message: string;
  params?: Record<string, string>;
};
export type FolderStatus = {
  mailbox_id: string;
  mailbox_label: string;
  folder: string;
  check: Check;
  state: string;
  mode?: string;
  last_check?: string;
  next_check?: string;
  last_error?: Failure;
  notice?: Failure;
};
export type Status = {
  folders: FolderStatus[];
  delivery: { pending: number; accepted: number; dead: number };
  channel: string;
  notices: Record<string, Failure>;
};
export type Delivery = {
  message?: {
    mailbox_id: string;
    mailbox_label: string;
    folder: string;
    subject: string | null;
    test: boolean;
  };
  channel?: string;
  devices?: {
    pairing_id: string;
    device_name: string;
    status: string;
    error_code?: string;
    relay_id?: string;
  }[];
  id: string;
  state: string;
  attempts: number;
  next_attempt?: string;
  created_at: string;
  accepted_at?: string;
  last_error?: Failure;
};
export type DeliverySettings = {
  native_available?: boolean;
  revision: number;
  channel: string;
  preview: "off" | "subject";
  retry_count: number;
  language: Language;
  bark: { endpoint: string; key: Credential };
  pushover: { token: Credential; user: Credential };
  webhook: { url: Credential; secret: Credential };
};
export type Token = {
  id: string;
  name: string;
  created_at: string;
  last_used_at: string | null;
};
export type LogEntry = {
  seq: number;
  time: string;
  level: string;
  message: string;
  attrs: Record<string, unknown>;
};
export type LogPage = { entries: LogEntry[]; next: number };
export type Diagnostics = {
  generated_at: string;
  build: {
    version: string;
    revision: string;
    go_version: string;
    os: string;
    arch: string;
  };
};
