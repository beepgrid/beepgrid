import { useState } from "react";
import {
  useCollection,
  useCapabilities,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type {
  Access,
  Binding,
  Group,
  Identity,
  Role,
  Rule,
  Target,
} from "../contracts";
import { useRoute } from "../navigation";
import { TargetPicker } from "../TargetPicker";
import {
  Badge,
  Button,
  ErrorView,
  Icon,
  IdentityIcon,
  Modal,
  PageHeader,
} from "../ui";
import Advanced from "./PermissionAdvanced";
export { RoleForm } from "./PermissionAdvanced";

const actions = [
  ["view", "See"],
  ["message", "Message"],
  ["ask", "Request a reply"],
  ["decide", "Approve / decline"],
] as const;
export default function Permissions({
  profile,
  embedded = false,
}: { profile?: Identity; embedded?: boolean; section?: string } = {}) {
  const me = useSession(),
    route = useRoute();
  const people = useCollection<Identity>("people"),
    agents = useCollection<Identity>("agents"),
    services = useCollection<Identity>("services"),
    groups = useCollection<Group>("groups");
  const entities = [
    ...people.items,
    ...agents.items,
    ...services.items,
    ...groups.items.map((g) => ({ ...g, kind: "group" as const })),
  ];
  const [subject, setSubject] = useState<Target | null>(null),
    [choosing, setChoosing] = useState(false),
    [edit, setEdit] = useState(false),
    [advanced, setAdvanced] = useState(false);
  const subjectID = route.page === "permissions" ? route.id : "";
  const selected = profile ||
    subject || {
      kind: entities.find((e) => e.id === subjectID)?.kind || "person",
      id: subjectID || me.person.id,
    };
  const identity =
    profile ||
    entities.find((e) => e.id === selected.id) ||
    (selected.id === me.person.id ? me.person : null);
  const access = useResource<Access>(
    "/access?subject_id=" + encodeURIComponent(selected.id),
    selected.kind !== "group",
  );
  const roles = useCollection<Role>("roles"),
    bindings = useResource<Binding[]>("/role-bindings");
  const direct =
    bindings.data
      ?.filter((b) => b.subject_id === selected.id)
      .map((b) => roles.items.find((r) => r.id === b.role_id))
      .filter((r): r is Role => !!r) || [];
  const entries =
    selected.kind === "group"
      ? direct
      : access.data?.roles.map((r) => r.role) || [];
  const policy = direct.find((r) => r.id.startsWith("rol_contacts_"));
  const selectedCaps = useCapabilities(selected),
    globalCaps = useCapabilities();
  const canManage =
    me.admin ||
    !!(
      selectedCaps.data?.operations.manage &&
      globalCaps.data?.operations.create_role
    );
  if (advanced)
    return (
      <>
        <Button onClick={() => setAdvanced(false)}>
          Back to contact permissions
        </Button>
        <Advanced />
      </>
    );
  return (
    <>
      {!embedded ? (
        <PageHeader
          title="Contact permissions"
          actions={
            <>
              {canManage ? (
                <Button onClick={() => setChoosing(true)}>
                  Choose a person, agent or team
                </Button>
              ) : null}
            </>
          }
        />
      ) : null}
      <section className="contact-access-head">
        <IdentityIcon
          kind={selected.kind}
          id={selected.id}
          label={identity?.name}
        />
        <div>
          <h2>{identity?.name || "Selected contact"}</h2>
          <p>
            {selected.kind === "agent"
              ? "Agent"
              : selected.kind === "group"
                ? "Team or agent role"
                : "Person"}
          </p>
        </div>
        {canManage ? (
          <Button primary onClick={() => setEdit(true)}>
            Edit contact access
          </Button>
        ) : null}
      </section>
      <ErrorView
        error={
          (selected.kind !== "group" ? access.error : null) ||
          roles.error ||
          bindings.error
        }
      />
      {access.data?.basis === "administrator_credential" ? (
        <div className="access-notice">
          <Icon name="permissions" />
          <span>This account is a workspace administrator.</span>
        </div>
      ) : null}
      <div className="permission-policies">
        {entries.map((role, i) => (
          <section className="permission-policy" key={role.id + i}>
            <header>
              <h3>
                {role.id.startsWith("rol_contacts_")
                  ? "Contact access"
                  : role.name}
              </h3>
              <Badge>
                {selected.kind === "group" ? "All members" : "Access policy"}
              </Badge>
            </header>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Contact</th>
                    {actions.map(([a, label]) => (
                      <th key={a}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {[
                    ...new Set(
                      role.rules.map((r) => r.target_kind + ":" + r.target_id),
                    ),
                  ].map((key) => {
                    const [kind, id] = key.split(":");
                    const entity = entities.find((e) => e.id === id);
                    return (
                      <tr key={key}>
                        <td>
                          <div className="identity">
                            <IdentityIcon
                              kind={kind}
                              id={id}
                              label={entity?.name}
                            />
                            <strong>
                              {id === "*"
                                ? `All ${kind === "*" ? "contacts" : kind === "person" ? "people" : kind + "s"}`
                                : id === "$self"
                                  ? "Self"
                                  : entity?.name || "Restricted contact"}
                            </strong>
                          </div>
                        </td>
                        {actions.map(([a, label]) => (
                          <td key={a} aria-label={label}>
                            <PermissionGrant
                              rules={role.rules.filter(
                                (r) =>
                                  r.target_kind === kind &&
                                  r.target_id === id &&
                                  (r.action === a || r.action === "*"),
                              )}
                            />
                          </td>
                        ))}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>
        ))}
      </div>
      {!entries.length &&
      !access.isPending &&
      access.data?.basis !== "administrator_credential" ? (
        <p className="empty-policy">No contact access has been assigned yet.</p>
      ) : null}
      {canManage ? (
        <details className="advanced-access">
          <summary>Advanced administration</summary>
          <Button onClick={() => setAdvanced(true)}>
            Permission policies and diagnostics
          </Button>
        </details>
      ) : null}
      {choosing ? (
        <Modal title="Whose access?" onClose={() => setChoosing(false)}>
          <TargetPicker
            value={[selected]}
            onChange={(v) => {
              if (v[0]) {
                setSubject(v[0]);
                setChoosing(false);
              }
            }}
            allowInstances={false}
          />
        </Modal>
      ) : null}
      {edit ? (
        <ContactEditor
          key={selected.id}
          subject={selected}
          name={identity?.name || "Contact"}
          policy={policy}
          entities={entities}
          onClose={() => setEdit(false)}
        />
      ) : null}
    </>
  );
}
function PermissionGrant({ rules }: { rules: Rule[] }) {
  if (!rules.length) return <span className="permission-empty">·</span>;
  if (rules.some((r) => !r.connection_id && !r.action_name && !r.conditions))
    return <Icon name="check" size={17} />;
  return (
    <span title="Restricted by connection, action or policy conditions">
      Limited
    </span>
  );
}
function ContactEditor({
  subject,
  name,
  policy,
  entities,
  onClose,
}: {
  subject: Target;
  name: string;
  policy?: Role;
  entities: { id: string; name: string; kind: string }[];
  onClose: () => void;
}) {
  const [rules, setRules] = useState<Rule[]>(policy?.rules || []),
    [adding, setAdding] = useState(false);
  const [contacts, setContacts] = useState<Target[]>(() => [
    ...new Map(
      (policy?.rules || []).map((r) => [
        r.target_kind + r.target_id,
        { kind: r.target_kind as Target["kind"], id: r.target_id },
      ]),
    ).values(),
  ]);
  const op = useOperation();
  function toggle(target: Target, action: string) {
    setRules((old) =>
      old.some(
        (r) =>
          r.target_kind === target.kind &&
          r.target_id === target.id &&
          r.action === action,
      )
        ? old.filter(
            (r) =>
              !(
                r.target_kind === target.kind &&
                r.target_id === target.id &&
                r.action === action
              ),
          )
        : [...old, { target_kind: target.kind, target_id: target.id, action }],
    );
  }
  return (
    <Modal title={`Contact access for ${name}`} wide onClose={onClose}>
      <p className="help">
        Allow these actions toward each contact. Other access policies can grant
        additional permissions.
      </p>
      <div className="table-wrap">
        <table className="permission-matrix">
          <thead>
            <tr>
              <th>Contact</th>
              {actions.map(([a, label]) => (
                <th key={a}>{label}</th>
              ))}
              <th />
            </tr>
          </thead>
          <tbody>
            {contacts.map((t) => (
              <tr key={t.kind + t.id}>
                <td>
                  <div className="identity">
                    <IdentityIcon
                      kind={t.kind}
                      id={t.id}
                      label={entities.find((e) => e.id === t.id)?.name}
                    />
                    <strong>
                      {entities.find((e) => e.id === t.id)?.name ||
                        "Selected contact"}
                    </strong>
                  </div>
                </td>
                {actions.map(([a, label]) => (
                  <td key={a}>
                    <input
                      type="checkbox"
                      aria-label={`${label} ${entities.find((e) => e.id === t.id)?.name || "contact"}`}
                      checked={rules.some(
                        (r) =>
                          r.target_kind === t.kind &&
                          r.target_id === t.id &&
                          r.action === a,
                      )}
                      onChange={() => toggle(t, a)}
                    />
                  </td>
                ))}
                <td>
                  <Button
                    onClick={() => {
                      setContacts((old) => old.filter((x) => x.id !== t.id));
                      setRules((old) =>
                        old.filter((r) => r.target_id !== t.id),
                      );
                    }}
                  >
                    Remove
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Button icon="plus" onClick={() => setAdding((x) => !x)}>
        Add contacts
      </Button>
      {adding ? (
        <TargetPicker
          value={contacts}
          multiple
          allowGroups
          allowInstances={false}
          onChange={(v) => {
            setContacts(v);
            setRules((old) =>
              old.filter((r) => v.some((t) => t.id === r.target_id)),
            );
          }}
        />
      ) : null}
      <ErrorView error={op.error} />
      <div className="form-actions">
        <Button onClick={onClose}>Cancel</Button>
        <Button
          primary
          disabled={op.pending}
          onClick={() =>
            void op
              .run("/contact-permissions", {
                subject,
                rules,
                version: policy?.version || 0,
              })
              .then((r) => {
                if (r) onClose();
              })
          }
        >
          Save permissions
        </Button>
      </div>
    </Modal>
  );
}
