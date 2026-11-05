import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../client";
import { useCollection, useOperation, useResource, useScope } from "../data";
import type { Group, Identity, Target } from "../contracts";
import { Button, ErrorView, Field, Form, Loading, Modal, Search } from "../ui";

export type SlackChannel = {
  address: string;
  name: string;
  restricted: boolean;
  allowed: Target[];
  version: number;
};
type ChannelPage = { channels: SlackChannel[]; next_cursor?: string };

export function SlackChannelPicker({
  connection,
  value,
  onChange,
}: {
  connection: string;
  value: string;
  onChange: (channel: SlackChannel) => void;
}) {
  const scope = useScope();
  const [search, setSearch] = useState("");
  const channels = useInfiniteQuery({
    queryKey: [scope, "slack-channels", connection],
    initialPageParam: "",
    queryFn: ({ pageParam, signal }) =>
      api<ChannelPage>(
        `/connections/${connection}/slack-channels?cursor=${encodeURIComponent(pageParam)}`,
        undefined,
        { signal },
      ),
    getNextPageParam: (page) => page.next_cursor || undefined,
    staleTime: 60_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const items = channels.data?.pages.flatMap((p) => p.channels) || [];
  return (
    <div className="slack-channel-picker">
      <Search
        value={search}
        onChange={setSearch}
        placeholder="Search Slack channels"
      />
      {channels.isPending ? <Loading label="Loading Slack channels" /> : null}
      <ErrorView error={channels.error} />
      <div
        className="slack-channel-options"
        role="radiogroup"
        aria-label="Slack channels"
      >
        {items
          .filter(
            (c, index) =>
              items.findIndex((x) => x.address === c.address) === index &&
              c.name.toLowerCase().includes(search.toLowerCase()),
          )
          .map((c) => (
            <label className="inline-check" key={c.address}>
              <input
                type="radio"
                name="slack-channel"
                checked={value === c.address}
                onChange={() => onChange(c)}
              />
              <span>{c.name}</span>
            </label>
          ))}
      </div>
      {!channels.isPending && !channels.error && !items.length ? (
        <p className="help">Invite the bot to a Slack channel, then refresh.</p>
      ) : null}
      <div className="actions">
        {channels.hasNextPage ? (
          <Button
            disabled={channels.isFetchingNextPage}
            onClick={() => void channels.fetchNextPage()}
          >
            Load more channels
          </Button>
        ) : null}
        <Button
          disabled={channels.isFetching}
          onClick={() => void channels.refetch()}
        >
          Refresh channels
        </Button>
      </div>
    </div>
  );
}

export function SlackChannels({ connection }: { connection: string }) {
  const q = useResource<SlackChannel[]>(`/connections/${connection}/channels`);
  const [editing, setEditing] = useState<SlackChannel | null>(null),
    [adding, setAdding] = useState(false);
  return (
    <section className="section slack-channels">
      <div className="actions">
        <h3>Slack channels</h3>
        <Button icon="plus" onClick={() => setAdding(true)}>
          Add channel
        </Button>
      </div>
      <ErrorView error={q.error} />
      {q.isPending ? (
        <Loading />
      ) : q.data?.length ? (
        <div className="slack-channel-list">
          {q.data.map((c) => (
            <div className="slack-channel-row" key={c.address}>
              <div>
                <strong>{c.name}</strong>
                <small>
                  {c.restricted
                    ? c.allowed.length
                      ? "Selected agents and roles"
                      : "No agents allowed"
                    : "All agents with contact permission"}
                </small>
              </div>
              <Button onClick={() => setEditing(c)}>Manage access</Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="help">
          Add the channels where your agents can contact people.
        </p>
      )}
      {adding || editing ? (
        <ChannelEditor
          key={editing?.address || "new"}
          connection={connection}
          channel={editing}
          onClose={() => {
            setAdding(false);
            setEditing(null);
          }}
        />
      ) : null}
    </section>
  );
}

function ChannelEditor({
  connection,
  channel,
  onClose,
}: {
  connection: string;
  channel: SlackChannel | null;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState<SlackChannel | null>(channel),
    [restricted, setRestricted] = useState(channel?.restricted || false),
    [allowed, setAllowed] = useState<Target[]>(channel?.allowed || []),
    [search, setSearch] = useState("");
  const agents = useCollection<Identity>("agents", search),
    roles = useCollection<Group>("groups", search, { purpose: "agent_role" }),
    op = useOperation();
  const saved = useResource<SlackChannel[]>(
    `/connections/${connection}/channels`,
  );
  const targets = [
    ...roles.items.map((r) => ({
      kind: "group" as const,
      id: r.id,
      name: r.name,
    })),
    ...agents.items
      .filter((a) => !a.disabled)
      .map((a) => ({ kind: "agent" as const, id: a.id, name: a.name })),
  ];
  const baseline =
    channel || saved.data?.find((c) => c.address === selected?.address);
  return (
    <Modal
      title={channel ? `Access to ${channel.name}` : "Add Slack channel"}
      onClose={onClose}
    >
      <Form
        onSubmit={() => {
          if (selected)
            void op
              .run(`/connections/${connection}/channels`, {
                ...selected,
                version: baseline?.version || 0,
                restricted,
                allowed,
              })
              .then((result) => {
                if (result) onClose();
              });
        }}
      >
        {!channel ? (
          <SlackChannelPicker
            connection={connection}
            value={selected?.address || ""}
            onChange={(c) => {
              const existing = saved.data?.find((x) => x.address === c.address);
              setSelected(existing || c);
              setRestricted(existing?.restricted || false);
              setAllowed(existing?.allowed || []);
            }}
          />
        ) : null}
        <Field label="Agent access">
          <select
            value={restricted ? "selected" : "all"}
            onChange={(e) => setRestricted(e.target.value === "selected")}
          >
            <option value="all">All agents with contact permission</option>
            <option value="selected">Selected agents and roles</option>
          </select>
        </Field>
        {restricted ? (
          <>
            <Search
              value={search}
              onChange={setSearch}
              placeholder="Search agents and roles"
            />
            <div
              className="slack-channel-options"
              role="group"
              aria-label="Allowed agents and roles"
            >
              {targets.map((t) => (
                <label className="inline-check" key={t.id}>
                  <input
                    type="checkbox"
                    checked={allowed.some(
                      (a) => a.kind === t.kind && a.id === t.id,
                    )}
                    onChange={(e) =>
                      setAllowed((a) =>
                        e.target.checked
                          ? [...a, { kind: t.kind, id: t.id }]
                          : a.filter((x) => x.kind !== t.kind || x.id !== t.id),
                      )
                    }
                  />
                  <span>
                    {t.name}
                    <small>{t.kind === "group" ? "Agent role" : "Agent"}</small>
                  </span>
                </label>
              ))}
            </div>
            <ErrorView error={agents.error || roles.error} />
            {agents.hasNextPage ? (
              <Button onClick={() => void agents.fetchNextPage()}>
                Load more agents
              </Button>
            ) : null}
            {roles.hasNextPage ? (
              <Button onClick={() => void roles.fetchNextPage()}>
                Load more roles
              </Button>
            ) : null}
            <p className="help">
              Only these agents and role members can send here. Contact
              permissions still apply.
            </p>
          </>
        ) : null}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={!selected || op.pending}>
            Save channel
          </Button>
        </div>
      </Form>
    </Modal>
  );
}

export function SlackDestination({
  connection,
  address,
  onChange,
}: {
  connection: string;
  address: string;
  onChange: (address: string) => void;
}) {
  const channels = useResource<SlackChannel[]>(
    `/connections/${connection}/channels`,
  );
  const [mode, setMode] = useState(
    address.startsWith("C") || address.startsWith("G") ? "channel" : "dm",
  );
  return (
    <>
      <Field label="Slack destination">
        <select
          value={mode}
          onChange={(e) => {
            setMode(e.target.value);
            onChange("");
          }}
        >
          <option value="dm">Direct message</option>
          <option value="channel">Slack channel</option>
        </select>
      </Field>
      {mode === "channel" ? (
        <Field label="Channel">
          <select
            aria-label="Channel"
            required
            value={address}
            onChange={(e) => onChange(e.target.value)}
          >
            <option value="">Choose a channel</option>
            {address && !channels.data?.some((c) => c.address === address) ? (
              <option value={address}>{address}</option>
            ) : null}
            {channels.data?.map((c) => (
              <option key={c.address} value={c.address}>
                {c.name}
              </option>
            ))}
          </select>
          <p className="help">
            Manage channels and agent access in the Slack connection. Everyone
            in the Slack channel can see messages posted there.
          </p>
          <ErrorView error={channels.error} />
        </Field>
      ) : (
        <Field label="Slack DM channel ID">
          <input
            required
            value={address}
            onChange={(e) => onChange(e.target.value)}
            placeholder="D…"
          />
        </Field>
      )}
    </>
  );
}
