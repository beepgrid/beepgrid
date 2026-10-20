// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { afterEach, it, expect, vi } from "vitest";
import { Client } from "@notif/client";
import { NotifRequest, NotifConversation } from "./index";
afterEach(cleanup);
it("clarification preserves a pending request and an authorized answer records its version", async () => {
  const respond = vi.fn(async () => ({
    ...q,
    state: "resolved",
    response: { outcome: "answered", answer: "Brief", version: 3 },
  }));
  const q = {
    id: "req_1",
    conversation_id: "con_1",
    kind: "question",
    title: "Which format?",
    body: "Choose one",
    choices: ["Brief", "Detailed"],
    state: "open",
    version: 3,
  };
  const client = {
    getRequest: vi.fn(async () => q),
    respond,
  } as unknown as Client;
  const open = vi.fn();
  render(<NotifRequest client={client} id="req_1" onConversation={open} />);
  fireEvent.click(
    await screen.findByRole("button", { name: "Ask for clarification" }),
  );
  expect(open).toHaveBeenCalledWith("con_1");
  expect(respond).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("radio", { name: "Brief" }));
  fireEvent.click(screen.getByRole("button", { name: "Send answer" }));
  await waitFor(() =>
    expect(respond).toHaveBeenCalledWith("req_1", {
      outcome: "answered",
      answer: "Brief",
      version: 3,
    }),
  );
  expect(await screen.findByText("answered: Brief")).toBeVisible();
});
it("a denied response preserves the question and shows the server error", async () => {
  const client = {
    getRequest: async () => ({
      id: "q",
      kind: "approval",
      title: "Deploy?",
      state: "open",
      version: 1,
    }),
    respond: async () => {
      throw new Error("This operation is not permitted.");
    },
  } as unknown as Client;
  render(<NotifRequest client={client} id="q" />);
  fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("not permitted");
  expect(screen.getByRole("button", { name: "Approve" })).toBeEnabled();
});

it("shows the exact approved action rather than only its title", async () => {
  const client = { getRequest: async () => ({id:"exact",kind:"approval",title:"Deploy?",state:"open",version:1,expires_at:"2026-12-01T00:00:00Z",action:{name:"deploy",executor_id:"builder-1",resource:"production/app",resource_version:"sha:123",arguments:{region:"eu"},operation_key:"release-123"}}) } as unknown as Client;
  render(<NotifRequest client={client} id="exact"/>);
  expect(await screen.findByText("sha:123")).toBeVisible();
  expect(screen.getByText("builder-1")).toBeVisible();
  expect(screen.getByText(/"region": "eu"/)).toBeVisible();
});
it("removes sensitive request content when a subsequent read loses permission", async () => {
  const getRequest=vi.fn().mockResolvedValueOnce({id:"secret",kind:"question",title:"Private request",state:"open",version:1}).mockRejectedValue(new Error("Access revoked"));
  render(<NotifRequest client={{getRequest} as unknown as Client} id="secret"/>);
  expect(await screen.findByText("Private request")).toBeVisible();
  expect(await screen.findByRole("alert",{}, {timeout:2500})).toHaveTextContent("Access revoked");
  expect(screen.queryByText("Private request")).toBeNull();
});
it("ignores an old response that completes after switching requests", async () => {
  let finish!: (v:unknown)=>void;
  const client={getRequest:async(id:string)=>({id,kind:"approval",title:id,state:"open",version:1}),respond:()=>new Promise(resolve=>{finish=resolve;})} as unknown as Client;
  const view=render(<NotifRequest client={client} id="first"/>);
  fireEvent.click(await screen.findByRole("button",{name:"Approve"}));
  view.rerender(<NotifRequest client={client} id="second"/>);
  expect(await screen.findByText("second")).toBeVisible();
  finish({id:"first",title:"old completed",state:"resolved",response:{outcome:"approved"}});
  await waitFor(()=>expect(screen.queryByText("old completed")).toBeNull());
  expect(screen.getByText("second")).toBeVisible();
});

it("uses a new operation key after switching conversations with an unfinished send", async () => {
  let finish!: ()=>void;
  const send=vi.fn().mockImplementationOnce(()=>new Promise<void>(resolve=>{finish=resolve;})).mockResolvedValue({});
  const client={call:async(_method:string,path:string)=>({id:path.split('/').at(-1),title:path.endsWith('first')?'First chat':'Second chat',messages:[]}),send} as unknown as Client;
  const view=render(<NotifConversation client={client} id="first"/>);
  await screen.findByText("First chat");
  fireEvent.change(screen.getByRole("textbox"),{target:{value:"same body"}});
  fireEvent.click(screen.getByRole("button",{name:"Send"}));
  view.rerender(<NotifConversation client={client} id="second"/>);
  await screen.findByText("Second chat");
  fireEvent.change(screen.getByRole("textbox"),{target:{value:"same body"}});
  finish();
  await waitFor(()=>expect(screen.getByRole("textbox")).toHaveValue("same body"));
  fireEvent.click(screen.getByRole("button",{name:"Send"}));
  await waitFor(()=>expect(send).toHaveBeenCalledTimes(2));
  expect(send.mock.calls[0][1]).not.toEqual(send.mock.calls[1][1]);
  expect(send.mock.calls[1][0].conversation_id).toBe("second");
});
