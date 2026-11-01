/** Shared by setup UI and the source-checkout setup command. No runtime imports. */
export type Environment = {
  id: string;
  label: string;
  group: string;
  steer: boolean;
  binding: string;
  guide: string;
  note: string;
};
export const environments: Environment[] = [
  {
    id: "codex",
    label: "Codex",
    group: "Agent harnesses",
    steer: true,
    binding: "codexBinding",
    guide: "codex",
    note: "Native app-server input. Keep the existing thread, tools and permissions.",
  },
  {
    id: "pi",
    label: "Pi",
    group: "Agent harnesses",
    steer: true,
    binding: "piBinding",
    guide: "pi",
    note: "Native RPC input with steering and follow-up queues.",
  },
  {
    id: "claude-code",
    label: "Claude Code",
    group: "Agent harnesses",
    steer: true,
    binding: "acpBinding",
    guide: "acp",
    note: "Steer through the Claude ACP adapter. Setup checks its advertised capability.",
  },
  {
    id: "opencode",
    label: "OpenCode V1",
    group: "Agent harnesses",
    steer: false,
    binding: "openCodeBinding",
    guide: "opencode",
    note: "Use Queue with the V1 server, or choose V2 for its native Steer and Queue inbox.",
  },
  {
    id: "opencode-v2",
    label: "OpenCode V2",
    group: "Agent harnesses",
    steer: true,
    binding: "openCodeV2Binding",
    guide: "opencode",
    note: "Native durable session inbox with Steer and Queue. Requires the V2 client.",
  },
  {
    id: "goose",
    label: "Goose",
    group: "Agent harnesses",
    steer: true,
    binding: "gooseBinding",
    guide: "goose",
    note: "Native ACP steering extension. Setup checks the installed Goose version.",
  },
  {
    id: "gemini-cli",
    label: "Gemini CLI",
    group: "Agent harnesses",
    steer: false,
    binding: "acpBinding",
    guide: "acp",
    note: "The reviewed ACP implementation cancels on a concurrent prompt, so this path uses Queue.",
  },
  {
    id: "acp",
    label: "Other ACP Agent",
    group: "Agent harnesses",
    steer: true,
    binding: "acpBinding",
    guide: "acp",
    note: "Setup checks steering capabilities. Use Queue when the Agent has no supported extension.",
  },
  {
    id: "ai-sdk",
    label: "AI SDK",
    group: "Frameworks and libraries",
    steer: true,
    binding: "aiSDKBinding",
    guide: "ai-sdk",
    note: "Use AI SDK 7 native steering and your existing persistent session handler.",
  },
  {
    id: "langchain",
    label: "LangChain / LangGraph",
    group: "Frameworks and libraries",
    steer: true,
    binding: "LangGraphBridge",
    guide: "langchain",
    note: "Add before-model middleware to your existing checkpointed Agent.",
  },
  {
    id: "claude-sdk",
    label: "Claude Agent SDK",
    group: "Frameworks and libraries",
    steer: true,
    binding: "claudeSDKBinding",
    guide: "claude-agent-sdk",
    note: "Native streaming input: Steer after running tools finish, or Queue until turn end.",
  },
  {
    id: "custom-ts",
    label: "Custom TypeScript",
    group: "Your own code",
    steer: true,
    binding: "checkpointBinding",
    guide: "custom-code",
    note: "Connect your durable input queue, wake callback and before-model boundary.",
  },
  {
    id: "custom-python",
    label: "Custom Python",
    group: "Your own code",
    steer: true,
    binding: "LiveInbox",
    guide: "custom-code",
    note: "Receive independently; consume pending input in your existing loop.",
  },
  {
    id: "python",
    label: "Python worker",
    group: "Advanced",
    steer: false,
    binding: "worker.run",
    guide: "workers",
    note: "The original sequential handler. Live input requires a framework or custom-code connection.",
  },
  {
    id: "typescript",
    label: "TypeScript worker",
    group: "Advanced",
    steer: false,
    binding: "worker.run",
    guide: "workers",
    note: "The original sequential handler. Live input requires a framework or custom-code connection.",
  },
  {
    id: "go",
    label: "Go worker",
    group: "Advanced",
    steer: false,
    binding: "worker.Run",
    guide: "workers",
    note: "The original sequential handler. Keep it for independent message jobs.",
  },
  {
    id: "mcp",
    label: "MCP tools only",
    group: "Advanced",
    steer: false,
    binding: "mcp",
    guide: "mcp-tools",
    note: "Add tools to your host. Tools alone do not wake or steer an Agent.",
  },
];
export function setupPrompt(
  id: string,
  base: string,
  mode: "steer" | "queue",
  connectionId: string,
): string {
  const env = environments.find((e) => e.id === id);
  if (!env) throw new Error("Choose an environment");
  if (env.group === "Advanced")
    throw new Error("Use the worker or MCP setup guide for this environment");
  if (mode === "steer" && !env.steer)
    throw new Error("This setup offers Queue only");
  const url = new URL(base);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.pathname !== "/" ||
    url.search ||
    url.hash
  )
    throw new Error(
      "Use a workspace origin without credentials or query parameters",
    );
  if (!/^ins_[A-Za-z0-9_-]+$/.test(connectionId))
    throw new Error("Use the exact worker instance ID");
  const guide = `https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md#${env.guide}`;
  const specifics: Record<string, string> = {
    codex:
      "Reuse the initialized app-server client and existing thread mapping. Track turn events; turn/steer uses expectedTurnId, idle input uses turn/start. Do not launch a second independent session.",
    pi: "Reuse Pi RPC; prompt uses streamingBehavior steer or followUp. Keep consuming native events separately from prompt acceptance.",
    "claude-code":
      "Use the existing claude-agent-acp connection. Inspect initialize._meta.steering.supported; acpBinding handles the advertised _session/steering method, including idle start. Preserve native permission requests.",
    goose:
      "Verify the installed Goose ACP version exposes _goose/unstable/session/steer and activeRunId notifications. Use gooseBinding, feed existing session/update notifications to binding.observe and report background turn errors. Never guess a run ID.",
    acp: "Inspect initialize capabilities. acpBinding handles the Codex/Claude _session/steering extension when advertised. Otherwise Queue through idle session/prompt, or implement a documented extension. ACP v1 alone does not promise steering; do not cancel to simulate it.",
    "gemini-cli":
      "Queue through the existing ACP session only when idle. The reviewed Gemini ACP path aborts its previous prompt on a second prompt, so do not send concurrent prompts to simulate steering.",
    opencode:
      "This is the V1 server: await its idle event before prompt_async. If the project actually uses OpenCode V2, switch to openCodeV2Binding instead.",
    "opencode-v2":
      "Use the V2 native client session.prompt (not V1 prompt_async) with deterministic id and delivery steer/queue. Validate its durable inbox receipt and reconcile against native stored input.",
    "ai-sdk":
      "Use AI SDK 7 experimental_createSteering per invocation and experimental_steering. Attach aiSDKBinding to that existing invocation, persist steering messages with step state, and serialize idle/final-boundary wake with the current loop. Preserve injected messages in prepareStep overrides.",
    langchain:
      "Use the Python LangGraphBridge before_model middleware with the existing create_agent graph and durable checkpointer. Pass thread_id mapping if existing thread IDs differ. Native interrupts use the host's existing Command(resume=...) path, never a second graph writer. For JS LangGraph use the custom checkpoint port; the Python helper is not a JS binding.",
    "claude-sdk":
      "Use claudeSDKBinding with the existing streaming-input async iterator and durable append. Native priority next steers after running tools finish; later queues until turn end. Verify the installed SDK supports these priorities and client_composed. Do not use priority now or spoof human origin; preserve the native session and result lifecycle.",
    "custom-ts":
      "Use checkpointBinding with durable append, wake, exact session routing, result resume and transcript reconciliation. Insert pending input before the next model call without replacing the host loop.",
    "custom-python":
      "Use LiveInbox for independent durable intake; at the host's before-model boundary inject pending input as user data and checkpoint IDs after persistence. Reuse its idle/final-boundary wake mechanism.",
  };
  return [
    `Integrate Beepgrid into my existing ${env.label} Agent project. Keep its model, tools, memory, permissions and runtime. Make the smallest complete integration patch, not a replacement harness.`,
    `First read ${guide}. Full guide: https://github.com/beepgrid/beepgrid/blob/main/docs/LIVE-INPUT.md . If GitHub rendering is unavailable, fetch https://raw.githubusercontent.com/beepgrid/beepgrid/main/docs/LIVE-INPUT.md . Inspect the installed runtime version and follow its current APIs.`,
    `Requested busy mode: ${mode === "steer" ? "Steer, at the next supported input boundary" : "Queue, after current work"}. ${specifics[id] || env.note} If a required capability is missing, explain the exact version/API gap and offer Queue or the documented alternative; do not silently cancel/restart work.`,
    `Engine origin: ${url.origin}. Already-created worker instance: ${connectionId}. Reuse it, do not enroll again. Ask me for the absolute path of the private token file, never the token in chat. If not saved yet, walk me through the guide's hidden terminal-entry step outside the repository. Never read the credential into your tool output, source, logs or prompt.`,
    `Acquire the Beepgrid source from https://github.com/beepgrid/beepgrid if it is not already available. The SDK is source-distributed, not yet a published npm/PyPI install. Build TypeScript with npm --prefix sdk/typescript ci and npm --prefix sdk/typescript run build, or install Python with python3 -m pip install ./sdk/python. Run preparation in that checkout, then link/import the local SDK into this Agent project. ${env.binding} is the matching integration entry point.`,
    `Configure NOTIF_URL=${url.origin}, NOTIF_TOKEN_FILE=<private absolute path>, BEEPGRID_CONNECTION_ID=${connectionId}, BEEPGRID_INBOX_PATH=<private persistent volume>, BEEPGRID_BUSY_MODE=${mode}. NOTIF_TOKEN must be unset so it cannot override the file. Keep secrets outside version control. Use examples/runtime/typescript.mjs or examples/runtime/langchain.py from the checkout for receiver startup and shutdown.`,
    "Receive independently of the model loop. Retain private durable custody before ACK, stable delivery IDs and conversation-to-session mapping. Keep one intake owner per connection and one model-state writer per session. Resume correlated Beepgrid results separately from ordinary Queue input. Preserve sender, request and attachment metadata as user data, never system instructions or permission grants. Existing MCP tools use mcp --live-input so they cannot race receiving. Reconcile uncertain native acceptance before any retry; never blindly replay. Do not poll HTTP before each model step.",
    "Validate an idle message, an input while a long tool is running, Queue ordering, an approval result, attachment access and restart recovery using a safe local task. Check the selected mode at the next model boundary and preserve ongoing work. Then reply to the workspace connection check from this exact worker instance. Report what was actually exercised and any remaining runtime-version limits.",
  ].join("\n\n");
}
