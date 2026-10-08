import type { Failure, Language } from "./types";

export class APIError extends Error {
  constructor(
    readonly status: number,
    readonly failure: Failure,
    readonly retryAfter = 0,
  ) {
    super(failure.message);
  }
  get conflict() {
    return ["settings_conflict", "subscriptions_conflict"].includes(
      this.failure.code,
    );
  }
}
let csrf = "";
let language: Language | undefined;
let expired = () => {};
export function configureAPI(options: {
  language?: Language;
  onExpired?: () => void;
}) {
  if (options.language) language = options.language;
  if (options.onExpired) expired = options.onExpired;
}
export function setCSRF(value: string) {
  csrf = value;
}
export async function api<T>(
  path: string,
  options: {
    method?: string;
    body?: unknown;
    public?: boolean;
    signal?: AbortSignal;
  } = {},
): Promise<T> {
  const method = options.method ?? "GET";
  const headers = new Headers({ Accept: "application/json" });
  if (language) headers.set("Accept-Language", language);
  if (options.body !== undefined)
    headers.set("Content-Type", "application/json");
  if (method !== "GET" && csrf) headers.set("X-CSRF-Token", csrf);
  const response = await fetch(`/api/v1${path}`, {
    method,
    headers,
    credentials: "same-origin",
    signal: options.signal,
    ...(options.body !== undefined
      ? { body: JSON.stringify(options.body) }
      : {}),
  });
  if (response.status === 204) return undefined as T;
  if (response.status === 401 && !options.public) {
    csrf = "";
    expired();
  }
  const invalidResponse = () =>
    new APIError(response.status, {
      code: "request_failed",
      message: "",
      params: { reason: "invalid_response" },
    });
  let data: unknown;
  try {
    data = await response.json();
  } catch (error) {
    if (options.signal?.aborted) throw error;
    throw invalidResponse();
  }
  if (!response.ok) {
    const failure =
      typeof data === "object" && data !== null && "error" in data
        ? data.error
        : undefined;
    if (
      !failure ||
      typeof failure !== "object" ||
      !("code" in failure) ||
      typeof failure.code !== "string" ||
      !("message" in failure) ||
      typeof failure.message !== "string"
    )
      throw invalidResponse();
    const detail = failure as Failure;
    const delay = response.headers.get("Retry-After");
    const seconds = delay
      ? /^\d+$/.test(delay)
        ? Number(delay)
        : Math.max(0, Math.ceil((Date.parse(delay) - Date.now()) / 1000))
      : Number(detail.params?.retry_after ?? 1);
    throw new APIError(response.status, detail, seconds);
  }
  return data as T;
}
export function downloadJSON(value: unknown, name: string) {
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(value, null, 2)], { type: "application/json" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
