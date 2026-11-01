import { Copy, Field } from "./ui";
import { WorkerSetup } from "./WorkerSetup";
import { ConnectionToken } from "./ConnectionToken";
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
          ? "Use new input during ongoing work, at a supported input boundary."
          : "Handle new messages after current work. Answers and decisions still resume their request."}
      </p>
    </fieldset>
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
      <div className="setup-environment">
        <EnvironmentPicker value={method} onChange={onMethodChange} />
        <BusyPolicy
          environment={method}
          value={mode}
          onChange={onBusyModeChange}
        />
      </div>
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
              Shown once. Save it outside your source code or in your secret
              manager. This token expires after 24 hours.
            </p>
            <ConnectionToken token={token} />
          </section>
          <section className="setup-step">
            <header>
              <span>2</span>
              <h3>Instructions for your AI</h3>
            </header>
            <p>
              Paste this into your coding assistant in your Agent’s project. It
              includes the guide and asks for your token file’s path, not the
              token.
            </p>
            <div className="setup-instruction">
              <div>
                <strong>{environment.label}</strong>
                <span>{mode === "steer" ? "Steer" : "Queue"}</span>
              </div>
              <Copy value={prompt} label="Copy setup instruction" />
            </div>
            <details className="setup-reference">
              <summary className="button">Set up by hand</summary>
              <a
                className="button"
                href={`https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#manual-setup`}
                target="_blank"
                rel="noreferrer"
              >
                Open integration guide
              </a>
            </details>
          </section>
        </>
      )}
    </div>
  );
}
