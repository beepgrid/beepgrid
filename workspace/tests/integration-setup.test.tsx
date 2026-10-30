import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  ConnectionDetail,
  ConnectionForm,
  DestinationForm,
  type Provider,
} from "../src/pages/Integrations";
import { harness, json, requestBody, me } from "./fixtures";
const connection = {
  id: "cn_telegram",
  kind: "telegram",
  name: "Team bot",
  enabled: true,
  secret_configured: true,
  version: 3,
  config: { bot_id: "123456" },
};
const provider: Provider = {
  kind: "telegram",
  builtin: false,
  inbound_messages: true,
  native_approvals: true,
  web_continuation: true,
  attachments: false,
  fields: [
    { name: "bot_id", required: true, secret: false },
    { name: "bot_token", required: true, secret: true },
    { name: "webhook_secret", required: true, secret: true },
  ],
};
afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
it("serializes Telegram credentials as the provider contract without placing them in config", async () => {
  const fn = vi.fn(async (_path: string, _init?: RequestInit) =>
    json(connection),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(<ConnectionForm provider={provider} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  fireEvent.change(screen.getByLabelText("Bot ID"), {
    target: { value: "123456" },
  });
  fireEvent.change(screen.getByLabelText("Bot token"), {
    target: { value: "123456:test-bot-token-fixture" },
  });
  fireEvent.change(screen.getByLabelText("Webhook secret"), {
    target: { value: "test-webhook-secret-fixture" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save connection" }));
  await waitFor(() => expect(fn).toHaveBeenCalled());
  const body = requestBody(fn.mock.calls[0][1]);
  expect(body.config).toEqual({ bot_id: "123456" });
  expect(JSON.parse(body.secret)).toEqual({
    bot_token: "123456:test-bot-token-fixture",
    webhook_secret: "test-webhook-secret-fixture",
  });
  expect(sessionStorage.length).toBe(0);
});
it("shows setup and observed evidence, with an explicit test confirmation", async () => {
  let queued = false;
  const fn = vi.fn(async (path: string) =>
    path.endsWith("/capabilities")
      ? json({ operations: { manage: true } })
      : path.endsWith("/setup")
        ? json({
            steps: ["Register the dedicated bot webhook."],
            callbacks: {
              inbound:
                "https://app.example.test/integrations/telegram/cn_telegram",
            },
          })
        : path.endsWith("/diagnostics")
          ? json({
              state: queued ? "verifying" : "configured",
              qualification: queued
                ? "provider_acceptance_observed"
                : "not_verified",
              configuration_valid: true,
              issues: [],
              ...(queued
                ? {
                    last_test: {
                      state: "provider_accepted",
                      conversation_id: "con_test",
                      detail: "",
                      inbound_reply: false,
                    },
                  }
                : {}),
            })
          : path.endsWith("/test")
            ? ((queued = true), json({ id: "del_test" }))
            : json({
                items: [
                  {
                    id: "dst_one",
                    address: "12345",
                    connection_id: connection.id,
                  },
                ],
              }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <ConnectionDetail
      connection={connection}
      provider={provider}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  await screen.findByText("Configured");
  expect(
    screen.queryByText("Connected", { exact: true }),
  ).not.toBeInTheDocument();
  expect(await screen.findByLabelText("Inbound webhook URL")).toHaveValue(
    "https://app.example.test/integrations/telegram/cn_telegram",
  );
  expect(fn.mock.calls.some(([p]) => p.endsWith("/test"))).toBe(false);
  fireEvent.change(await screen.findByLabelText("Test recipient"), {
    target: { value: "dst_one" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Test connection" }));
  expect(fn.mock.calls.some(([p]) => p.endsWith("/test"))).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Send test message" }));
  await screen.findByText(
    "The provider accepted the test. Reply to that message to verify the return path.",
  );
  expect(
    screen.queryByText("Connected", { exact: true }),
  ).not.toBeInTheDocument();
});
it("does not read setup or diagnostic details without management permission", async () => {
  const fn = vi.fn(async (path: string) =>
    path.endsWith("/capabilities")
      ? json({ operations: {} })
      : json({ items: [] }),
  );
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <ConnectionDetail
      connection={connection}
      provider={provider}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  await screen.findByText("Telegram", { exact: true });
  expect(
    fn.mock.calls.some(
      ([p]) => p.endsWith("/setup") || p.endsWith("/diagnostics"),
    ),
  ).toBe(false);
  expect(
    screen.queryByRole("button", { name: "Add recipient" }),
  ).not.toBeInTheDocument();
});

it("links the selected person to the installation without granting contact access", async () => {
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/capabilities"))
      return json({ operations: { manage: true, link_account: true } });
    if (path.endsWith("/telegram-account")) return json({ linked: true });
    const body = requestBody(init);
    return json({
      items:
        body.collection === "people"
          ? [
              {
                ...me.person,
                metadata: {
                  telegram_user_id: "99",
                  telegram_bot_id: "different-bot",
                },
              },
            ]
          : body.collection === "connections"
            ? [connection]
            : [],
    });
  });
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <DestinationForm
      connection={connection}
      destination={{
        id: "dst_one",
        connection_id: connection.id,
        principal_id: me.person.id,
        address: "12345",
        mode: "primary",
        priority: 0,
        version: 2,
      }}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  await screen.findByText(
    "Link this person’s account before saving their destination.",
  );
  fireEvent.click(screen.getByRole("button", { name: "Link account" }));
  fireEvent.change(screen.getByLabelText("Telegram user ID"), {
    target: { value: "54321" },
  });
  const dialog = screen.getByRole("dialog", {
    name: "Link Taylor to Telegram",
  });
  fireEvent.submit(dialog.querySelector("form")!);
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/telegram-account"))).toBe(
      true,
    ),
  );
  const call = fn.mock.calls.find(([p]) => p.endsWith("/telegram-account"))!;
  expect(requestBody(call[1])).toEqual({ bot_id: "123456", user_id: "54321" });
  expect(call[0]).toContain("/people/per_self/");
  expect(fn.mock.calls.some(([p]) => p.endsWith("/contact-permissions"))).toBe(
    false,
  );
});

it("offers people as recipients for person-linked messaging providers", async () => {
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/capabilities")) return json({ operations: {} });
    const body = requestBody(init);
    return json({
      items:
        body.collection === "connections"
          ? [connection]
          : body.collection === "people"
            ? [me.person]
            : [],
    });
  });
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(<DestinationForm connection={connection} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  await screen.findByRole("button", { name: /^People$/ });
  expect(
    screen.queryByRole("button", { name: /^Agents$/ }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /^Software services$/ }),
  ).not.toBeInTheDocument();
  await screen.findByText("Taylor");
});

it("shows Discord registration without substituting acceptance for a working reply", async () => {
  const discord = {
    ...connection,
    id: "cn_discord",
    name: "Release bot",
    kind: "discord",
    config: { application_id: "12345", public_key: "fixture-public-key" },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path.endsWith("/capabilities")
        ? json({ operations: { manage: true } })
        : path.endsWith("/setup")
          ? json({
              callbacks: {
                interactions:
                  "https://app.example.test/integrations/discord/cn_discord",
              },
              steps: ["Use a dedicated bot."],
              registration: {
                installation_url:
                  "https://discord.com/oauth2/authorize?client_id=12345",
                command_endpoint:
                  "https://discord.com/api/v10/applications/12345/commands",
                command: { name: "beepgrid" },
              },
            })
          : path.endsWith("/diagnostics")
            ? json({
                state: "configured",
                qualification: "not_verified",
                issues: [],
              })
            : json({ items: [] }),
    ),
  );
  const { Wrapper } = harness();
  render(<ConnectionDetail connection={discord} onClose={vi.fn()} />, {
    wrapper: Wrapper,
  });
  expect(await screen.findByLabelText("Interactions endpoint URL")).toHaveValue(
    "https://app.example.test/integrations/discord/cn_discord",
  );
  expect(
    screen.getByRole("link", { name: /Install dedicated bot/ }),
  ).toHaveAttribute(
    "href",
    "https://discord.com/oauth2/authorize?client_id=12345",
  );
  expect(
    screen.queryByText("Connected", { exact: true }),
  ).not.toBeInTheDocument();
});

it("links a Discord Person to the exact application and reuses their verified destination ID", async () => {
  const discord = {
    ...connection,
    id: "cn_discord",
    kind: "discord",
    config: { application_id: "12345" },
  };
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    if (path.endsWith("/capabilities"))
      return json({ operations: { manage: true, link_account: true } });
    if (path.endsWith("/discord-account")) return json({ linked: true });
    const body = requestBody(init);
    return json({
      items:
        body.collection === "people"
          ? [
              {
                ...me.person,
                metadata: {
                  discord_user_id: "99",
                  discord_application_id: "different-app",
                },
              },
            ]
          : body.collection === "connections"
            ? [discord]
            : [],
    });
  });
  vi.stubGlobal("fetch", fn);
  const { Wrapper } = harness();
  render(
    <DestinationForm
      connection={discord}
      destination={{
        id: "dst_d",
        connection_id: discord.id,
        principal_id: me.person.id,
        address: "99",
        mode: "primary",
        priority: 0,
        version: 1,
      }}
      onClose={vi.fn()}
    />,
    { wrapper: Wrapper },
  );
  fireEvent.click(await screen.findByRole("button", { name: "Link account" }));
  fireEvent.change(screen.getByLabelText("Discord user ID"), {
    target: { value: "56789" },
  });
  fireEvent.submit(
    screen
      .getByRole("dialog", { name: "Link Taylor to Discord" })
      .querySelector("form")!,
  );
  await waitFor(() =>
    expect(fn.mock.calls.some(([p]) => p.endsWith("/discord-account"))).toBe(
      true,
    ),
  );
  const call = fn.mock.calls.find(([p]) => p.endsWith("/discord-account"))!;
  expect(requestBody(call[1])).toEqual({
    application_id: "12345",
    user_id: "56789",
  });
  expect(fn.mock.calls.some(([p]) => p.endsWith("/contact-permissions"))).toBe(
    false,
  );
});
