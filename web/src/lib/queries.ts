import { useQuery } from "@tanstack/react-query";
import { api } from "./api";
import { useI18n } from "./i18n";
import type { Mailbox, Status, Delivery } from "./types";
export function useMailboxes() {
  const { language } = useI18n();
  return useQuery({
    queryKey: ["mailboxes", language],
    queryFn: ({ signal }) =>
      api<{ mailboxes: Mailbox[] }>("/mailboxes", { signal }),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}
export function useStatus() {
  const { language } = useI18n();
  return useQuery({
    queryKey: ["status", language],
    queryFn: ({ signal }) => api<Status>("/status", { signal }),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}
export function useDeliveries() {
  const { language } = useI18n();
  return useQuery({
    queryKey: ["deliveries", language],
    queryFn: ({ signal }) =>
      api<{ deliveries: Delivery[] }>("/deliveries", { signal }),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  });
}
