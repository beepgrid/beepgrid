import { useDeferredValue, useState } from "react";
import { useCollection } from "./data";
import type { Group, Identity, Instance, Kind, Target } from "./contracts";
import { Badge, Field, IdentityIcon, QueryState, Search } from "./ui";

export function TargetPicker({
  value,
  onChange,
  multiple = false,
  allowGroups = true,
  allowInstances = true,
}: {
  value: Target[];
  onChange: (v: Target[]) => void;
  multiple?: boolean;
  allowGroups?: boolean;
  allowInstances?: boolean;
}) {
  const [kind, setKind] = useState<Kind>("agent"),
    [search, setSearch] = useState("");
  const deferred = useDeferredValue(search);
  const collection = {
    person: "people",
    agent: "agents",
    service: "services",
    group: "groups",
    instance: "instances",
  }[kind];
  const q = useCollection<Identity | Group | Instance>(collection, deferred);
  function toggle(id: string) {
    const target = { kind, id };
    const selected = value.some((t) => t.kind === kind && t.id === id);
    onChange(
      multiple
        ? selected
          ? value.filter((t) => !(t.kind === kind && t.id === id))
          : [...value, target]
        : selected
          ? []
          : [target],
    );
  }
  return (
    <div className="form">
      <div className="form-grid">
        <Field label="Recipient type">
          <select
            value={kind}
            onChange={(e) => setKind(e.target.value as Kind)}
          >
            <option value="person">People</option>
            <option value="agent">Agents</option>
            <option value="service">Software services</option>
            {allowGroups && !multiple ? (
              <option value="group">Groups</option>
            ) : null}
            {allowInstances ? (
              <option value="instance">Exact running instance</option>
            ) : null}
          </select>
        </Field>
        <Field label="Find a recipient">
          <Search
            value={search}
            onChange={setSearch}
            placeholder="Search permitted recipients"
          />
        </Field>
      </div>
      {value.length ? (
        <div className="actions">
          {value.map((t) => (
            <Badge key={t.kind + t.id}>
              {t.kind} · {t.id.slice(0, 18)}{" "}
              <button
                type="button"
                onClick={() => onChange(value.filter((x) => x !== t))}
                aria-label={`Remove ${t.id}`}
              >
                ×
              </button>
            </Badge>
          ))}
        </div>
      ) : null}
      <div className="pick-list">
        <QueryState query={q} empty="No permitted recipients found">
          {q.items.map((item) => {
            const name = "name" in item ? item.name : item.id;
            return (
              <label className="pick-row" key={item.id}>
                <input
                  type={multiple ? "checkbox" : "radio"}
                  name="recipient"
                  checked={value.some(
                    (t) => t.kind === kind && t.id === item.id,
                  )}
                  onChange={() => toggle(item.id)}
                />
                <IdentityIcon id={item.id} kind={kind} label={name} />
                <span>
                  {name}
                  <br />
                  <small>
                    {"external_id" in item
                      ? item.external_id
                      : "agent_id" in item
                        ? item.agent_id
                        : item.id}
                  </small>
                </span>
                {"disabled" in item && item.disabled ? (
                  <Badge>Disabled</Badge>
                ) : null}
              </label>
            );
          })}
        </QueryState>
      </div>
      {kind === "instance" ? (
        <small>
          A message to an exact instance stays bound to that runtime. A message
          to an agent selects one eligible instance.
        </small>
      ) : null}
    </div>
  );
}
