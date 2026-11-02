import { useState } from "react";
import { Copy } from "./ui";

export function ConnectionToken({ token }: { token: string }) {
  return <TokenField key={token} token={token} />;
}
function TokenField({ token }: { token: string }) {
  const [shown, setShown] = useState(false);
  return (
    <div className="setup-token">
      <input
        aria-label="Connection token"
        type={shown ? "text" : "password"}
        value={token}
        readOnly
        autoComplete="off"
        spellCheck={false}
      />
      <button
        type="button"
        className="button"
        aria-pressed={shown}
        onClick={() => setShown(!shown)}
      >
        {shown ? "Hide" : "Show"}
      </button>
      <Copy value={token} label="Copy token" />
    </div>
  );
}
