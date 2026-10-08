import { expect, test } from "vitest";
import { deliveryPayload } from "./delivery-form";
test("delivery payload retains revision and write-only credentials, subject has no sender option", () => {
  const values = {
    channel: "bark",
    preview: "subject" as const,
    retry_count: 2,
    language: "zh-CN",
    endpoint: "https://api.day.app",
    bark_key: "",
    pushover_token: "",
    pushover_user: "",
    webhook_url: "",
    webhook_secret: "",
  };
  const payload = deliveryPayload(values, 8);
  expect(payload.revision).toBe(8);
  expect(payload.preview).toBe("subject");
  expect(payload.bark.key).toBeUndefined();
  expect(JSON.stringify(payload)).not.toContain("configured");
});
