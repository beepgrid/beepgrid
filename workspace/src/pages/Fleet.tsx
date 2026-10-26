import { useState } from "react";
import { usePostResource, useCollection } from "../data";
import type { FleetSource, Group } from "../contracts";
import {
  Badge,
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Loading,
  Status,
} from "../ui";
import { assistantName } from "../OverseerEntry";
import { navigate } from "../navigation";

export default function Fleet() {
  const [group, setGroup] = useState("");
  const groups = useCollection<Group>("groups"),
    status = usePostResource<{
      agents: FleetSource[];
      summary: string;
      as_of: string;
    }>("/fleet/status", group ? { target: { kind: "group", id: group } } : {});
  return (
    <div className="form">
      <Field label="Fleet scope">
        <select value={group} onChange={(e) => setGroup(e.target.value)}>
          <option value="">All permitted agents (up to 100)</option>
          {groups.items.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </Field>
      {groups.hasNextPage ? (
        <Button
          disabled={groups.isFetchingNextPage}
          onClick={() => void groups.fetchNextPage()}
        >
          Load more groups
        </Button>
      ) : null}
      <ErrorView error={status.error || groups.error} />
      {status.isPending ? <Loading label="Reading reported status" /> : null}
      {status.data ? (
        <>
          <p>{status.data.summary}</p>
          <small>
            Observed <DateValue value={status.data.as_of} />
          </small>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>Report freshness</th>
                  <th>Instances</th>
                </tr>
              </thead>
              <tbody>
                {status.data.agents.map((a) => (
                  <tr key={a.agent.id}>
                    <td>{a.agent.name}</td>
                    <td>
                      <Badge tone={a.fresh ? "good" : "neutral"}>
                        {a.fresh ? "Fresh connection" : "Stale or offline"}
                      </Badge>
                    </td>
                    <td>
                      {a.instances.map((i) => (
                        <div key={i.id}>
                          <Status value={i.presence} />
                          <Details
                            label={i.id}
                            value={{
                              last_contact: i.last_seen,
                              reported_progress: i.progress,
                              declared_capabilities: i.capabilities,
                            }}
                          />
                        </div>
                      ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : null}
      <p className="help">
        Fleet status reads agent reports directly. Open {assistantName}{" "}
        separately for AI-assisted interpretation and coordination.
      </p>
      <Button onClick={() => navigate("/conversations/overseer")}>
        Open {assistantName}
      </Button>
    </div>
  );
}
