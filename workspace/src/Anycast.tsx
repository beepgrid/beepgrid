import { useState } from "react";
import { useOperation, useResource } from "./data";
import { navigate } from "./navigation";
import { Badge, Button, ErrorView, Modal } from "./ui";

export type AnycastDispatch = {
  id: string;
  role_id: string;
  role_name: string;
  state:
    | "queued"
    | "assigned"
    | "accepted"
    | "completed"
    | "cancelled"
    | "expired"
    | "blocked";
  agent_id?: string;
  conversation_id?: string;
  request_id?: string;
  input: { body: string; title?: string };
  last_error?: string;
};
const labels = {
  queued: "Waiting for an agent",
  assigned: "Assigned",
  accepted: "Assigned",
  completed: "Finished",
  cancelled: "Cancelled",
  expired: "Expired",
  blocked: "Needs attention",
};

function DispatchRow({ dispatch }: { dispatch: AnycastDispatch }) {
  const op = useOperation("anycast:" + dispatch.id);
  return (
    <div className="anycast-row">
      <div>
        <strong>{dispatch.role_name}</strong>
        <p>{dispatch.input.title || dispatch.input.body}</p>
        {dispatch.last_error ? <small>{dispatch.last_error}</small> : null}
      </div>
      <Badge>{labels[dispatch.state]}</Badge>
      {dispatch.conversation_id ? (
        <Button
          onClick={() => navigate("/conversations/" + dispatch.conversation_id)}
        >
          Open conversation
        </Button>
      ) : dispatch.state === "queued" ? (
        <Button
          disabled={op.pending}
          onClick={() => void op.run("/anycast/" + dispatch.id + "/cancel", {})}
        >
          Cancel
        </Button>
      ) : null}
      <ErrorView error={op.error} />
    </div>
  );
}
export function AnycastReceipt({ id }: { id: string }) {
  const q = useResource<AnycastDispatch>("/anycast/" + id);
  return (
    <div className="anycast-receipt" role="status">
      <p>Follow this in Messages → Anyone available.</p>
      <ErrorView error={q.error} />
      {q.data ? <DispatchRow dispatch={q.data} /> : null}
    </div>
  );
}
export function AnycastQueue() {
  const q = useResource<AnycastDispatch[]>("/anycast");
  const [open, setOpen] = useState(false);
  if (!q.data?.length) return null;
  const waiting = q.data.filter((d) => d.state === "queued").length;
  return (
    <>
      <Button onClick={() => setOpen(true)}>
        Anyone available{waiting ? ` · ${waiting} waiting` : ""}
      </Button>
      {open ? (
        <Modal title="Anyone available" onClose={() => setOpen(false)}>
          <ErrorView error={q.error} />
          <div className="anycast-list">
            {q.data.map((d) => (
              <DispatchRow key={d.id} dispatch={d} />
            ))}
          </div>
        </Modal>
      ) : null}
    </>
  );
}
