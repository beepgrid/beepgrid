import { useDeferredValue, useState } from "react";
import { useCapabilities, useCollection, useOperation } from "../data";
import type { Group, Target } from "../contracts";
import { Compose } from "../Compose";
import { TargetPicker } from "../TargetPicker";
import {
  Badge,
  Button,
  Details,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Modal,
  PageHeader,
  QueryState,
  Search,
} from "../ui";

export default function Groups() {
  const [search, setSearch] = useState(""),
    [selected, setSelected] = useState<Group | null>(null),
    [create, setCreate] = useState(false),
    [compose, setCompose] = useState<Target[] | null>(null);
  const q = useCollection<Group>("groups", useDeferredValue(search)),
    caps = useCapabilities();
  return (
    <>
      <PageHeader
        title="Groups"
        description="Membership, audiences and delegated teams."
        actions={
          caps.data?.operations.create_group ? (
            <Button primary icon="plus" onClick={() => setCreate(true)}>
              Create group
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <Search
          value={search}
          onChange={setSearch}
          placeholder="Search groups"
        />
      </div>
      <QueryState query={q} empty="No permitted groups">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Group</th>
                <th>Your membership</th>
                <th>Visible members</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {q.items.map((g) => (
                <tr key={g.id}>
                  <td>
                    <div className="identity">
                      <IdentityIcon kind="group" />
                      <div>
                        <strong>{g.name}</strong>
                        <small>{g.id}</small>
                      </div>
                    </div>
                  </td>
                  <td>
                    {g.member ? (
                      <Badge>Member</Badge>
                    ) : (
                      <span className="muted">Outside group</span>
                    )}
                  </td>
                  <td>
                    {g.members.length}
                    {!g.members_complete ? " visible (partial)" : ""}
                  </td>
                  <td>
                    <Button onClick={() => setSelected(g)}>Open</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </QueryState>
      {selected ? (
        <GroupDetail
          group={q.items.find((g) => g.id === selected.id) || selected}
          onClose={() => setSelected(null)}
          onCompose={() => {
            setCompose([{ kind: "group", id: selected.id }]);
            setSelected(null);
          }}
        />
      ) : null}
      {create ? <GroupForm onClose={() => setCreate(false)} /> : null}
      {compose ? (
        <Compose initial={compose} onClose={() => setCompose(null)} />
      ) : null}
    </>
  );
}
function GroupDetail({
  group: g,
  onClose,
  onCompose,
}: {
  group: Group;
  onClose: () => void;
  onCompose: () => void;
}) {
  const caps = useCapabilities({ kind: "group", id: g.id });
  const [edit, setEdit] = useState(false);
  if (caps.isPending)
    return (
      <Modal title="Group" onClose={onClose}>
        <p>Checking current access.</p>
      </Modal>
    );
  if (caps.error)
    return (
      <Modal title="Group unavailable" onClose={onClose}>
        <ErrorView error={caps.error} />
      </Modal>
    );
  return (
    <Modal title={g.name} wide onClose={onClose}>
      <div className="intro">
        <IdentityIcon kind="group" />
        <div>
          <h2>{g.name}</h2>
          <p>
            {g.id} · Revision {g.version}
          </p>
        </div>
      </div>
      <div className="actions">
        {caps.data?.operations.message || caps.data?.operations.ask ? (
          <Button primary onClick={onCompose}>
            Communicate
          </Button>
        ) : null}
        {caps.data?.operations.manage ? (
          <Button onClick={() => setEdit(true)}>Edit group</Button>
        ) : null}
      </div>
      <section className="section">
        <h3>Membership</h3>
        {!g.members_complete ? (
          <p className="help">
            Some member identities are outside your visibility. This is a
            partial roster. It cannot be replaced from this view.
          </p>
        ) : null}
        {g.members.map((id) => (
          <div className="pick-row" key={id}>
            <code>{id}</code>
          </div>
        ))}
      </section>
      <Details
        value={{
          member: g.member,
          created_by: g.created_by,
          version: g.version,
        }}
        label="Group context"
      />
      {edit ? (
        <GroupForm group={g} onClose={() => setEdit(false)} onSaved={onClose} />
      ) : null}
    </Modal>
  );
}
function GroupForm({
  group,
  onClose,
  onSaved,
}: {
  group?: Group;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [name, setName] = useState(group?.name || ""),
    [members, setMembers] = useState<Target[]>(
      (group?.members || []).map((id) => ({
        kind: id.startsWith("per_")
          ? "person"
          : id.startsWith("svc_")
            ? "service"
            : "agent",
        id,
      })),
    );
  const op = useOperation();
  return (
    <Modal title={group ? "Edit group" : "Create group"} wide onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run("/groups", {
              ...(group ? { id: group.id, version: group.version } : {}),
              name,
              ...(!group || group.members_complete
                ? { members: members.map((t) => t.id) }
                : { members: undefined }),
            })
            .then((r) => {
              if (r) {
                onClose();
                onSaved?.();
              }
            })
        }
      >
        <Field label="Group name">
          <input
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        {!group || group.members_complete ? (
          <>
            <h3>Explicit members</h3>
            <TargetPicker
              value={members}
              onChange={setMembers}
              multiple
              allowGroups={false}
            />
            <p className="help">
              Membership changes are checked against your delegated boundary and
              any roles assigned to this group.
            </p>
          </>
        ) : (
          <p className="help">
            The full roster is not visible. Membership replacement is disabled
            to protect hidden members.
          </p>
        )}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            type="submit"
            disabled={op.pending || (!!group && !group.members_complete)}
          >
            Save group
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
