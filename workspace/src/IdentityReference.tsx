import { usePostResource } from "./data";
import type { Identity, Page } from "./contracts";
export function IdentityReference({ id }: { id: string }) {
  const kind = id.startsWith("agt_")
    ? "agent"
    : id.startsWith("svc_")
      ? "service"
      : "person";
  const q = usePostResource<Page<Identity>>("/query", {
    collection: { agent: "agents", service: "services", person: "people" }[
      kind
    ],
    filters: { id },
    limit: 1,
  });
  return (
    <span title={id}>
      {q.data?.items[0]?.name || `${kind} ${id.slice(-8)}`}
    </span>
  );
}
