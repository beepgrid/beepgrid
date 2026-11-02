import { useRef, useState, type ReactNode } from "react";
import { useCollection, useOperation, useResource, useSession } from "../data";
import type {
  ChannelPolicy,
  DeliveryPreferences as Preferences,
  Connection,
  Destination,
  Group,
  Identity,
  Target,
} from "../contracts";
import { Button, ErrorView, Field, Icon, Loading } from "../ui";
import { ChannelMark } from "../ChannelMark";

const sourceKey = (t: Target) => t.kind + ":" + t.id;
export function DeliveryPreferences({
  renderDestinations,
}: {
  renderDestinations?: (personID: string) => ReactNode;
}) {
  const me = useSession();
  const [person, setPerson] = useState(me.person.id);
  const people = useCollection<Identity>("people", "", {}, me.admin);
  const preferences = useResource<Preferences>(
    `/people/${person}/delivery-preferences`,
  );
  const destinations = useCollection<Destination>("destinations");
  const connections = useCollection<Connection>("connections");
  const agents = useCollection<Identity>("agents");
  const roles = useCollection<Group>("groups", "", { purpose: "agent_role" });
  return (
    <section className="delivery-preferences">
      <div className="toolbar">
        <div>
          <h2>
            Where to reach {person === me.person.id ? "you" : "this Person"}
          </h2>
          <p className="help">
            Messages always stay in Beepgrid. Choose where else they arrive.
          </p>
        </div>
        {me.admin ? (
          <Field label="Person">
            <select value={person} onChange={(e) => setPerson(e.target.value)}>
              <option value={me.person.id}>Me</option>
              {people.items
                .filter((p) => p.id !== me.person.id)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
            </select>
          </Field>
        ) : null}
      </div>
      <ErrorView
        error={
          preferences.error ||
          destinations.error ||
          connections.error ||
          agents.error ||
          roles.error
        }
      />
      {preferences.isPending ||
      destinations.isPending ||
      connections.isPending ? (
        <Loading />
      ) : preferences.data ? (
        <PreferenceEditor
          key={person + ":" + preferences.data.version}
          value={preferences.data}
          destinations={destinations.items.filter(
            (d) => d.principal_id === person,
          )}
          connections={connections.items}
          agents={agents.items}
          roles={roles.items}
        />
      ) : null}
      {renderDestinations ? (
        <div className="delivery-destinations">
          <h2>Delivery destinations</h2>
          {renderDestinations(person)}
        </div>
      ) : null}
      <div className="actions">
        {agents.hasNextPage ? (
          <Button onClick={() => void agents.fetchNextPage()}>
            Load more Agents
          </Button>
        ) : null}
        {roles.hasNextPage ? (
          <Button onClick={() => void roles.fetchNextPage()}>
            Load more Agent roles
          </Button>
        ) : null}
        {destinations.hasNextPage ? (
          <Button onClick={() => void destinations.fetchNextPage()}>
            Load more destinations
          </Button>
        ) : null}
      </div>
    </section>
  );
}
export function PreferenceEditor({
  value,
  destinations,
  connections,
  agents,
  roles,
}: {
  value: Preferences;
  destinations: Destination[];
  connections: Connection[];
  agents: Identity[];
  roles: Group[];
}) {
  const [draft, setDraft] = useState<Preferences>(() => structuredClone(value));
  const [selected, setSelected] = useState("default");
  const [saved, setSaved] = useState(false);
  const op = useOperation();
  const selectedOverride = draft.overrides.find(
    (o) => sourceKey(o.source) === selected,
  );
  const sources = [
    ...roles.map((g) => ({ kind: "group", id: g.id, name: g.name })),
    ...agents
      .filter((a) => !a.disabled)
      .map((a) => ({ kind: "agent", id: a.id, name: a.name })),
  ];
  const source = sources.find((s) => sourceKey(s as Target) === selected);
  const unavailable = draft.overrides.filter(
    (o) => !sources.some((s) => sourceKey(s as Target) === sourceKey(o.source)),
  );
  const initial: ChannelPolicy = {
    mode: "preferred",
    destinations: [...destinations]
      .sort(
        (a, b) =>
          Number(b.mode === "primary") - Number(a.mode === "primary") ||
          a.priority - b.priority || a.id.localeCompare(b.id),
      )
      .map((d) => d.id),
    followup_seconds: 0,
  };
  const basePolicy = draft.default || initial;
  const rolePolicy =
    source?.kind === "agent"
      ? draft.overrides.find(
          (o) =>
            o.source.kind === "group" &&
            roles
              .find((g) => g.id === o.source.id)
              ?.members.includes(source.id),
        )?.policy
      : undefined;
  const inherited = rolePolicy || basePolicy;
  const policy =
    selected === "default" ? inherited : selectedOverride?.policy || inherited;
  const usingDefault = selected !== "default" && !selectedOverride;
  function change(next: ChannelPolicy) {
    setSaved(false);
    setDraft((current) =>
      selected === "default"
        ? { ...current, default: next }
        : {
            ...current,
            overrides: current.overrides.some(
              (o) => sourceKey(o.source) === selected,
            )
              ? current.overrides.map((o) =>
                  sourceKey(o.source) === selected ? { ...o, policy: next } : o,
                )
              : [
                  ...current.overrides,
                  {
                    source: {
                      kind: source!.kind as Target["kind"],
                      id: source!.id,
                    },
                    policy: next,
                  },
                ],
          },
    );
  }
  const roleOverrides = draft.overrides.filter(
    (o) => o.source.kind === "group",
  );
  function moveRole(from: number, to: number) {
    const reordered = [...roleOverrides];
    const [item] = reordered.splice(from, 1);
    reordered.splice(to, 0, item);
    setDraft((d) => ({
      ...d,
      overrides: [
        ...d.overrides.filter((o) => o.source.kind !== "group"),
        ...reordered,
      ],
    }));
    setSaved(false);
  }
  return (
    <>
      <div className="routing-controls">
        <Field label="Preferences for">
          <select
            value={selected}
            onChange={(e) => {
              setSelected(e.target.value);
              setSaved(false);
            }}
          >
            <option value="default">Default</option>
            <optgroup label="Agent roles">
              {roles.map((g) => (
                <option key={g.id} value={"group:" + g.id}>
                  {g.name}
                  {draft.overrides.some((o) => o.source.id === g.id)
                    ? " · Custom"
                    : ""}
                </option>
              ))}
            </optgroup>
            <optgroup label="Agents">
              {agents
                .filter((a) => !a.disabled)
                .map((a) => (
                  <option key={a.id} value={"agent:" + a.id}>
                    {a.name}
                    {draft.overrides.some((o) => o.source.id === a.id)
                      ? " · Custom"
                      : ""}
                  </option>
                ))}
            </optgroup>
            {unavailable.length ? (
              <optgroup label="Unavailable">
                {unavailable.map((o) => (
                  <option key={sourceKey(o.source)} value={sourceKey(o.source)}>
                    Unavailable{" "}
                    {o.source.kind === "group" ? "Agent role" : "Agent"} ·
                    Custom
                  </option>
                ))}
              </optgroup>
            ) : null}
          </select>
        </Field>
        {selected !== "default" ? (
          <label className="field routing-inherit">
            <input
              type="checkbox"
              checked={usingDefault}
              onChange={(e) => {
                setSaved(false);
                if (e.target.checked) {
                  setDraft((d) => ({
                    ...d,
                    overrides: d.overrides.filter(
                      (o) => sourceKey(o.source) !== selected,
                    ),
                  }));
                  if (!source) setSelected("default");
                } else change(structuredClone(inherited));
              }}
            />
            Use default
          </label>
        ) : null}
        <span
          className="routing-info"
          title="An Agent override takes precedence over Agent roles, then your default. If several Agent roles match, the first role below wins."
          aria-label="How overrides work"
        >
          <Icon name="info" size={16} />
        </span>
      </div>
      {selected !== "default" && !source ? (
        <p className="help">
          This contact is unavailable. Select Use default to remove its
          override.
        </p>
      ) : null}
      <fieldset
        className="routing-editor"
        disabled={
          usingDefault || op.pending || (selected !== "default" && !source)
        }
      >
        <Field label="Delivery">
          <select
            value={policy.mode}
            onChange={(e) =>
              change({
                ...policy,
                mode: e.target.value as ChannelPolicy["mode"],
                followup_seconds:
                  e.target.value === "all" ? 0 : policy.followup_seconds,
              })
            }
          >
            <option value="preferred">Preferred channel, then fallbacks</option>
            <option value="all">All selected channels</option>
          </select>
        </Field>
        <ChannelStack
          ids={policy.destinations}
          destinations={destinations}
          connections={connections}
          onChange={(ids) => change({ ...policy, destinations: ids })}
        />
        {policy.mode === "preferred" && policy.destinations.length > 1 ? (
          <div className="routing-followup">
            <label className="field">
              <input
                type="checkbox"
                checked={policy.followup_seconds > 0}
                onChange={(e) =>
                  change({
                    ...policy,
                    followup_seconds: e.target.checked ? 900 : 0,
                  })
                }
              />
              If I haven’t responded, try the next channel after
            </label>
            {policy.followup_seconds > 0 ? (
              <label className="routing-delay">
                <input
                  aria-label="Follow-up delay in minutes"
                  type="number"
                  min={1}
                  max={10080}
                  value={policy.followup_seconds / 60}
                  onChange={(e) =>
                    change({
                      ...policy,
                      followup_seconds: Number(e.target.value) * 60,
                    })
                  }
                />{" "}
                minutes
              </label>
            ) : null}
            <span
              className="routing-info"
              title="Only open questions and approvals follow this timer. An answer or cancellation stops future follow-ups. Definite delivery failures try the next channel immediately."
              aria-label="About channel follow-ups"
            >
              <Icon name="info" size={16} />
            </span>
          </div>
        ) : null}
      </fieldset>
      {usingDefault ? (
        <p className="help">
          {rolePolicy
            ? "Using Agent role preferences."
            : "Using the default. An Agent role override may apply."}
        </p>
      ) : selected === "default" && !draft.default ? (
        <p className="help">
          Existing destination settings apply until you change this list.
        </p>
      ) : null}
      {roleOverrides.length > 1 ? (
        <div className="routing-role-order">
          <h3>Agent role priority</h3>
          <p className="help">
            The first matching role wins. Individual Agent preferences come
            first.
          </p>
          <ReorderStack
            items={roleOverrides.map((o) => ({
              id: o.source.id,
              content:
                roles.find((g) => g.id === o.source.id)?.name || "Agent role",
            }))}
            onMove={moveRole}
            label="Agent role"
          />
        </div>
      ) : null}
      <div className="actions routing-save">
        <Button
          primary
          disabled={
            op.pending || JSON.stringify(draft) === JSON.stringify(value)
          }
          onClick={() =>
            void op
              .run<Preferences>(
                `/people/${value.person_id}/delivery-preferences`,
                draft,
              )
              .then((result) => {
                if (result) {
                  setDraft(result);
                  setSaved(true);
                }
              })
          }
        >
          Save preferences
        </Button>
        {saved ? <span role="status">Saved</span> : null}
        <ErrorView error={op.error} />
      </div>
    </>
  );
}
function ReorderStack({
  items,
  onMove,
  label,
}: {
  items: { id: string; content: ReactNode }[];
  onMove: (from: number, to: number) => void;
  label: string;
}) {
  const dragging = useRef<number | null>(null);
  return (
    <ol className="routing-stack">
      {items.map((item, index) => (
        <li
          key={item.id}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            if (dragging.current !== null) {
              onMove(dragging.current, index);
              dragging.current = null;
            }
          }}
        >
          <button
            type="button"
            className="routing-grip"
            draggable
            aria-label={`Move ${label} ${index + 1}`}
            title="Drag to reorder, or use the arrow keys"
            onDragStart={(e) => {
              dragging.current = index;
              e.dataTransfer.effectAllowed = "move";
              const row = e.currentTarget.closest("li");
              if (row) e.dataTransfer.setDragImage?.(row, 20, 24);
              e.dataTransfer.setData("text/plain", String(index));
            }}
            onDragEnd={() => {
              dragging.current = null;
            }}
            onKeyDown={(e) => {
              const next =
                e.key === "ArrowUp"
                  ? index - 1
                  : e.key === "ArrowDown"
                    ? index + 1
                    : index;
              if (next !== index) {
                e.preventDefault();
                if (next >= 0 && next < items.length) onMove(index, next);
              }
            }}
          >
            <span aria-hidden="true">⠿</span>
          </button>
          <span className="routing-number" aria-hidden="true">
            {index + 1}
          </span>
          {item.content}
        </li>
      ))}
    </ol>
  );
}
function ChannelStack({
  ids,
  destinations,
  connections,
  onChange,
}: {
  ids: string[];
  destinations: Destination[];
  connections: Connection[];
  onChange: (ids: string[]) => void;
}) {
  function move(from: number, to: number) {
    const next = [...ids];
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item);
    onChange(next);
  }
  return (
    <>
      <ReorderStack
        label="channel"
        onMove={move}
        items={ids.map((id, index) => {
          const destination = destinations.find((d) => d.id === id),
            connection = connections.find(
              (c) => c.id === destination?.connection_id,
            );
          return {
            id,
            content: (
              <>
                <span className="routing-channel-mark">
                  <ChannelMark kind={connection?.kind || "native"} />
                </span>
                <select
                  aria-label={`Channel ${index + 1}`}
                  value={id}
                  onChange={(e) =>
                    onChange(
                      ids.map((d, i) => (i === index ? e.target.value : d)),
                    )
                  }
                >
                  {!destination ? (
                    <option value={id}>Removed destination</option>
                  ) : null}
                  {destinations
                    .filter((d) => d.id === id || !ids.includes(d.id))
                    .map((d) => {
                      const c = connections.find(
                        (c) => c.id === d.connection_id,
                      );
                      return (
                        <option key={d.id} value={d.id}>
                          {c?.name || "Connection"} · {d.address}
                          {c && !c.enabled ? " · Disabled" : ""}
                        </option>
                      );
                    })}
                </select>
                <Button
                  aria-label={`Remove channel ${index + 1}`}
                  title="Remove channel"
                  onClick={() => onChange(ids.filter((_, i) => i !== index))}
                >
                  <Icon name="close" size={15} />
                </Button>
              </>
            ),
          };
        })}
      />
      {!ids.length ? (
        <p className="help">
          Only in Beepgrid. Add a channel to receive messages elsewhere.
        </p>
      ) : null}
      <Button
        icon="plus"
        disabled={
          ids.length >= 16 || destinations.every((d) => ids.includes(d.id))
        }
        onClick={() => {
          const next = destinations.find((d) => !ids.includes(d.id));
          if (next) onChange([...ids, next.id]);
        }}
      >
        Add channel
      </Button>
    </>
  );
}
