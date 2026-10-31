export type AgentDeadlock = {
  dependencies: {
    request_id: string;
    conversation_id: string;
    from: Identity;
    to: Identity;
  }[];
};
import { useState } from "react";
import { useCollection, usePostResource } from "../data";
import type {
  Conversation,
  FleetSource,
  Identity,
  Request,
} from "../contracts";
import { navigate } from "../navigation";
import {
  Badge,
  Button,
  ErrorView,
  Icon,
  IdentityIcon,
  Loading,
  PageHeader,
  Search,
  Status,
  RequestStatus,
} from "../ui";

export default function Fleet() {
  const [search, setSearch] = useState("");
  const status = usePostResource<{
    agents: FleetSource[];
    as_of: string;
    deadlocks?: AgentDeadlock[];
  }>("/fleet/status", {});
  const requests = useCollection<Request>("requests", "", { state: "open" });
  const conversations = useCollection<Conversation>("conversations");
  const people = useCollection<Identity>("people");
  const all = status.data?.agents || [];
  const names = new Map(
    [...all.map((a) => a.agent), ...people.items].map((a) => [a.id, a]),
  );
  const pending = requests.items.map((request) => {
    const conversation = conversations.items.find(
      (c) => c.id === request.conversation_id,
    );
    return {
      request,
      peers: (conversation?.participants || []).filter(
        (p) => p.id !== request.requester_id,
      ),
    };
  });
  const deadlocks = status.data?.deadlocks || [];
  const cycles = new Set(
    deadlocks.flatMap((c) =>
      c.dependencies.flatMap((d) => [d.from.id, d.to.id]),
    ),
  );
  const shown = all.filter((a) =>
    a.agent.name.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <>
      <PageHeader
        title="Fleet"
        actions={
          <Button icon="agents" onClick={() => navigate("/agents")}>
            Manage Agents
          </Button>
        }
      />
      <ErrorView
        error={
          status.error || requests.error || conversations.error || people.error
        }
      />
      {status.isPending ? (
        <Loading label="Loading fleet" />
      ) : (
        <>
          <div className="fleet-summary">
            <div>
              <span>Visible Agents</span>
              <strong>{all.length}</strong>
            </div>
            <div>
              <span>
                <i className="fleet-dot" />
                Connected
              </span>
              <strong>{all.filter((a) => a.fresh).length}</strong>
            </div>
            <div>
              <span>Waiting for a reply</span>
              <strong>
                {requests.items.length}
                {requests.hasNextPage ? "+" : ""}
              </strong>
            </div>
          </div>
          {deadlocks.length ? (
            <section className="fleet-deadlock" id="blocking-dependencies">
              <header>
                <Icon name="activity" size={18} />
                <h2>Agents are waiting on each other</h2>
                <Badge tone="warn">{cycles.size} Agents blocked</Badge>
              </header>
              <p>
                These Agents reported blocking dependencies. Open a conversation
                to reply or help them move forward.
              </p>
              {deadlocks.map((cycle, i) => (
                <div key={i}>
                  {cycle.dependencies.map((d) => (
                    <button
                      key={d.request_id}
                      onClick={() =>
                        navigate("/conversations/" + d.conversation_id)
                      }
                    >
                      <IdentityIcon kind="agent" id={d.from.id} />
                      <strong>{d.from.name}</strong>
                      <Icon name="arrow" size={14} />
                      <span>{d.to.name}</span>
                      <Icon name="chevron" size={14} />
                    </button>
                  ))}
                </div>
              ))}
            </section>
          ) : null}
          <div className="fleet-layout">
            <section className="fleet-agents">
              <header>
                <h2>Agent activity</h2>
                <Search
                  value={search}
                  onChange={setSearch}
                  placeholder="Find an Agent"
                />
              </header>
              {shown.length ? (
                <div className="table-wrap">
                  <table>
                    <thead>
                      <tr>
                        <th>Agent</th>
                        <th>Connection</th>
                        <th>Communication</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {shown.map((a) => {
                        const waiting = pending.filter(
                          (p) => p.request.requester_id === a.agent.id,
                        );
                        const waitingNames = [
                          ...new Set(
                            waiting.flatMap((p) => p.peers.map((x) => x.name)),
                          ),
                        ];
                        return (
                          <tr key={a.agent.id}>
                            <td>
                              <div className="fleet-identity">
                                <IdentityIcon kind="agent" id={a.agent.id} />
                                <div>
                                  <strong>{a.agent.name}</strong>
                                  <small>
                                    {a.instances.length} connection
                                    {a.instances.length === 1 ? "" : "s"}
                                  </small>
                                </div>
                              </div>
                            </td>
                            <td>
                              <Status
                                value={a.fresh ? "connected" : "offline"}
                              />
                            </td>
                            <td>
                              {cycles.has(a.agent.id) ? (
                                <a href="#blocking-dependencies">
                                  <Badge tone="warn">Deadlock</Badge>
                                </a>
                              ) : waiting.length ? (
                                <span className="fleet-wait">
                                  Waiting on{" "}
                                  {waitingNames.join(", ") || "a reply"}
                                </span>
                              ) : (
                                <span className="muted">
                                  No pending replies
                                </span>
                              )}
                            </td>
                            <td>
                              <Button
                                primary
                                onClick={() =>
                                  navigate("/agents/" + a.agent.id)
                                }
                              >
                                View Agent
                              </Button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="fleet-empty">
                  <Icon name="agents" size={28} />
                  <h3>
                    {search ? "No Agents match" : "Your fleet starts here"}
                  </h3>
                  {!search ? (
                    <Button
                      primary
                      icon="plus"
                      onClick={() => navigate("/agents")}
                    >
                      Connect an Agent
                    </Button>
                  ) : null}
                </div>
              )}
            </section>
            <section className="fleet-waiting">
              <header>
                <h2>Waiting on a response</h2>
                <Icon name="conversations" size={17} />
              </header>
              {pending.length ? (
                pending.map(({ request: r, peers }) => (
                  <button
                    className="fleet-request"
                    key={r.id}
                    onClick={() =>
                      navigate("/conversations/" + r.conversation_id)
                    }
                  >
                    <div>
                      <IdentityIcon
                        kind={names.get(r.requester_id)?.kind || "agent"}
                        id={r.requester_id}
                      />
                      <strong>
                        {names.get(r.requester_id)?.name || "Workspace contact"}
                      </strong>
                      <Icon name="arrow" size={13} />
                      <span>
                        {peers.map((p) => p.name).join(", ") || "Recipient"}
                      </span>
                    </div>
                    <p>{r.body || r.title}</p>
                    <RequestStatus request={r} />
                  </button>
                ))
              ) : (
                <div className="fleet-empty">
                  <Icon name="check" size={23} />
                  <p>No pending responses</p>
                </div>
              )}
              {requests.hasNextPage ? (
                <Button onClick={() => void requests.fetchNextPage()}>
                  Load more requests
                </Button>
              ) : null}
            </section>
          </div>
          {conversations.hasNextPage ? (
            <Button onClick={() => void conversations.fetchNextPage()}>
              Load more communication history
            </Button>
          ) : null}
        </>
      )}
    </>
  );
}
