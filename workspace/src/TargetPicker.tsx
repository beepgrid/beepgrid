import { useDeferredValue, useEffect, useRef, useState } from "react";
import { api } from "./client";
import { useCollection } from "./data";
import type { Group, Identity, Instance, Kind, Target } from "./contracts";
import { Icon, IdentityIcon, QueryState, Search } from "./ui";

type Entry = Identity | Group | Instance;
export function TargetPicker({
  value,
  onChange,
  multiple = false,
  allowGroups = true,
  allowInstances = true,
  peopleOnly = false,
  agentsOnly = false,
  messaging = false,
  onIdentitiesChange,
}: {
  value: Target[];
  onChange: (v: Target[]) => void;
  onIdentitiesChange?: (identities: Identity[]) => void;
  multiple?: boolean;
  allowGroups?: boolean;
  allowInstances?: boolean;
  peopleOnly?: boolean;
  agentsOnly?: boolean;
  messaging?: boolean;
}) {
  const [kind, setKind] = useState<Kind>(
    peopleOnly ? "person" : value[0]?.kind || "agent",
  );
  const [search, setSearch] = useState("");
  const [expanding, setExpanding] = useState(false),
    [pickerError, setPickerError] = useState("");
  const term = useDeferredValue(search);
  const people = useCollection<Identity>("people", term, {}, kind === "person");
  const agents = useCollection<Identity>(
    "agents",
    term,
    {},
    kind === "agent" || kind === "instance",
  );
  const services = useCollection<Identity>(
    "services",
    term,
    {},
    kind === "service",
  );
  const groups = useCollection<Group>("groups", term, {}, kind === "group");
  const instances = useCollection<Instance>(
    "instances",
    term,
    {},
    kind === "instance",
  );
  const q = {
    person: people,
    agent: agents,
    service: services,
    group: groups,
    instance: instances,
  }[kind];
  const [expanded, setExpanded] = useState<Record<string, string[]>>({});
  const [names, setNames] = useState<Record<string, string>>({});
  const chosenIdentities = useRef(new Map<string, Identity>());
  function change(targets: Target[], extra: Identity[] = []) {
    for (const item of [
      ...people.items,
      ...agents.items,
      ...services.items,
      ...extra,
    ])
      chosenIdentities.current.set(item.id, item);
    onChange(targets);
    onIdentitiesChange?.(
      targets.flatMap((target) => {
        const item = chosenIdentities.current.get(target.id);
        return item ? [item] : [];
      }),
    );
  }
  function name(item: Entry) {
    if ("name" in item) return item.name;
    const role = agents.items.find((a) => a.id === item.agent_id);
    return `${role?.name || "Agent"} · ${item.id.slice(-6)}`;
  }
  useEffect(() => {
    setNames((old) => {
      const next = { ...old };
      for (const item of q.items) next[kind + item.id] = name(item);
      return next;
    });
  }, [q.data, agents.data, kind]);
  async function toggle(item: Entry) {
    if (messaging && kind === "group") {
      const group = item as Group;
      if (!group.members_complete) {
        setPickerError(
          "Choose individual recipients. You cannot view every member of this group.",
        );
        return;
      }
      setExpanding(true);
      setPickerError("");
      try {
        const memberKind = group.purpose === "agent_role" ? "agent" : "person";
        let cursor = "",
          members: Identity[] = [];
        do {
          const page = await api<{ items: Identity[]; next_cursor?: string }>(
            "/query",
            {
              collection: memberKind === "agent" ? "agents" : "people",
              filters: { group_id: group.id, disabled: "false" },
              limit: 100,
              cursor,
            },
          );
          members = [...members, ...page.items];
          cursor = page.next_cursor || "";
        } while (cursor && members.length <= 100);
        if (members.length > 100)
          throw Error("Choose up to 100 recipients per message.");
        if (!members.length) throw Error("No active recipients in this group.");
        const combined = [
          ...value.filter((t) => !members.some((m) => m.id === t.id)),
          ...members.map((m) => ({ kind: memberKind as Kind, id: m.id })),
        ];
        if (combined.length > 100)
          throw Error("Choose up to 100 recipients per message.");
        setExpanded((old) => ({
          ...old,
          [group.id]: members.map((m) => m.id),
        }));
        setNames((old) => ({
          ...old,
          ...Object.fromEntries(
            members.map((m) => [memberKind + m.id, m.name]),
          ),
        }));
        const allSelected = members.every((m) =>
          value.some((t) => t.id === m.id),
        );
        change(
          allSelected
            ? value.filter((t) => !members.some((m) => m.id === t.id))
            : [
                ...value.filter((t) => !members.some((m) => m.id === t.id)),
                ...members.map((m) => ({ kind: memberKind as Kind, id: m.id })),
              ],
          members,
        );
      } catch (e) {
        setPickerError((e as Error).message);
      } finally {
        setExpanding(false);
      }
      return;
    }
    const target = { kind, id: item.id };
    const found = value.some((t) => t.kind === kind && t.id === item.id);
    if (multiple && !found && value.length >= 100) {
      setPickerError("Choose up to 100 recipients per message.");
      return;
    }
    setPickerError("");
    change(
      multiple
        ? found
          ? value.filter((t) => !(t.kind === kind && t.id === item.id))
          : [...value, target]
        : found
          ? []
          : [target],
    );
  }
  const categories: [Kind, string][] = peopleOnly
    ? [["person", "People"]]
    : agentsOnly
      ? [["agent", "Agents"]]
      : [
          ["agent", "Agents"],
          ["person", "People"],
          ...(!messaging
            ? [["service", "Software services"] as [Kind, string]]
            : []),
          ...(allowGroups
            ? [
                ["group", messaging ? "Roles & teams" : "Teams & roles"] as [
                  Kind,
                  string,
                ],
              ]
            : []),
          ...(allowInstances && !messaging
            ? [["instance", "Agent connections"] as [Kind, string]]
            : []),
        ];
  return (
    <div className="recipient-picker">
      <div className="recipient-tabs" aria-label="Recipient categories">
        {categories.map(([id, label]) => (
          <button
            type="button"
            key={id}
            aria-pressed={kind === id}
            onClick={() => {
              setKind(id);
              setSearch("");
            }}
          >
            {label}
          </button>
        ))}
        {messaging ? (
          <select
            aria-label="Other recipient types"
            value={["service", "instance"].includes(kind) ? kind : ""}
            onChange={(e) => {
              if (e.target.value) {
                setKind(e.target.value as Kind);
                setSearch("");
              }
            }}
          >
            <option value="">More</option>
            <option value="service">Software services</option>
            <option value="instance">Agent connections</option>
          </select>
        ) : null}
      </div>
      {pickerError ? (
        <div role="alert" className="error">
          {pickerError}
        </div>
      ) : null}
      <Search
        value={search}
        onChange={setSearch}
        placeholder="Search by name"
      />
      {value.length > 0 ? (
        <div className="recipient-chips">
          {value.map((t) => (
            <button
              type="button"
              key={t.kind + t.id}
              onClick={() => change(value.filter((x) => x !== t))}
              aria-label={`Remove ${names[t.kind + t.id] || "recipient"}`}
            >
              <IdentityIcon
                kind={t.kind}
                id={t.id}
                label={names[t.kind + t.id]}
              />
              <span>{names[t.kind + t.id] || "Selected recipient"}</span>
              <Icon name="close" size={13} />
            </button>
          ))}
        </div>
      ) : null}
      <div className="recipient-results">
        <QueryState query={q} empty="No matches">
          {q.items
            .filter(
              (item) =>
                !messaging ||
                kind !== "group" ||
                ["agent_role", "team"].includes((item as Group).purpose || ""),
            )
            .map((item) => {
              const checked =
                messaging && kind === "group"
                  ? (expanded[item.id] || (item as Group).members).length > 0 &&
                    (expanded[item.id] || (item as Group).members).every((id) =>
                      value.some((t) => t.id === id),
                    )
                  : value.some((t) => t.kind === kind && t.id === item.id);
              return (
                <button
                  type="button"
                  role={multiple ? "checkbox" : "radio"}
                  aria-checked={checked}
                  className={`recipient-option ${checked ? "selected" : ""}`}
                  key={item.id}
                  disabled={expanding}
                  onClick={() => void toggle(item)}
                >
                  <IdentityIcon kind={kind} id={item.id} label={name(item)} />
                  <span>
                    <strong>{name(item)}</strong>
                    <small>
                      {kind === "agent"
                        ? "Agent"
                        : kind === "group"
                          ? (item as Group).purpose === "team"
                            ? "Team"
                            : (item as Group).purpose === "agent_role"
                              ? `${(item as Group).members.length} agents`
                              : "Contact group"
                          : kind === "instance"
                            ? (item as Instance).presence
                            : kind === "service"
                              ? "Software service"
                              : "Person"}
                    </small>
                  </span>
                  <span className="recipient-check">
                    {checked ? <Icon name="check" size={15} /> : null}
                  </span>
                </button>
              );
            })}
        </QueryState>
      </div>
    </div>
  );
}
