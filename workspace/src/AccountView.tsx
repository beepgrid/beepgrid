import { Badge, DateValue, Details } from "./ui";
export type Account = {
  deployment: string;
  applied: {
    company: string;
    environment: string;
    suspended: boolean;
    recovery_hold: boolean;
    access_until?: string;
    control_version: number;
    metered: boolean;
    budget?: {
      version: number;
      messages: number;
      requests: number;
      upload_bytes: number;
    };
    usage: { period: string; metric: string; units: number }[];
    source: string;
  };
  managed?: {
    id: string;
    company: string;
    region: string;
    state: string;
    desired: string;
    version: number;
    quota_revision: number;
    quota_policy?: {
      version: number;
      messages: number;
      requests: number;
      upload_bytes: number;
    };
    access: {
      enabled: boolean;
      reason: string;
      status: string;
      valid_until: number;
      revision: number;
    };
  };
};
export function AccountView({ account: a }: { account: Account }) {
  const p = a.applied,
    m = a.managed,
    expired = !!p.access_until && Date.parse(p.access_until) <= Date.now();
  return (
    <>
      <section className="section">
        <h2>{p.company}</h2>
        <dl className="detail-list">
          <dt>Deployment</dt>
          <dd>{a.deployment.replaceAll("_", " ")}</dd>
          <dt>Environment</dt>
          <dd>{p.environment}</dd>
          <dt>Applied access</dt>
          <dd>
            <Badge
              tone={p.suspended || p.recovery_hold || expired ? "warn" : "good"}
            >
              {p.recovery_hold
                ? "Recovery hold"
                : p.suspended
                  ? "Suspended"
                  : expired
                    ? "Expired"
                    : "Available"}
            </Badge>
          </dd>
          <dt>Access deadline</dt>
          <dd>
            {p.access_until ? (
              <DateValue value={p.access_until} />
            ) : (
              "No applied deadline"
            )}
          </dd>
          <dt>Control revision</dt>
          <dd>{p.control_version}</dd>
        </dl>
      </section>
      {m ? (
        <section className="section">
          <h2>Managed account</h2>
          <dl className="detail-list">
            <dt>Organization</dt>
            <dd>
              {m.company} · <code>{m.id}</code>
            </dd>
            <dt>Region</dt>
            <dd>{m.region}</dd>
            <dt>Provisioning state</dt>
            <dd>{m.state}</dd>
            <dt>Requested state</dt>
            <dd>{m.desired}</dd>
            <dt>Subscription access</dt>
            <dd>
              <Badge>{m.access.status}</Badge> ·{" "}
              {m.access.enabled ? "Enabled" : "Disabled"}
              <p>{m.access.reason}</p>
            </dd>
            <dt>Validity</dt>
            <dd>
              {m.access.valid_until ? (
                <DateValue
                  value={new Date(m.access.valid_until * 1000).toISOString()}
                />
              ) : (
                "No deadline reported"
              )}
            </dd>
            <dt>Access revision</dt>
            <dd>
              {m.access.revision} requested · {p.control_version} applied
            </dd>
            <dt>Quota revision</dt>
            <dd>
              {m.quota_revision} requested ·{" "}
              {p.budget?.version ?? "Unavailable"} applied
            </dd>
          </dl>
          <small>
            Requested policy and runtime-applied policy can differ while
            reconciliation is pending.
          </small>
        </section>
      ) : null}
      <section className="section">
        <h2>Monthly accepted usage</h2>
        {p.metered ? (
          <>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Metric</th>
                    <th>UTC period</th>
                    <th>Accepted units</th>
                    <th>Applied monthly limit</th>
                  </tr>
                </thead>
                <tbody>
                  {p.usage.map((u) => {
                    const key = u.metric as
                      "messages" | "requests" | "upload_bytes";
                    const limit = p.budget?.[key];
                    return (
                      <tr key={u.period + u.metric}>
                        <td>{u.metric.replaceAll("_", " ")}</td>
                        <td>{new Date(u.period).toISOString().slice(0, 7)}</td>
                        <td>{u.units.toLocaleString()}</td>
                        <td>
                          {limit === undefined
                            ? "Unavailable"
                            : limit === -1
                              ? "Unlimited"
                              : limit === 0
                                ? "No new work"
                                : limit.toLocaleString()}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <small>
              Budget revision {p.budget?.version}. Source: {p.source}.
            </small>
          </>
        ) : (
          <p>Metered usage is not configured for this deployment.</p>
        )}
        <p className="help">
          Usage counts canonical recipient messages, request records and
          accepted upload bytes in UTC months. Group fan-out counts per
          recipient; canonical retries do not consume work twice. Upload intake
          differs from retained storage. Usage is not a payment receipt or
          provider invoice.
        </p>
      </section>
      <Details value={a} label="Exact account response and sources" />
    </>
  );
}
