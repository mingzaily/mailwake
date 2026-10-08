import { useSyncExternalStore } from "react";
const subscribe = (callback: () => void) => {
  addEventListener("hashchange", callback);
  return () => removeEventListener("hashchange", callback);
};
export function useRoute() {
  return useSyncExternalStore(
    subscribe,
    () => location.hash.slice(2) || "overview",
  ).split("/");
}
export function navigate(route: string) {
  location.hash = `/${route}`;
}
