import { Copy, Field } from "./ui";
import { WorkerSetup } from "./WorkerSetup";
import {
  environments,
  setupPrompt,
} from "../../sdk/typescript/src/runtime/catalog";
export { environments };

export function EnvironmentPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const selected = environments.find((e) => e.id === value);
  return (
    <Field label="Your environment">
      <select
        aria-label="Your environment"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        required
      >
        <option value="" disabled>
          Choose your existing setup
        </option>
        {[...new Set(environments.map((e) => e.group))].map((group) => (
          <optgroup label={group} key={group}>
            {environments
              .filter((e) => e.group === group)
              .map((e) => (
                <option key={e.id} value={e.id}>
                  {e.label}
                </option>
              ))}
          </optgroup>
        ))}
      </select>
      {selected ? <span className="help">{selected.note}</span> : null}
    </Field>
  );
}
export function BusyPolicy({
  environment,
  value,
  onChange,
}: {
  environment: string;
  value: "steer" | "queue";
  onChange: (value: "steer" | "queue") => void;
}) {
  const selected = environments.find((e) => e.id === environment);
  if (!selected?.steer) return null;
  return (
    <fieldset className="setup-policy">
      <legend>When the Agent is busy</legend>
      <div>
        {(["steer", "queue"] as const).map((mode) => (
          <label key={mode}>
            <input
              type="radio"
              name="busy-policy"
              value={mode}
              checked={value === mode}
              onChange={() => onChange(mode)}
            />
            <span>{mode === "steer" ? "Steer" : "Queue"}</span>
          </label>
        ))}
      </div>
      <p className="help">
        {value === "steer"
          ? "Handle new input at the next model step. Running tools finish normally."
          : "Handle new messages after current work. Answers and decisions still resume their request."}
      </p>
    </fieldset>
  );
}
function Code({ label, value }: { label: string; value: string }) {
  return (
    <div className="setup-code">
      <div>
        <span>{label}</span>
        <Copy value={value} />
      </div>
      <pre className="code-example">{value}</pre>
    </div>
  );
}
export function AgentSetup({
  method,
  onMethodChange,
  base,
  token,
  busyMode,
  onBusyModeChange,
  instanceId,
}: {
  method: string;
  onMethodChange: (method: string) => void;
  base: string;
  token: string;
  busyMode: "steer" | "queue";
  onBusyModeChange: (mode: "steer" | "queue") => void;
  instanceId: string;
}) {
  const environment = environments.find((e) => e.id === method);
  if (!environment) return null;
  const advanced = environment.group === "Advanced";
  const mode = environment.steer ? busyMode : "queue";
  const prompt = advanced ? "" : setupPrompt(method, base, mode, instanceId);
  return (
    <div className="agent-setup-guide">
      <details className="setup-environment">
        <summary>
          <strong>{environment.label}</strong>
          <span>
            {advanced ? "Tools / worker" : mode === "steer" ? "Steer" : "Queue"}
          </span>
          <span>Change</span>
        </summary>
        <div>
          <EnvironmentPicker value={method} onChange={onMethodChange} />
          <BusyPolicy
            environment={method}
            value={mode}
            onChange={onBusyModeChange}
          />
        </div>
      </details>
      {advanced ? (
        <WorkerSetup method={method} base={base} token={token} />
      ) : (
        <>
          <section className="setup-step">
            <header>
              <span>1</span>
              <h3>Save your connection token</h3>
            </header>
            <p>
              Save it in a private file or your secret manager. This token
              expires after 24 hours.
            </p>
            <div className="setup-token">
              <span>
                <code>worker-token</code>
                <small>
                  Shown once. Keep it out of source code and prompts.
                </small>
              </span>
              <Copy value={token} label="Copy token" />
            </div>
            <div className="setup-command">
              <code>chmod 600 /absolute/path/worker-token</code>
              <Copy value="chmod 600 /absolute/path/worker-token" />
            </div>
          </section>
          <section className="setup-step">
            <header>
              <span>2</span>
              <h3>Connect {environment.label}</h3>
            </header>
            <p>
              Copy the setup instruction into your coding assistant in the
              Beepgrid checkout. It uses your existing model, tools and runtime.
            </p>
            <div className="setup-instruction">
              <div>
                <strong>{environment.label}</strong>
                <span>{mode === "steer" ? "Steer" : "Queue"}</span>
              </div>
              <Copy value={prompt} label="Copy setup instruction" />
            </div>
            <details className="setup-reference">
              <summary>Set up by hand</summary>
              <p>
                Use <code>{environment.binding}</code> from the source SDK. The
                guide covers session routing, checkpoints and the wake handler.
              </p>
              <a
                className="button"
                href={`https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#${environment.guide}`}
                target="_blank"
                rel="noreferrer"
              >
                Open integration guide
              </a>
              <Code
                label="Generate the instruction locally"
                value={`node sdk/typescript/bin/setup.mjs ${method} \\\n  --url '${base.replaceAll("'", "'\\''")}' --instance '${instanceId}' --mode ${mode}`}
              />
            </details>
            <p className="help">
              The check below verifies replies from this connection. Your
              runtime's supported input boundary determines when a busy Agent
              can respond.
            </p>
          </section>
        </>
      )}
    </div>
  );
}
