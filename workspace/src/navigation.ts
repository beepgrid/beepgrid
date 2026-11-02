import { useSyncExternalStore } from "react";
import { beginNavigation } from "./performance";
const listen = (fn: () => void) => {
  window.addEventListener("popstate", fn);
  return () => window.removeEventListener("popstate", fn);
};
let base = "";
export function configureWorkspaceBase(value: string) {
  if (value && !/^\/w\/[a-z][a-z0-9_]{2,79}$/.test(value))
    throw Error("Invalid workspace base");
  base = value;
}
export function workspaceURL(path: string) {
  return base + path;
}
const snapshot = () => location.pathname;
export function useRoute() {
  const path = useSyncExternalStore(listen, snapshot);
  const [page = "conversations", id = ""] = (
    base && path.startsWith(base + "/") ? path.slice(base.length) : path
  )
    .split("/")
    .filter(Boolean);
  return { page, id };
}
export function navigate(path: string) {
  beginNavigation(path);
  history.pushState(null, "", workspaceURL(path));
  window.dispatchEvent(new PopStateEvent("popstate"));
}
