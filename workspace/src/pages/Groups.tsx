import { useDeferredValue, useState } from "react";
import { useCapabilities, useCollection, useOperation } from "../data";
import type { Group, Identity, Target } from "../contracts";
import { TargetPicker } from "../TargetPicker";
import { navigate } from "../navigation";
import {
  Badge,
  Button,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Modal,
  PageHeader,
  QueryState,
  Search,
} from "../ui";
export default function Groups({
  purpose = "team",
  embedded = false,
}: {
  purpose?: "team" | "agent_role" | "contacts";
  embedded?: boolean;
  section?: string;
}) {
  const [search, setSearch] = useState(""),
    [selected, setSelected] = useState<Group | null>(null),
    [create, setCreate] = useState(false);
  const q = useCollection<Group>("groups", useDeferredValue(search), {
      purpose,
    }),
    caps = useCapabilities();
  const label =
    purpose === "agent_role"
      ? "Agent roles"
      : purpose === "team"
        ? "Teams"
        : "Contact groups";
  return (
    <>
      {!embedded ? <PageHeader title={label} /> : null}
      <div className="toolbar">
        <Search
          value={search}
          onChange={setSearch}
          placeholder={`Search ${label.toLowerCase()}`}
        />
        {caps.data?.operations.create_group ? (
          <Button primary icon="plus" onClick={() => setCreate(true)}>
            Create{" "}
            {purpose === "agent_role"
              ? "Agent role"
              : purpose === "team"
                ? "team"
                : "contact group"}
          </Button>
        ) : null}
      </div>
      <QueryState query={q} empty={`No ${label.toLowerCase()} yet`}>
        <div className="contact-directory">
          {q.items.map((g) => (
            <button
              className="contact-row"
              key={g.id}
              onClick={() => setSelected(g)}
            >
              <IdentityIcon kind="group" id={g.id} label={g.name} />
              <div className="contact-name">
                <strong>{g.name}</strong>
                <small>
                  {g.members.length}
                  {g.members_complete ? "" : " visible"}{" "}
                  {purpose === "agent_role" ? "Agents" : "members"}
                </small>
              </div>
              <Badge>
                {purpose === "agent_role"
                  ? "Agent role"
                  : purpose === "team"
                    ? "Team"
                    : "Contact group"}
              </Badge>
            </button>
          ))}
        </div>
      </QueryState>
      {selected ? (
        <GroupDetail group={selected} onClose={() => setSelected(null)} />
      ) : null}
      {create ? (
        <GroupForm purpose={purpose} onClose={() => setCreate(false)} />
      ) : null}
    </>
  );
}
function GroupDetail({
  group: g,
  onClose,
}: {
  group: Group;
  onClose: () => void;
}) {
  const [edit, setEdit] = useState(false);
  const caps = useCapabilities({ kind: "group", id: g.id }),
    people = useCollection<Identity>(
      g.purpose === "agent_role" ? "agents" : "people",
      "",
      { group_id: g.id },
    ),
    agents = useCollection<Identity>(
      "agents",
      "",
      { group_id: g.id },
      g.purpose === "contacts",
    ),
    services = useCollection<Identity>(
      "services",
      "",
      { group_id: g.id },
      g.purpose === "contacts",
    );
  return (
    <Modal title={g.name} onClose={onClose}>
      <div className="actions">
        {caps.data?.operations.manage ? (
          <Button onClick={() => setEdit(true)}>
            Edit{" "}
            {g.purpose === "agent_role"
              ? "Agent role"
              : g.purpose === "team"
                ? "team"
                : "contact group"}
          </Button>
        ) : null}
        <Button
          onClick={() => {
            onClose();
            navigate("/permissions/" + g.id);
          }}
        >
          Permissions
        </Button>
      </div>
      <section className="section">
        <h3>Members</h3>
        {[people, ...(g.purpose === "contacts" ? [agents, services] : [])].map(
          (q, i) => (
            <QueryState key={i} query={q} empty="">
              {q.items.map((p) => (
                <div className="team-member" key={p.id}>
                  <IdentityIcon kind={p.kind} id={p.id} label={p.name} />
                  <strong>{p.name}</strong>
                </div>
              ))}
            </QueryState>
          ),
        )}
        {!g.members_complete ? (
          <p className="help">Only members you can see are shown.</p>
        ) : null}
      </section>
      {edit ? (
        <GroupForm
          group={g}
          purpose={g.purpose || "contacts"}
          onClose={() => setEdit(false)}
          onSaved={onClose}
        />
      ) : null}
    </Modal>
  );
}
export function GroupForm({
  group: g,
  purpose,
  onClose,
  onSaved,
}: {
  group?: Group;
  purpose: "team" | "agent_role" | "contacts";
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [name, setName] = useState(g?.name || ""),
    [members, setMembers] = useState<Target[]>(
      (g?.members || []).map((id) => ({
        kind:
          purpose === "agent_role" ||
          (purpose === "contacts" && id.startsWith("agt_"))
            ? "agent"
            : purpose === "contacts" && id.startsWith("svc_")
              ? "service"
              : "person",
        id,
      })),
    );
  const op = useOperation();
  return (
    <Modal
      title={`${g ? "Edit" : "Create"} ${purpose === "agent_role" ? "Agent role" : purpose === "team" ? "team" : "contact group"}`}
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          void op
            .run("/groups", {
              ...(g ? { id: g.id, version: g.version } : {}),
              name,
              purpose,
              ...(g && !g.members_complete
                ? {}
                : { members: members.map((t) => t.id) }),
            })
            .then((r) => {
              if (r) {
                onClose();
                onSaved?.();
              }
            })
        }
      >
        <Field label="Name">
          <input
            required
            maxLength={100}
            placeholder={purpose === "agent_role" ? "Builders" : "Engineering"}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        {!g || g.members_complete ? (
          <div className="field">
            <span>Members</span>
            <TargetPicker
              value={members}
              onChange={setMembers}
              multiple
              allowGroups={false}
              allowInstances={false}
              peopleOnly={purpose === "team"}
              agentsOnly={purpose === "agent_role"}
            />
          </div>
        ) : (
          <p>
            Your full roster is not visible. You can safely change the name
            here.
          </p>
        )}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={op.pending}>
            Save{" "}
            {purpose === "agent_role"
              ? "Agent role"
              : purpose === "team"
                ? "team"
                : "group"}
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
