import { useSyncExternalStore } from "react";
const listen = (fn: () => void) => {
  window.addEventListener("popstate", fn);
  return () => window.removeEventListener("popstate", fn);
};
const snapshot = () => location.pathname;
export function useRoute() {
  const path = useSyncExternalStore(listen, snapshot);
  const [page = "agents", id = ""] = path.split("/").filter(Boolean);
  return { page, id };
}
export function navigate(path: string) {
  history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
