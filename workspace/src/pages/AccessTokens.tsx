import { useState } from "react";
import { useCapabilities, useCollection, useOperation } from "../data";
import type { Credential, Target } from "../contracts";
import { TargetPicker } from "../TargetPicker";
import {
  Badge,
  Button,
  Copy,
  DateValue,
  ErrorView,
  Field,
  Form,
  Modal,
  QueryState,
} from "../ui";

const permissionOptions = [
  ["read", "Read"],
  ["message", "Send messages"],
  ["request", "Create requests"],
  ["respond", "Answer and approve"],
  ["execute", "Report execution"],
  ["manage", "Manage resources"],
];
function tokenStatus(c: Credential) {
  if (c.revoked) return { label: "Revoked", tone: "neutral" as const };
  if (c.expires_at && Date.parse(c.expires_at) <= Date.now())
    return { label: "Expired", tone: "neutral" as const };
  const deadline = c.renewable_until || c.expires_at;
  if (deadline && Date.parse(deadline) < Date.now() + 7 * 86400000)
    return { label: "Expiring soon", tone: "warn" as const };
  return { label: "Active", tone: "good" as const };
}
function TokenDate({
  value,
  relative = false,
}: {
  value: string;
  relative?: boolean;
}) {
  const date = new Date(value);
  const minutes = Math.max(
    0,
    Math.floor((Date.now() - date.getTime()) / 60000),
  );
  const label = !relative
    ? date.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
      })
    : minutes < 1
      ? "Just now"
      : minutes < 60
        ? `${minutes}m ago`
        : minutes < 1440
          ? `${Math.floor(minutes / 60)}h ago`
          : `${Math.floor(minutes / 1440)}d ago`;
  return (
    <time dateTime={value} title={date.toLocaleString()}>
      {label}
    </time>
  );
}
function localDateTime(value: Date) {
  return new Date(value.getTime() - value.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
function accessLabel(permissions: string[] | null | undefined) {
  if (!permissions) return "";
  if (permissions.length === 1 && permissions[0] === "read")
    return " · Read only";
  return " · Limited access";
}
function PermissionFields({
  value,
  onChange,
}: {
  value: string[] | null;
  onChange: (v: string[] | null) => void;
}) {
  const same = (a: string[] | null, b: string[]) =>
    !!a && a.length === b.length && b.every((v) => a.includes(v));
  const [custom, setCustom] = useState(
    !!value &&
      !same(value, ["read"]) &&
      !same(value, ["read", "message", "request", "respond"]),
  );
  const preset = custom
    ? "custom"
    : value === null
      ? "identity"
      : value.length === 1 && value[0] === "read"
        ? "read"
        : "messages";
  return (
    <>
      <Field
        label="Token access"
        hint="Limits this token. It never grants access its owner does not have."
      >
        <select
          aria-label="Token access"
          value={preset}
          onChange={(e) => {
            setCustom(e.target.value === "custom");
            onChange(
              e.target.value === "identity"
                ? null
                : e.target.value === "read"
                  ? ["read"]
                  : e.target.value === "messages"
                    ? ["read", "message", "request", "respond"]
                    : value || ["read"],
            );
          }}
        >
          <option value="identity">Use owner's permissions</option>
          <option value="read">Read only</option>
          <option value="messages">Messaging and replies</option>
          <option value="custom">Custom permissions</option>
        </select>
      </Field>
      {custom ? (
        <fieldset className="token-permissions">
          <legend>Allowed actions</legend>
          {permissionOptions.map(([id, label]) => (
            <label key={id}>
              <input
                type="checkbox"
                checked={value?.includes(id) || false}
                onChange={(e) =>
                  onChange(
                    e.target.checked
                      ? [...(value || []), id]
                      : (value || []).filter((v) => v !== id),
                  )
                }
              />
              {label}
            </label>
          ))}
        </fieldset>
      ) : null}
    </>
  );
}
function ExpiryField({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  const custom = value.startsWith("date:");
  return (
    <Field label="Expires">
      <select
        value={custom ? "custom" : value}
        onChange={(e) =>
          onChange(e.target.value === "custom" ? "date:" : e.target.value)
        }
      >
        <option value="7">In 7 days</option>
        <option value="30">In 30 days</option>
        <option value="90">In 90 days</option>
        <option value="365">In 1 year</option>
        <option value="custom">Custom date</option>
      </select>
      {custom ? (
        <input
          aria-label="Expiry date"
          required
          type="datetime-local"
          min={localDateTime(new Date(Date.now() + 60000))}
          value={value.slice(5)}
          onChange={(e) => onChange("date:" + e.target.value)}
        />
      ) : null}
    </Field>
  );
}
function expires(value: string) {
  return value.startsWith("date:")
    ? new Date(value.slice(5)).toISOString()
    : new Date(Date.now() + Number(value) * 86400000).toISOString();
}
type Secret = {
  token: string;
  credential?: Credential;
  id?: string;
  kind?: string;
  expires_at?: string;
};
export default function AccessTokens() {
  const [search, setSearch] = useState(""),
    q = useCollection<Credential>("credentials", search),
    [creating, setCreating] = useState(false),
    [edit, setEdit] = useState<Credential | null>(null),
    [rotate, setRotate] = useState<Credential | null>(null),
    [revoke, setRevoke] = useState<Credential | null>(null),
    [secret, setSecret] = useState<Secret | null>(null),
    [filter, setFilter] = useState("active");
  const rows = q.items
    .filter(
      (c) =>
        c.kind !== "browser" &&
        c.kind !== "admin" &&
        [c.name, c.owner_name, c.token_hint, c.id]
          .join(" ")
          .toLowerCase()
          .includes(search.toLowerCase()),
    )
    .filter(
      (c) =>
        filter === "all" ||
        (filter === "active" &&
          ["Active", "Expiring soon"].includes(tokenStatus(c).label)) ||
        (filter === "inactive" &&
          ["Expired", "Revoked"].includes(tokenStatus(c).label)),
    );
  return (
    <>
      <div className="page-header">
        <div>
          <h2>Access tokens</h2>
          <p>Connect applications and Agents securely.</p>
        </div>
        <Button primary icon="plus" onClick={() => setCreating(true)}>
          Create token
        </Button>
      </div>
      <div className="token-toolbar">
        <input
          type="search"
          aria-label="Search tokens"
          placeholder="Search token names"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Token status"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        >
          <option value="active">Active tokens</option>
          <option value="inactive">Expired and revoked</option>
          <option value="all">All tokens</option>
        </select>
      </div>
      <QueryState query={q} empty="No access tokens">
        <div className="table-wrap">
          <table className="token-table">
            <thead>
              <tr>
                <th>Token</th>
                <th>Owner</th>
                <th>Status</th>
                <th>Expires</th>
                <th title="Approximate, updated at most once every five minutes">
                  Last used
                </th>
                <th>
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => {
                const status = tokenStatus(c),
                  system = c.kind === "admin",
                  active = ["Active", "Expiring soon"].includes(status.label);
                return (
                  <tr key={c.id}>
                    <td>
                      <strong>
                        {c.name ||
                          (system
                            ? "Administrator"
                            : c.kind === "worker"
                              ? "Agent connection"
                              : c.kind === "enrollment"
                                ? "Enrollment token"
                                : "API token")}
                      </strong>
                      <small>
                        <code>{c.token_hint || c.id}</code>
                        <span>
                          {c.kind === "worker"
                            ? "Agent connection"
                            : c.kind === "enrollment"
                              ? "Enrollment"
                              : "API"}
                          {accessLabel(c.permissions)}
                        </span>
                      </small>
                    </td>
                    <td>{c.owner_name || c.principal_id}</td>
                    <td>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </td>
                    <td
                      title={
                        c.renewable_until
                          ? "Renews during activity, up to this deadline. An idle connection expires after 24 hours."
                          : undefined
                      }
                    >
                      {c.renewable_until ? (
                        <>
                          <TokenDate value={c.renewable_until} />
                          <small>Auto-renewing</small>
                        </>
                      ) : c.expires_at ? (
                        <TokenDate value={c.expires_at} />
                      ) : (
                        "No expiry"
                      )}
                    </td>
                    <td>
                      {c.last_used_at ? (
                        <TokenDate value={c.last_used_at} relative />
                      ) : (
                        <span className="muted">Never</span>
                      )}
                    </td>
                    <td>
                      <div className="token-actions">
                        {!system && !c.revoked ? (
                          <>
                            <Button
                              onClick={() => setEdit(c)}
                              disabled={!active}
                            >
                              Edit
                            </Button>
                            {active ? (
                              <Button onClick={() => setRotate(c)}>
                                Rotate
                              </Button>
                            ) : null}
                            <Button danger onClick={() => setRevoke(c)}>
                              Revoke
                            </Button>
                          </>
                        ) : system ? (
                          <span className="muted">System managed</span>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!rows.length ? (
            <p className="token-empty">No matching tokens.</p>
          ) : null}
        </div>
        {q.hasNextPage ? (
          <Button
            onClick={() => void q.fetchNextPage()}
            disabled={q.isFetchingNextPage}
          >
            Load more
          </Button>
        ) : null}
      </QueryState>
      {creating ? (
        <TokenForm
          onClose={() => setCreating(false)}
          onCreated={(s) => {
            setCreating(false);
            setSecret(s);
          }}
        />
      ) : null}
      {edit ? (
        <TokenForm
          key={edit.id}
          credential={edit}
          onClose={() => setEdit(null)}
          onCreated={() => setEdit(null)}
        />
      ) : null}
      {rotate ? (
        <RotateToken
          key={rotate.id}
          credential={rotate}
          onClose={() => setRotate(null)}
          onCreated={(s) => {
            setRotate(null);
            setSecret(s);
          }}
        />
      ) : null}
      {revoke ? (
        <RevokeToken
          key={revoke.id}
          credential={revoke}
          onClose={() => setRevoke(null)}
        />
      ) : null}
      {secret ? (
        <Modal
          title={
            secret.credential
              ? "Save your replacement token"
              : "Save your new token"
          }
          onClose={() => setSecret(null)}
        >
          <div className="form">
            <p>
              Shown once. Store it in your secret manager or a private file
              outside source control.
            </p>
            <div className="token-secret">
              <code>{secret.token}</code>
              <Copy value={secret.token} label="Copy token" />
            </div>
            {secret.credential?.previous_until ? (
              <p>
                The previous token works until{" "}
                <DateValue value={secret.credential.previous_until} />. Update
                your connection before then. Its identity and pending work stay
                the same.
              </p>
            ) : null}
            <Button primary onClick={() => setSecret(null)}>
              I have saved it securely
            </Button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
function TokenForm({
  credential: c,
  onClose,
  onCreated,
}: {
  credential?: Credential;
  onClose: () => void;
  onCreated: (s: Secret) => void;
}) {
  const [name, setName] = useState(c?.name || ""),
    [target, setTarget] = useState<Target[]>([]),
    [kind, setKind] = useState(c?.kind || "api"),
    [prefix, setPrefix] = useState(c?.prefix || ""),
    [expiry, setExpiry] = useState("30"),
    [changeExpiry, setChangeExpiry] = useState(!c),
    [permissions, setPermissions] = useState<string[] | null>(
      c?.permissions || null,
    ),
    op = useOperation(),
    cap = useCapabilities(target[0]);
  const limitedEmpty = permissions?.length === 0;
  return (
    <Modal
      title={c ? "Edit token" : "Create access token"}
      wide
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          void op
            .run<Secret>(
              c ? `/credentials/${c.id}/edit` : "/credentials",
              c
                ? {
                    version: c.version,
                    name,
                    ...(changeExpiry ? { expires_at: expires(expiry) } : {}),
                    ...(kind === "api"
                      ? permissions === null
                        ? { clear_permissions: true }
                        : { permissions }
                      : {}),
                  }
                : {
                    name,
                    principal_id: target[0]?.id,
                    kind,
                    prefix: kind === "enrollment" ? prefix : "",
                    expires_at: expires(expiry),
                    ...(permissions ? { permissions } : {}),
                  },
            )
            .then((r) => {
              if (r) onCreated(r);
            })
        }
      >
        <Field label="Token name">
          <input
            autoFocus
            required
            maxLength={120}
            placeholder="e.g. Production backend"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        {!c ? (
          <>
            <TargetPicker
              value={target}
              onChange={setTarget}
              allowGroups={false}
              allowInstances={false}
            />
            <Field label="Token type">
              <select
                value={kind}
                onChange={(e) => {
                  setKind(e.target.value);
                  setPermissions(null);
                }}
              >
                <option value="api">API access</option>
                <option value="enrollment">Agent enrollment</option>
              </select>
            </Field>
          </>
        ) : null}
        {kind === "enrollment" ? (
          <Field
            label="Allowed Agent prefix"
            hint="Only Agents in this namespace can connect."
          >
            <input
              required
              disabled={!!c}
              value={prefix}
              onChange={(e) => setPrefix(e.target.value)}
              placeholder="research/"
            />
          </Field>
        ) : null}
        {kind === "api" ? (
          <PermissionFields value={permissions} onChange={setPermissions} />
        ) : null}
        {c ? (
          <label className="check">
            <input
              type="checkbox"
              checked={changeExpiry}
              onChange={(e) => setChangeExpiry(e.target.checked)}
            />
            Change expiry
          </label>
        ) : null}
        {changeExpiry ? (
          <ExpiryField value={expiry} onChange={setExpiry} />
        ) : null}
        <ErrorView error={op.error || (!c ? cap.error : null)} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            type="submit"
            disabled={
              op.pending ||
              limitedEmpty ||
              (!c && (!target.length || !cap.data?.operations.issue_credential))
            }
          >
            {c ? "Save changes" : "Create token"}
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
function RotateToken({
  credential: c,
  onClose,
  onCreated,
}: {
  credential: Credential;
  onClose: () => void;
  onCreated: (s: Secret) => void;
}) {
  const [overlap, setOverlap] = useState("3600"),
    op = useOperation();
  return (
    <Modal title="Rotate token" onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run<Secret>(`/credentials/${c.id}/rotate`, {
              version: c.version,
              overlap_seconds: Number(overlap),
            })
            .then((r) => {
              if (r) onCreated(r);
            })
        }
      >
        <p>
          Create a new secret for <strong>{c.name || c.owner_name}</strong>.
          Permissions, expiry and the connection stay the same.
        </p>
        <Field label="Previous token remains valid for">
          <select value={overlap} onChange={(e) => setOverlap(e.target.value)}>
            <option value="3600">1 hour</option>
            <option value="86400">24 hours</option>
            <option value="300">5 minutes</option>
            <option value="0">Revoke immediately</option>
          </select>
        </Field>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={op.pending}>
            Create replacement
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
function RevokeToken({
  credential: c,
  onClose,
}: {
  credential: Credential;
  onClose: () => void;
}) {
  const [workers, setWorkers] = useState(false),
    op = useOperation();
  return (
    <Modal title="Revoke token?" onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run(`/credentials/${c.id}/revoke`, { revoke_workers: workers })
            .then((r) => {
              if (r) onClose();
            })
        }
      >
        <p>
          <strong>{c.name || c.owner_name}</strong> will stop accepting
          authenticated calls. History is preserved.
        </p>
        {c.kind === "enrollment" ? (
          <label className="check">
            <input
              type="checkbox"
              checked={workers}
              onChange={(e) => setWorkers(e.target.checked)}
            />
            Also stop connections issued by this token
          </label>
        ) : null}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Keep token</Button>
          <Button primary danger type="submit" disabled={op.pending}>
            Revoke token
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
