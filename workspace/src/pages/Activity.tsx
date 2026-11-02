import { useDeferredValue, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
} from "../data";
import { navigate } from "../navigation";
import {
  Badge,
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Modal,
  PageHeader,
  QueryState,
  Search,
  Status,
  Tabs,
} from "../ui";

type Event = {
  sequence: number;
  kind: string;
  actor_id: string;
  recipient_id: string;
  conversation_id: string;
  created_at: string;
  data: unknown;
};
type Delivery = {
  id: string;
  recipient_id: string;
  operation_id: string;
  state: string;
  connection_id: string;
  attempts: number;
  expires_at: string;
  last_error: string;
  attempt_history?: { items: unknown[]; next_cursor?: string };
};
export default function Activity() {
  const [tab, setTab] = useState("events"),
    [search, setSearch] = useState(""),
    [state, setState] = useState(""),
    [operation, setOperation] = useState(""),
    [selected, setSelected] = useState<Delivery | null>(null);
  const events = useResource<Event[]>("/activity", tab === "events"),
    filters: Record<string, string> = {};
  if (state) filters.state = state;
  if (operation)
    filters[tab === "deliveries" ? "operation_id" : "request_id"] = operation;
  const records = useCollection<Delivery & Record<string, unknown>>(
    tab === "executions" ? "executions" : "deliveries",
    useDeferredValue(search),
    filters,
    tab !== "events",
  );
  return (
    <>
      <PageHeader
        title="Activity log"
      />
      <Tabs
        value={tab}
        onChange={(v) => {
          setTab(v);
          setState("");
          setOperation("");
        }}
        options={[
          { id: "events", label: "Recent events" },
          { id: "deliveries", label: "Deliveries" },
          { id: "executions", label: "Execution reports" },
        ]}
      />
      <div className="toolbar">
        {tab !== "events" ? (
          <div className="filters">
            <select
              aria-label="Lifecycle state"
              value={state}
              onChange={(e) => setState(e.target.value)}
            >
              <option value="">All states</option>
              {(tab === "executions"
                ? ["authorized", "running", "succeeded", "failed", "unknown"]
                : [
                    "queued",
                    "leased",
                    "waiting",
                    "received",
                    "provider_accepted",
                    "failed",
                    "unknown",
                    "cancelled",
                  ]
              ).map((s) => (
                <option key={s}>{s}</option>
              ))}
            </select>
            <input
              aria-label="Operation or request ID"
              placeholder={
                tab === "deliveries" ? "Exact operation ID" : "Exact request ID"
              }
              value={operation}
              onChange={(e) => setOperation(e.target.value)}
              style={{ width: 200, fontSize: 11, minHeight: 32 }}
            />
          </div>
        ) : null}
        <Search
          value={search}
          onChange={setSearch}
          placeholder={
            tab === "events"
              ? "Filter loaded recent events"
              : "Search permitted evidence"
          }
        />
      </div>
      {tab === "events" ? (
        <>
          <ErrorView error={events.error} />
          <p className="collection-footer">
            Recent authorized events, up to the engine's current bounded window.
            Filtering applies to loaded events.
          </p>
          <div className="timeline">
            {events.data
              ?.filter((e) =>
                JSON.stringify(e).toLowerCase().includes(search.toLowerCase()),
              )
              .map((e) => (
                <div className="timeline-row" key={e.sequence}>
                  <div className="actions">
                    <strong>
                      {e.kind.replaceAll(".", " ").replaceAll("_", " ")}
                    </strong>
                    <small>#{e.sequence}</small>
                  </div>
                  <p>
                    <DateValue value={e.created_at} /> · {e.actor_id}
                  </p>
                  {e.conversation_id ? (
                    <Button
                      onClick={() =>
                        navigate("/conversations/" + e.conversation_id)
                      }
                    >
                      Conversation
                    </Button>
                  ) : null}
                  <Details value={e.data} label="Exact event detail" />
                </div>
              ))}
          </div>
        </>
      ) : (
        <QueryState query={records} empty="No permitted evidence matches">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Reference</th>
                  <th>State</th>
                  <th>
                    {tab === "deliveries" ? "Route / recipient" : "Executor"}
                  </th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {records.items.map((r) => (
                  <tr key={r.id}>
                    <td>
                      <code>{r.id}</code>
                      <p>
                        <small>
                          {String(r.operation_id || r.request_id || "")}
                        </small>
                      </p>
                    </td>
                    <td>
                      <Status value={r.state} />
                    </td>
                    <td>
                      {String(
                        tab === "deliveries" ? r.connection_id : r.executor_id,
                      )}
                      <p>
                        <small>{r.recipient_id}</small>
                      </p>
                    </td>
                    <td>
                      {tab === "deliveries" ? (
                        <Button onClick={() => setSelected(r)}>Inspect</Button>
                      ) : (
                        <Details
                          value={r}
                          label="Authenticated runtime report"
                        />
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </QueryState>
      )}
      {selected ? (
        <DeliveryDetail delivery={selected} onClose={() => setSelected(null)} />
      ) : null}
    </>
  );
}
function DeliveryDetail({
  delivery: d,
  onClose,
}: {
  delivery: Delivery;
  onClose: () => void;
}) {
  const [confirm, setConfirm] = useState(false),
    [risk, setRisk] = useState(false),
    op = useOperation(),
    cap = useCapabilities({ kind: "delivery", id: d.id }),
    attempts = useCollection<Record<string, unknown>>("delivery_attempts", "", {
      delivery_id: d.id,
    });
  return (
    <Modal title="Delivery evidence" wide onClose={onClose}>
      <div className="actions">
        <Status value={d.state} />
        <Badge>{d.attempts} attempts</Badge>
      </div>
      <dl className="detail-list">
        <dt>Delivery</dt>
        <dd>
          <code>{d.id}</code>
        </dd>
        <dt>Operation</dt>
        <dd>
          <code>{d.operation_id}</code>
        </dd>
        <dt>Recipient</dt>
        <dd>
          <code>{d.recipient_id}</code>
        </dd>
        <dt>Connection</dt>
        <dd>{d.connection_id}</dd>
        <dt>Expiry</dt>
        <dd>
          <DateValue value={d.expires_at} />
        </dd>
        <dt>Last safe error</dt>
        <dd>{d.last_error || "None recorded"}</dd>
      </dl>
      <p className="help">
        Provider acceptance is not a read receipt. Runtime acknowledgement is
        not execution success. Only an authenticated executor reports its
        external effect.
      </p>
      <h3>Attempt history</h3>
      <QueryState query={attempts} empty="No readable attempts">
        {attempts.items.map((a, i) => (
          <div className="timeline-row" key={i}>
            <strong>{String(a.outcome)}</strong>
            <p>{String(a.detail || "")}</p>
            <small>
              <DateValue value={String(a.created_at)} />
            </small>
            <Details value={a} />
          </div>
        ))}
      </QueryState>
      {cap.data?.operations.retry &&
      ["failed", "unknown"].includes(d.state) &&
      Date.parse(d.expires_at) > Date.now() ? (
        <>
          <Button danger onClick={() => setConfirm(true)}>
            Review delivery retry
          </Button>
          {confirm ? (
            <section className="approval">
              <h3>Retry transport delivery</h3>
              <p>
                This requeues existing accepted work. It does not approve again
                or authorize another execution.
              </p>
              {d.state === "unknown" ? (
                <label className="inline-check">
                  <input
                    type="checkbox"
                    checked={risk}
                    onChange={(e) => setRisk(e.target.checked)}
                  />
                  The provider may already have delivered this. I understand a
                  duplicate is possible.
                </label>
              ) : null}
              <ErrorView error={op.error} />
              <div className="form-actions">
                <Button onClick={() => setConfirm(false)}>Cancel</Button>
                <Button
                  primary
                  disabled={op.pending || (d.state === "unknown" && !risk)}
                  onClick={() =>
                    void op
                      .run("/deliveries/" + d.id + "/retry", {})
                      .then((v) => {
                        if (v) onClose();
                      })
                  }
                >
                  Retry with current policy checks
                </Button>
              </div>
            </section>
          ) : null}
        </>
      ) : null}
    </Modal>
  );
}
