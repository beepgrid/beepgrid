import { useEffect, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type {
  Access,
  Binding,
  Role,
  Rule,
  Target,
  Workspace,
} from "../contracts";
import { TargetPicker } from "../TargetPicker";
import { useRoute } from "../navigation";
import {
  Badge,
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Form,
  JsonField,
  Loading,
  Modal,
  PageHeader,
  QueryState,
  Tabs,
} from "../ui";

export default function Permissions() {
  const route = useRoute(),
    me = useSession();
  const [tab, setTab] = useState("access"),
    [role, setRole] = useState<Role | null>(null),
    [create, setCreate] = useState(false);
  const caps = useCapabilities(),
    roles = useCollection<Role>("roles", "", {}, tab === "roles"),
    subject = route.id || me.person.id,
    access = useResource<Access>(
      "/access?subject_id=" + encodeURIComponent(subject),
      tab === "access",
    );
  return (
    <>
      <PageHeader
        title="Permissions"
        description="Explicit, directional access with clear scope."
        actions={
          caps.data?.operations.create_role ? (
            <Button primary icon="plus" onClick={() => setCreate(true)}>
              Create role
            </Button>
          ) : null
        }
      />
      <Tabs
        value={tab}
        onChange={setTab}
        options={[
          { id: "access", label: route.id ? "Effective access" : "My access" },
          { id: "roles", label: "Roles" },
          { id: "assignments", label: "Assignments" },
          { id: "explain", label: "Check permission" },
          { id: "route", label: "Recipient preview" },
        ]}
      />
      {tab === "access" ? (
        <>
          <ErrorView error={access.error} />
          {access.isPending ? <Loading /> : null}
          {access.data ? (
            <>
              <section className="section">
                <h2>{access.data.subject.name}</h2>
                <p>
                  Observed <DateValue value={access.data.observed_at} />.{" "}
                  {access.data.basis === "administrator_credential"
                    ? "This session has administrator credential authority. Role grants below are not the complete source of that authority."
                    : "No matching role grant means access is denied. Receiving, replying, asking and deciding are independent."}
                </p>
              </section>
              {access.data.roles.length === 0 ? (
                <p className="help">
                  No effective role assignments are currently visible.
                </p>
              ) : (
                access.data.roles.map((entry, i) => (
                  <section className="section" key={entry.role.id + i}>
                    <h3>{entry.role.name}</h3>
                    <p>
                      <small>
                        {entry.subject_kind === "group"
                          ? "Inherited through group"
                          : "Direct assignment"}{" "}
                        · {entry.subject_id}
                      </small>
                    </p>
                    <RuleTable rules={entry.role.rules} />
                  </section>
                ))
              )}
            </>
          ) : null}
        </>
      ) : null}
      {tab === "roles" ? (
        <QueryState query={roles} empty="No permitted role definitions">
          <table>
            <thead>
              <tr>
                <th>Role</th>
                <th>Rules</th>
                <th>Revision</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {roles.items.map((r) => (
                <tr key={r.id}>
                  <td>
                    <strong>{r.name}</strong>
                    <p>
                      <small>{r.id}</small>
                    </p>
                  </td>
                  <td>{r.rules.length}</td>
                  <td>{r.version}</td>
                  <td>
                    <Button onClick={() => setRole(r)}>Inspect</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </QueryState>
      ) : null}
      {tab === "assignments" ? <Assignments /> : null}
      {tab === "explain" ? <PolicyExplain /> : null}
      {tab === "route" ? <RoutePreview /> : null}
      {role ? <RoleDetail role={role} onClose={() => setRole(null)} /> : null}
      {create ? <RoleForm onClose={() => setCreate(false)} /> : null}
    </>
  );
}
function RuleTable({ rules }: { rules: Rule[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Action</th>
            <th>Target</th>
            <th>Connection / action</th>
            <th>Conditions</th>
          </tr>
        </thead>
        <tbody>
          {rules.map((r, i) => (
            <tr key={i}>
              <td>
                <Badge>{r.action}</Badge>
              </td>
              <td>
                {r.target_kind} : <code>{r.target_id}</code>
              </td>
              <td>
                {r.connection_id || "Any connection"}
                <br />
                {r.action_name || "Any action name"}
              </td>
              <td>
                {r.conditions ? (
                  <Details value={r.conditions} label="Exact conditions" />
                ) : (
                  "None"
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
function RoleDetail({ role: r, onClose }: { role: Role; onClose: () => void }) {
  const caps = useCapabilities({ kind: "role", id: r.id }),
    [edit, setEdit] = useState(false);
  if (caps.error)
    return (
      <Modal title="Role unavailable" onClose={onClose}>
        <ErrorView error={caps.error} />
      </Modal>
    );
  return (
    <Modal title={r.name} wide onClose={onClose}>
      <RuleTable rules={r.rules} />
      <ErrorView error={caps.error} />
      {caps.data?.operations.manage ? (
        <Button primary onClick={() => setEdit(true)}>
          Edit role
        </Button>
      ) : null}
      {edit ? (
        <RoleForm role={r} onClose={() => setEdit(false)} onSaved={onClose} />
      ) : null}
    </Modal>
  );
}
const blank: Rule = { action: "view", target_kind: "agent", target_id: "" };
export function RoleForm({
  role,
  onClose,
  onSaved,
}: {
  role?: Role;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [name, setName] = useState(role?.name || ""),
    [rules, setRules] = useState<Rule[]>(role?.rules || [{ ...blank }]),
    [checks, setChecks] = useState("[]"),
    [preview, setPreview] = useState<{
      can_save: boolean;
      affected_assignments: Binding[];
      checks: unknown[];
      basis: string;
    } | null>(null),
    [error, setError] = useState<Error | null>(null);
  const op = useOperation(),
    workspace = useResource<Workspace>("/workspace");
  function update(index: number, patch: Partial<Rule>) {
    setRules((x) => x.map((r, i) => (i === index ? { ...r, ...patch } : r)));
    setPreview(null);
  }
  async function prepare() {
    setError(null);
    try {
      const parsed = JSON.parse(checks);
      const result = await op.run<typeof preview>("/roles/preview", {
        role: {
          ...(role ? { id: role.id, version: role.version } : {}),
          name,
          rules,
        },
        checks: parsed,
      });
      if (result) setPreview(result);
    } catch (e) {
      setError(e as Error);
    }
  }
  return (
    <Modal title={role ? "Edit role" : "Create role"} wide onClose={onClose}>
      <Form
        onSubmit={() =>
          void (preview
            ? op
                .run("/roles", {
                  ...(role ? { id: role.id, version: role.version } : {}),
                  name,
                  rules,
                })
                .then((r) => {
                  if (r) {
                    onClose();
                    onSaved?.();
                  }
                })
            : prepare())
        }
      >
        <Field label="Role name">
          <input
            required
            maxLength={100}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setPreview(null);
            }}
          />
        </Field>
        <p className="help">
          Rules only grant the exact operations shown. A wildcard (*) is
          organization-wide authority. Manage does not imply permission to read
          message content, send, ask or decide.
        </p>
        {rules.map((r, i) => (
          <section className="rule" key={i}>
            <div className="actions">
              <h3>Rule {i + 1}</h3>
              <Button
                danger
                onClick={() => {
                  setRules((x) => x.filter((_, j) => j !== i));
                  setPreview(null);
                }}
              >
                Remove
              </Button>
            </div>
            <div className="form-grid">
              <Field label="Action">
                <select
                  value={r.action}
                  onChange={(e) =>
                    update(i, {
                      action: e.target.value,
                      conditions: ["ask", "decide"].includes(e.target.value)
                        ? r.conditions
                        : undefined,
                    })
                  }
                >
                  {["view", "message", "ask", "decide", "manage", "*"].map(
                    (a) => (
                      <option key={a}>{a}</option>
                    ),
                  )}
                </select>
              </Field>
              <Field label="Target category">
                <select
                  value={r.target_kind}
                  onChange={(e) => update(i, { target_kind: e.target.value })}
                >
                  {Array.from(
                    new Set([
                      "person",
                      "agent",
                      "service",
                      "group",
                      "instance",
                      "*",
                      ...(r.action === "manage"
                        ? Object.keys(
                            workspace.data?.permission_model
                              .management_targets || {},
                          )
                        : ["access", "account"]),
                    ]),
                  ).map((t) => (
                    <option key={t}>{t}</option>
                  ))}
                </select>
              </Field>
              <Field label="Target ID">
                <input
                  required
                  placeholder="Exact ID, self, or *"
                  value={r.target_id}
                  onChange={(e) => update(i, { target_id: e.target.value })}
                />
              </Field>
            </div>
            <div className="form-grid">
              <Field label="Connection ID (optional)">
                <input
                  value={r.connection_id || ""}
                  onChange={(e) => update(i, { connection_id: e.target.value })}
                />
              </Field>
              <Field label="Action name (optional)">
                <input
                  value={r.action_name || ""}
                  onChange={(e) => update(i, { action_name: e.target.value })}
                />
              </Field>
            </div>
            {["ask", "decide"].includes(r.action) ? (
              <>
                <div className="form-grid">
                  {(["resource", "resource_version", "currency"] as const).map(
                    (key) => (
                      <Field key={key} label={key.replaceAll("_", " ")}>
                        <input
                          value={r.conditions?.[key] || ""}
                          onChange={(e) =>
                            update(i, {
                              conditions: {
                                ...r.conditions,
                                [key]: e.target.value || undefined,
                              },
                            })
                          }
                        />
                      </Field>
                    ),
                  )}
                </div>
                <div className="form-grid">
                  <Field label="Maximum amount (exact minor units)">
                    <input
                      type="number"
                      min={0}
                      max={1e12}
                      step={1}
                      value={r.conditions?.max_amount_minor ?? ""}
                      onChange={(e) =>
                        update(i, {
                          conditions: {
                            ...r.conditions,
                            max_amount_minor:
                              e.target.value === ""
                                ? undefined
                                : Number(e.target.value),
                          },
                        })
                      }
                    />
                  </Field>
                  <Field label="Before (optional)">
                    <input
                      type="datetime-local"
                      value={
                        r.conditions?.before
                          ? new Date(
                              Date.parse(r.conditions.before) -
                                new Date(
                                  r.conditions.before,
                                ).getTimezoneOffset() *
                                  60000,
                            )
                              .toISOString()
                              .slice(0, 16)
                          : ""
                      }
                      onChange={(e) =>
                        update(i, {
                          conditions: {
                            ...r.conditions,
                            before: e.target.value
                              ? new Date(e.target.value).toISOString()
                              : undefined,
                          },
                        })
                      }
                    />
                  </Field>
                </div>
                <small>
                  Currency is required with an amount. No rounding or inferred
                  exchange rate is applied.
                </small>
              </>
            ) : null}
          </section>
        ))}
        <Button
          disabled={rules.length >= 100}
          icon="plus"
          onClick={() => {
            setRules((x) => [...x, { ...blank }]);
            setPreview(null);
          }}
        >
          Add rule
        </Button>
        <details>
          <summary>Optional before/after permission checks</summary>
          <JsonField
            label="Explicit checks (JSON array)"
            value={checks}
            onChange={(v) => {
              setChecks(v);
              setPreview(null);
            }}
          />
          <small>
            Checks include subject_id, action, target, and optional
            connection_id, action_name and descriptor. Only authorized affected
            subjects may be inspected.
          </small>
        </details>
        <ErrorView error={error || op.error} />
        {preview ? (
          <section className="approval">
            <h3>Policy preview</h3>
            <p>
              {preview.affected_assignments.length} affected assignments. This
              preview uses role grants and does not simulate administrator
              credentials.
            </p>
            <Details
              value={preview.affected_assignments}
              label="Affected assignment scope"
            />
            <Details
              value={preview.checks}
              label="Before/after check results"
            />
          </section>
        ) : null}
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          {preview ? (
            <Button onClick={() => setPreview(null)}>Back to editing</Button>
          ) : null}
          <Button
            primary
            type="submit"
            disabled={op.pending || preview?.can_save === false}
          >
            {op.pending
              ? "Checking"
              : preview
                ? "Save reviewed role"
                : "Preview changes"}
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
function Assignments() {
  const roles = useCollection<Role>("roles"),
    bindings = useResource<Binding[]>("/role-bindings");
  const [targets, setTargets] = useState<Target[]>([]),
    [role, setRole] = useState("");
  const cap = useCapabilities(role ? { kind: "role", id: role } : undefined),
    op = useOperation();
  const selected = targets[0];
  return (
    <>
      <ErrorView error={roles.error || bindings.error || op.error} />
      <section className="section">
        <h2>Role assignments</h2>
        <p>
          Direct and group assignments are distinct. Current role assignment
          authority and subject-management bounds are checked on every change.
        </p>
        <Form
          onSubmit={() =>
            void op.run("/role-bindings", {
              role_id: role,
              subject_kind: selected?.kind === "group" ? "group" : "principal",
              subject_id: selected?.id,
            })
          }
        >
          <Field label="Role">
            <select
              required
              value={role}
              onChange={(e) => setRole(e.target.value)}
            >
              <option value="">Select a permitted role</option>
              {roles.items.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name}
                </option>
              ))}
            </select>
          </Field>
          {roles.hasNextPage ? (
            <Button onClick={() => void roles.fetchNextPage()}>
              Load more roles
            </Button>
          ) : null}
          <TargetPicker
            value={targets}
            onChange={setTargets}
            allowInstances={false}
          />
          <Button
            primary
            type="submit"
            disabled={
              op.pending ||
              !selected ||
              !cap.data?.operations.assign_to_managed_subject
            }
          >
            Assign selected role
          </Button>
        </Form>
      </section>
      {bindings.data ? (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Role</th>
                <th>Subject</th>
                <th>Source</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {bindings.data.map((b) => (
                <BindingRow
                  key={b.role_id + b.subject_id}
                  binding={b}
                  roles={roles.items}
                />
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </>
  );
}
function BindingRow({
  binding: b,
  roles,
}: {
  binding: Binding;
  roles: Role[];
}) {
  const caps = useCapabilities({ kind: "role", id: b.role_id }),
    op = useOperation();
  const [confirm, setConfirm] = useState(false);
  return (
    <tr>
      <td>{roles.find((r) => r.id === b.role_id)?.name || b.role_id}</td>
      <td>
        <code>{b.subject_id}</code>
      </td>
      <td>{b.subject_kind === "group" ? "Group membership" : "Direct"}</td>
      <td>
        {caps.data?.operations.assign_to_managed_subject ? (
          <>
            <Button
              danger
              disabled={op.pending}
              onClick={() => setConfirm(true)}
            >
              Remove
            </Button>
            {confirm ? (
              <div className="approval">
                <p>
                  Remove this binding? Access derived from this assignment will
                  be reevaluated.
                </p>
                <Button onClick={() => setConfirm(false)}>Keep</Button>
                <Button
                  danger
                  disabled={op.pending}
                  onClick={() =>
                    void op
                      .run("/role-bindings", { ...b, remove: true })
                      .then((r) => {
                        if (r) setConfirm(false);
                      })
                  }
                >
                  Confirm removal
                </Button>
                <ErrorView error={op.error} />
              </div>
            ) : null}
          </>
        ) : null}
      </td>
    </tr>
  );
}
function PolicyExplain() {
  const me = useSession(),
    [subject, setSubject] = useState(me.person.id),
    [target, setTarget] = useState<Target[]>([]),
    [action, setAction] = useState("view"),
    [resourceKind, setResourceKind] = useState("group"),
    [resourceId, setResourceId] = useState(""),
    [connection, setConnection] = useState(""),
    [name, setName] = useState(""),
    [descriptor, setDescriptor] = useState(""),
    [result, setResult] = useState<unknown>(null),
    [error, setError] = useState<Error | null>(null);
  const op = useOperation();
  const meta = useResource<Workspace>("/workspace");
  useEffect(
    () => setResult(null),
    [
      subject,
      action,
      target,
      connection,
      name,
      descriptor,
      resourceKind,
      resourceId,
    ],
  );
  return (
    <Form
      onSubmit={() => {
        setError(null);
        try {
          void op
            .run("/permissions/explain", {
              subject_id: subject,
              action,
              target:
                action === "manage"
                  ? { kind: resourceKind, id: resourceId }
                  : target[0],
              connection_id: connection,
              action_name: name,
              descriptor: descriptor ? JSON.parse(descriptor) : undefined,
            })
            .then((v) => {
              if (v) setResult(v);
            });
        } catch (e) {
          setError(e as Error);
        }
      }}
    >
      <p>
        Check your own access, or another subject only with explicit inspection
        authority. This sends no messages.
      </p>
      <Field label="Subject identity ID">
        <input
          required
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
        />
      </Field>
      <Field label="Action">
        <select value={action} onChange={(e) => setAction(e.target.value)}>
          {["view", "message", "ask", "decide", "manage"].map((a) => (
            <option key={a}>{a}</option>
          ))}
        </select>
      </Field>
      {action === "manage" ? (
        <div className="form-grid">
          <Field label="Resource category">
            <select
              value={resourceKind}
              onChange={(e) => setResourceKind(e.target.value)}
            >
              {Object.keys(
                meta.data?.permission_model.management_targets || {},
              ).map((kind) => (
                <option key={kind}>{kind}</option>
              ))}
            </select>
          </Field>
          <Field
            label="Exact resource ID"
            hint={meta.data?.permission_model.management_targets[resourceKind]}
          >
            <input
              required
              value={resourceId}
              onChange={(e) => setResourceId(e.target.value)}
            />
          </Field>
        </div>
      ) : (
        <TargetPicker value={target} onChange={setTarget} />
      )}
      <div className="form-grid">
        <Field label="Connection (optional)">
          <input
            value={connection}
            onChange={(e) => setConnection(e.target.value)}
          />
        </Field>
        <Field label="Action name (optional)">
          <input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
      </div>
      <JsonField
        label="Exact action descriptor (optional JSON)"
        value={descriptor}
        onChange={setDescriptor}
      />
      <ErrorView error={error || op.error} />
      <Button
        primary
        type="submit"
        disabled={
          op.pending || (action === "manage" ? !resourceId : !target.length)
        }
      >
        Check permission
      </Button>
      {result ? (
        <Details value={result} label="Decision and matched grant provenance" />
      ) : null}
    </Form>
  );
}
function RoutePreview() {
  const [target, setTarget] = useState<Target[]>([]),
    [action, setAction] = useState("message"),
    [result, setResult] = useState<unknown>(null),
    op = useOperation();
  return (
    <Form
      onSubmit={() =>
        void op
          .run("/route-preview", { target: target[0], action })
          .then((r) => {
            if (r) setResult(r);
          })
      }
    >
      <p>
        Preview permitted recipients without sending. This is not a cost
        estimate or hypothetical policy simulation.
      </p>
      <TargetPicker value={target} onChange={setTarget} />
      <Field label="Operation">
        <select value={action} onChange={(e) => setAction(e.target.value)}>
          <option value="message">Message</option>
          <option value="ask">Ask</option>
        </select>
      </Field>
      <ErrorView error={op.error} />
      <Button primary type="submit" disabled={op.pending || !target.length}>
        Preview recipients
      </Button>
      {result ? (
        <Details
          value={result}
          label="Authorized expansion and private recipient mode"
        />
      ) : null}
    </Form>
  );
}
