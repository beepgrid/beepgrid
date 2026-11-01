import { useEffect, useRef, useState } from "react";
import {
  useCollection,
  useCapabilities,
  useOperation,
  usePostResource,
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
import { APIError } from "../client";
import { TargetPicker } from "../TargetPicker";
import {
  Button,
  ErrorView,
  Icon,
  IdentityIcon,
  Modal,
  PageHeader,
  Search,
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
  const selected: Target = (profile
    ? { kind: profile.kind, id: profile.id }
    : null) ||
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
  const selectedCaps = useCapabilities(selected),
    globalCaps = useCapabilities();
  const canManage =
    me.admin ||
    !!(
      selectedCaps.data?.operations.manage &&
      globalCaps.data?.operations.create_role
    );
  const effective = usePostResource<ContactPreview>(
    "/contact-permissions/preview",
    { subject: { kind: selected.kind, id: selected.id }, targets: [] },
    selected.kind === "group" ? canManage : !!access.data,
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
                  Choose a person, Agent or team
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
                ? "Team or Agent role"
                : "Person"}
          </p>
        </div>
        {canManage ? (
          <Button
            primary
            disabled={
              !effective.data ||
              !effective.data.can_save ||
              effective.isFetching
            }
            onClick={() => setEdit(true)}
          >
            Edit contact access
          </Button>
        ) : null}
      </section>
      <ErrorView
        error={
          (selected.kind !== "group" ? access.error : null) ||
          roles.error ||
          bindings.error ||
          effective.error
        }
      />
      {access.data?.basis === "administrator_credential" ? (
        <div className="access-notice">
          <Icon name="permissions" />
          <span>This account is a workspace administrator.</span>
        </div>
      ) : null}
      {effective.data?.contacts.length ? (
        <div className="table-wrap">
          <table className="permission-matrix contact-effective-matrix">
            <thead>
              <tr>
                <th>Contact</th>
                {actions.map(([a, label]) => (
                  <th key={a}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {effective.data.contacts.map((row) => (
                <tr key={row.target.kind + row.target.id}>
                  <td>
                    <div className="identity">
                      <IdentityIcon
                        kind={row.target.kind}
                        id={row.target.id}
                        label={row.name}
                      />
                      <strong>{row.name}</strong>
                    </div>
                  </td>
                  {actions.map(([action, label]) => {
                    const cell = row.permissions[action];
                    return (
                      <td key={action} aria-label={label}>
                        <div
                          className="contact-permission-cell"
                          title={cell.sources.join(", ")}
                        >
                          {cell.blocked ? (
                            <span className="contact-blocked">Blocked</span>
                          ) : cell.allowed ? (
                            <Icon name="check" size={17} />
                          ) : cell.limited ? (
                            <span>Limited</span>
                          ) : (
                            <span className="permission-empty">·</span>
                          )}
                          {cell.sources.length ? (
                            <small>{cell.sources.join(", ")}</small>
                          ) : null}
                        </div>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : !effective.isPending ? (
        <p className="empty-policy">
          No individual contact access has been assigned yet.
        </p>
      ) : null}
      {entries.some((role) =>
        role.rules.some(
          (r) =>
            r.target_id === "*" ||
            r.target_id === "$self" ||
            r.conditions ||
            r.action_name ||
            r.connection_id,
        ),
      ) ? (
        <details className="advanced-access contact-policy-details">
          <summary>Additional access policies</summary>
          {entries.map((role) => (
            <section className="permission-policy" key={role.id}>
              <header>
                <h3>
                  {role.id.startsWith("rol_contacts_")
                    ? "Contact access"
                    : role.name}
                </h3>
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
                        role.rules.map(
                          (r) => r.target_kind + ":" + r.target_id,
                        ),
                      ),
                    ].map((key) => {
                      const [kind, id] = key.split(":");
                      return (
                        <tr key={key}>
                          <td>
                            {id === "*"
                              ? `All ${kind === "*" ? "contacts" : kind === "person" ? "people" : kind + "s"}`
                              : id === "$self"
                                ? "Self"
                                : entities.find((e) => e.id === id)?.name ||
                                  "Restricted contact"}
                          </td>
                          {actions.map(([action, label]) => (
                            <td key={action} aria-label={label}>
                              <PermissionGrant
                                rules={role.rules.filter(
                                  (r) =>
                                    r.target_kind === kind &&
                                    r.target_id === id &&
                                    (r.action === action || r.action === "*"),
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
        </details>
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
      {edit && effective.data ? (
        <ContactEditor
          key={selected.id}
          subject={selected}
          name={identity?.name || "Contact"}
          preview={effective.data}
          onClose={() => setEdit(false)}
        />
      ) : null}
    </>
  );
}
function PermissionGrant({ rules }: { rules: Rule[] }) {
  if (rules.some((r) => r.effect === "deny"))
    return <span className="contact-blocked">Blocked</span>;
  if (!rules.length) return <span className="permission-empty">·</span>;
  if (rules.some((r) => !r.connection_id && !r.action_name && !r.conditions))
    return <Icon name="check" size={17} />;
  return (
    <span title="Restricted by connection, action or policy conditions">
      Limited
    </span>
  );
}
export type ContactCell = {
  allowed: boolean;
  blocked: boolean;
  limited: boolean;
  direct: "default" | "allow" | "deny";
  sources: string[];
};
export type ContactRow = {
  target: Target;
  name: string;
  permissions: Record<string, ContactCell>;
};
export type ContactPreview = {
  policy: Role;
  contacts: ContactRow[];
  warnings: string[];
  can_save: boolean;
};
function ContactEditor({
  subject,
  name,
  preview: initial,
  onClose,
}: {
  subject: Target;
  name: string;
  preview: ContactPreview;
  onClose: () => void;
}) {
  const [baseline, setBaseline] = useState(initial);
  const [rules, setRules] = useState<Rule[]>(baseline.policy.rules || []),
    [adding, setAdding] = useState(false),
    [review, setReview] = useState(false),
    [search, setSearch] = useState("");
  const [contacts, setContacts] = useState<Target[]>(() =>
    baseline.contacts.map((r) => r.target),
  );
  const current = usePostResource<ContactPreview>(
    "/contact-permissions/preview",
    {
      subject,
      rules,
      version: baseline.policy.version,
      targets: contacts,
    },
  );
  const last = useRef(initial);
  if (current.data) last.current = current.data;
  const preview = current.data || last.current;
  const op = useOperation();
  const error = current.error || op.error;
  const needsReload = error instanceof APIError && error.status === 409;
  async function reload() {
    const next = await op.run<ContactPreview>("/contact-permissions/preview", {
      subject,
      targets: [],
    });
    if (!next) return;
    setBaseline(next);
    setRules(next.policy.rules || []);
    setContacts(next.contacts.map((r) => r.target));
    setReview(false);
    last.current = next;
  }
  const dirty =
    JSON.stringify(rules) !== JSON.stringify(baseline.policy.rules || []);
  function setPermission(target: Target, action: string, allowed: boolean) {
    setReview(false);
    setRules((old) => {
      const affected =
        action === "view" && !allowed ? actions.map(([a]) => a) : [action];
      const next = old.filter(
        (r) =>
          !(
            r.target_kind === target.kind &&
            r.target_id === target.id &&
            affected.includes(r.action as (typeof actions)[number][0])
          ),
      );
      for (const action of affected)
        next.push({
          target_kind: target.kind,
          target_id: target.id,
          action,
          ...(!allowed ? { effect: "deny" as const } : {}),
        });
      if (allowed && action !== "view") {
        const noView = next.filter(
          (r) =>
            !(
              r.target_kind === target.kind &&
              r.target_id === target.id &&
              r.action === "view"
            ),
        );
        return [
          ...noView,
          { target_kind: target.kind, target_id: target.id, action: "view" },
        ];
      }
      return next;
    });
  }
  function defaults(t: Target) {
    setReview(false);
    setRules((old) =>
      old.filter((r) => !(r.target_kind === t.kind && r.target_id === t.id)),
    );
  }
  const changed = contacts.flatMap((target) => {
    const before = baseline.policy.rules.filter(
      (r) => r.target_kind === target.kind && r.target_id === target.id,
    );
    const after = rules.filter(
      (r) => r.target_kind === target.kind && r.target_id === target.id,
    );
    if (JSON.stringify(before) === JSON.stringify(after)) return [];
    const row = preview.contacts.find(
      (r) => r.target.id === target.id && r.target.kind === target.kind,
    );
    return [
      {
        name: row?.name || "Selected contact",
        changes: actions.flatMap(([action, label]) => {
          const prior = before.find((r) => r.action === action);
          const next = after.find((r) => r.action === action);
          if (JSON.stringify(prior) === JSON.stringify(next)) return [];
          return [
            `${label}: ${next?.effect === "deny" ? "blocked" : next ? "allowed directly" : "use defaults"}`,
          ];
        }),
      },
    ];
  });
  const reviewRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (review) reviewRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [review]);
  const blocks = rules.filter(
    (r) =>
      r.effect === "deny" &&
      !baseline.policy.rules.some(
        (old) =>
          old.action === r.action &&
          old.target_kind === r.target_kind &&
          old.target_id === r.target_id &&
          old.effect === "deny",
      ),
  );
  async function save() {
    if ((blocks.length || preview.warnings.length) && !review) {
      setReview(true);
      return;
    }
    const result = await op.run("/contact-permissions", {
      subject,
      rules,
      version: baseline.policy.version,
    });
    if (result) onClose();
  }
  return (
    <Modal
      title={`Contact access for ${name}`}
      wide
      className="contact-editor-dialog"
      onClose={onClose}
    >
      <div className="help">
        <p>Choose what {name} can do toward each contact.</p>
        <small>
          Role and team access applies by default. A block overrides it.
        </small>
      </div>
      {contacts.length > 8 ? (
        <Search
          value={search}
          onChange={setSearch}
          placeholder="Find a contact"
        />
      ) : null}
      {contacts.length ? (
        <div className="table-wrap">
          <table className="permission-matrix contact-effective-matrix">
            <thead>
              <tr>
                <th>Contact</th>
                {actions.map(([a, label]) => (
                  <th key={a}>{label}</th>
                ))}
                <th>
                  <span className="sr-only">Defaults</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {contacts
                .filter(
                  (t) =>
                    !search ||
                    (
                      preview.contacts.find((r) => r.target.id === t.id)
                        ?.name || ""
                    )
                      .toLowerCase()
                      .includes(search.toLowerCase()),
                )
                .map((t) => {
                  const row = preview.contacts.find(
                    (r) => r.target.id === t.id && r.target.kind === t.kind,
                  );
                  const name =
                    row?.name ||
                    baseline.contacts.find((r) => r.target.id === t.id)?.name ||
                    "Selected contact";
                  const explicit = rules.some(
                    (r) => r.target_kind === t.kind && r.target_id === t.id,
                  );
                  return (
                    <tr key={t.kind + t.id}>
                      <td>
                        <div className="identity">
                          <IdentityIcon kind={t.kind} id={t.id} label={name} />
                          <strong>{name}</strong>
                        </div>
                      </td>
                      {actions.map(([action, label]) => {
                        const cell = row?.permissions[action];
                        const direct = rules.find(
                          (r) =>
                            r.target_kind === t.kind &&
                            r.target_id === t.id &&
                            r.action === action,
                        );
                        const externalBlock =
                          cell?.blocked && cell.direct !== "deny";
                        const checked = direct
                          ? direct.effect !== "deny" && !externalBlock
                          : !!cell?.allowed;
                        const source =
                          direct?.effect === "deny"
                            ? "Blocked"
                            : externalBlock
                              ? "Blocked by policy"
                              : direct
                                ? ""
                                : cell?.limited && !cell.allowed
                                  ? "Limited"
                                  : cell?.sources.join(", ") || "";
                        return (
                          <td key={action}>
                            <label
                              className={`contact-permission-cell ${direct?.effect === "deny" || externalBlock ? "is-blocked" : ""}`}
                              title={cell?.sources.join(", ") || source}
                            >
                              <input
                                type="checkbox"
                                aria-label={`${label} ${name}`}
                                checked={checked}
                                disabled={!!externalBlock || op.pending}
                                onChange={() =>
                                  setPermission(t, action, !checked)
                                }
                              />
                              {source ? <small>{source}</small> : null}
                            </label>
                          </td>
                        );
                      })}
                      <td>
                        {explicit ? (
                          <Button
                            className="contact-defaults"
                            disabled={op.pending}
                            onClick={() => defaults(t)}
                          >
                            Use defaults
                          </Button>
                        ) : (
                          <span className="permission-empty">Default</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="empty-policy">Add a contact to assign access.</p>
      )}
      <div className="actions">
        <Button icon="plus" onClick={() => setAdding((x) => !x)}>
          {adding ? "Done adding" : "Add contacts"}
        </Button>
      </div>
      {adding ? (
        <TargetPicker
          value={[]}
          multiple
          allowGroups
          allowInstances={false}
          showSelection={false}
          excludeIds={[subject.id, ...contacts.map((t) => t.id)]}
          onChange={(value) => {
            setContacts((old) => [
              ...old,
              ...value.filter(
                (t) => !old.some((o) => o.id === t.id && o.kind === t.kind),
              ),
            ]);
            setReview(false);
          }}
        />
      ) : null}
      <p className="contact-editor-note">
        Requesting a reply includes the answer to that request. Approval
        authority is always separate.
      </p>
      {review ? (
        <div
          className="contact-change-review help"
          role="status"
          ref={reviewRef}
        >
          <strong>Review access changes</strong>
          {blocks.length ? (
            <p>
              {blocks.length}{" "}
              {blocks.length === 1 ? "permission is" : "permissions are"}{" "}
              explicitly blocked. These blocks remain when role or team
              membership changes.
            </p>
          ) : null}
          <ul>
            {changed.map((row) => (
              <li key={row.name}>
                <strong>{row.name}</strong>
                <span>{row.changes.join(" · ")}</span>
              </li>
            ))}
          </ul>
          {preview.warnings.map((warning) => (
            <p key={warning}>{warning}</p>
          ))}
        </div>
      ) : null}
      <ErrorView error={error} />
      {needsReload ? (
        <Button disabled={op.pending} onClick={() => void reload()}>
          Reload access
        </Button>
      ) : null}
      <div className="form-actions">
        <Button disabled={op.pending} onClick={onClose}>
          Cancel
        </Button>
        <Button
          primary
          disabled={
            !dirty ||
            needsReload ||
            current.isFetching ||
            !!current.error ||
            !current.data?.can_save ||
            op.pending
          }
          onClick={() => void save()}
        >
          {review ? "Confirm changes" : "Save permissions"}
        </Button>
      </div>
    </Modal>
  );
}
