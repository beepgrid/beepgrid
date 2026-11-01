import { useState } from "react";
import type { Identity, Target } from "../contracts";
import { useCapabilities, useCollection, usePostResource } from "../data";
import {
  Badge,
  Button,
  ErrorView,
  IdentityIcon,
  Loading,
  Search,
  Tabs,
} from "../ui";

type Contact = {
  target: Target;
  name: string;
  message: boolean;
  ask: boolean;
  routes: { kind: string; mode: string; enabled: boolean }[];
};
const names: Record<string, string> = {
  native: "Workspace",
  slack: "Slack",
  telegram: "Telegram",
  discord: "Discord",
  email: "Email",
  webhook: "Webhook",
};

export default function AgentContacts({ agent }: { agent: Identity }) {
  const [kind, setKind] = useState("people"),
    [search, setSearch] = useState(""),
    [page, setPage] = useState(0);
  const caps = useCapabilities({ kind: "agent", id: agent.id });
  const allowed = !!caps.data?.operations.view_contact_access;
  const directory = useCollection<Identity>(kind, search, {}, allowed);
  const contacts = usePostResource<{ contacts: Contact[] }>(
    `/agents/${agent.id}/contacts`,
    {
      targets: (directory.data?.pages[page]?.items || []).map((p) => ({
        kind: p.kind,
        id: p.id,
      })),
    },
    allowed && !directory.isPending,
  );
  return (
    <section className="profile-section reachable-contacts">
      <header>
        <h2>Who this Agent can reach</h2>
      </header>
      {caps.isPending ? (
        <Loading />
      ) : !allowed ? (
        <p className="help">
          Contact access is restricted to authorized viewers.
        </p>
      ) : (
        <>
          <div className="contacts-toolbar">
            <Tabs
              value={kind}
              onChange={(value) => {
                setKind(value);
                setSearch("");
                setPage(0);
              }}
              options={[
                { id: "people", label: "People" },
                { id: "agents", label: "Agents" },
                { id: "services", label: "Services" },
              ]}
            />
            <Search
              value={search}
              onChange={(value) => {
                setSearch(value);
                setPage(0);
              }}
              placeholder="Search contacts"
            />
          </div>
          <ErrorView error={directory.error || contacts.error} />
          {directory.isPending || contacts.isPending ? (
            <Loading />
          ) : contacts.data?.contacts.length ? (
            <div className="reachable-list">
              {contacts.data.contacts.map((p) => (
                <div className="reachable-contact" key={p.target.id}>
                  <IdentityIcon
                    kind={p.target.kind}
                    id={p.target.id}
                    label={p.name} icon={String(directory.items.find((i) => i.id === p.target.id)?.metadata?.appearance_icon || "")}
                  />
                  <div>
                    <strong>{p.name}</strong>
                    <small>
                      {p.message && p.ask
                        ? "Messages and reply requests"
                        : p.ask
                          ? "Reply requests"
                          : "Messages"}
                    </small>
                  </div>
                  <div className="reachable-routes">
                    {p.routes.map((r, i) => (
                      <Badge
                        key={r.kind + r.mode + i}
                        tone={r.enabled ? "neutral" : "warn"}
                      >
                        {names[r.kind] || "External channel"}
                        {!r.enabled
                          ? " · Disabled"
                          : r.mode === "fallback"
                            ? " · Fallback"
                            : r.mode === "copy"
                              ? " · Copy"
                              : ""}
                      </Badge>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          ) : !directory.error && !contacts.error ? (
            <p className="profile-inline-empty">
              No permitted contacts in these results
            </p>
          ) : null}
          {page > 0 ||
          directory.hasNextPage ||
          (directory.data?.pages.length || 0) > 1 ? (
            <div className="actions contacts-pagination">
              <Button
                disabled={page === 0}
                onClick={() => setPage((p) => p - 1)}
              >
                Previous
              </Button>
              <Button
                disabled={
                  directory.isFetchingNextPage ||
                  (!directory.hasNextPage &&
                    page >= (directory.data?.pages.length || 1) - 1)
                }
                onClick={async () => {
                  if (page < (directory.data?.pages.length || 0) - 1) {
                    setPage((p) => p + 1);
                    return;
                  }
                  const result = await directory.fetchNextPage();
                  if (
                    !result.isError &&
                    (result.data?.pages.length || 0) > page + 1
                  )
                    setPage((p) => p + 1);
                }}
              >
                Next
              </Button>
            </div>
          ) : null}
          <p className="help">
            Channels follow each recipient’s delivery preferences and current
            contact permissions.
          </p>
        </>
      )}
    </section>
  );
}
