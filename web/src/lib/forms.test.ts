import { expect, test } from "vitest";
import {
  connectionUsage,
  errorFields,
  mailboxPayload,
  mailboxSchema,
  previewOptions,
  subscriptionsPayload,
} from "./forms";

test("scheduled folders share a connection while realtime folders each use one", () => {
  expect(connectionUsage([])).toBe(0);
  expect(
    connectionUsage([
      { name: "A", check: "5m" },
      { name: "B", check: "15m" },
    ]),
  ).toBe(1);
  expect(
    connectionUsage([
      { name: "A", check: "realtime" },
      { name: "B", check: "5m" },
      { name: "C", check: "15m" },
    ]),
  ).toBe(2);
});
test("mailbox validates required label, host and connection limits", () => {
  const good = {
    label: "Work",
    host: "imap.test",
    port: 993,
    username: "user",
    password: "",
    connection_limit: 10,
  };
  expect(mailboxSchema.safeParse(good).success).toBe(true);
  for (const bad of [
    { label: "" },
    { host: "bad host" },
    { port: 65536 },
    { connection_limit: 1 },
    { label: "x".repeat(65) },
  ])
    expect(mailboxSchema.safeParse({ ...good, ...bad }).success).toBe(false);
  expect(mailboxPayload(good, 3)).toEqual({
    ...good,
    password: undefined,
    revision: 3,
  });
  expect(errorFields.current_password_invalid).toBe("current_password");
  expect(errorFields.setup_code_invalid).toBe("code");
});
test("subscription revisions, check methods and subject-only preview retain the API contract", () => {
  expect(subscriptionsPayload([{ name: "Bank", check: "15m" }], 7)).toEqual({
    revision: 7,
    folders: [{ name: "Bank", check: "15m" }],
  });
  expect(previewOptions).toEqual(["off", "subject"]);
});
