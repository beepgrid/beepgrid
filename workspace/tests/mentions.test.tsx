import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { editedMentions, MentionEditor, MentionText } from "../src/Mentions";
import type { Mention } from "../src/contracts";
import { agent } from "./fixtures";
it("retains Unicode offsets outside edits and removes edited mention tokens", () => {
  const before = "🙂 @DevOps Agent 2, check staging",
    m = { principal_id: "a", start: 2, end: 17 };
  expect(editedMentions(before, "Hey " + before, [m])).toEqual([
    { ...m, start: 6, end: 21 },
  ]);
  expect(
    editedMentions(before, before.replace("Agent 2", "Agent 3"), [m]),
  ).toEqual([]);
  expect(editedMentions(before, before + " today", [m])).toEqual([m]);
});
it("selects a named participant with spaces, distinguishing duplicate names", () => {
  const people = [
    {
      ...agent,
      kind: "agent" as const,
      id: "agent_one",
      name: "DevOps Agent 2",
    },
    {
      ...agent,
      kind: "agent" as const,
      id: "agent_two",
      name: "DevOps Agent 2",
    },
    { ...agent, kind: "agent" as const, id: "agent_three", name: "Builder 7" },
  ];
  function Editor() {
    const [value, setValue] = useState(""),
      [mentions, setMentions] = useState<Mention[]>([]);
    return (
      <>
        <MentionEditor
          value={value}
          onChange={setValue}
          mentions={mentions}
          onMentions={setMentions}
          participants={people}
          onKeyDown={() => {}}
        />
        <output>{JSON.stringify(mentions)}</output>
      </>
    );
  }
  render(<Editor />);
  const input = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(input, {
    target: { value: "🙂 @DevOps", selectionStart: 10 },
  });
  expect(screen.getAllByRole("option")).toHaveLength(2);
  fireEvent.keyDown(input, { key: "ArrowDown" });
  // A native selection notification must not reset keyboard navigation.
  fireEvent.select(input);
  fireEvent.keyDown(input, { key: "Enter" });
  expect(input).toHaveValue("🙂 @DevOps Agent 2 ");
  expect(screen.getByRole("status")).toHaveTextContent(
    '"principal_id":"agent_two"',
  );
  expect(screen.getByRole("status")).toHaveTextContent('"start":2,"end":17');
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});
it("renders addressed tokens as identity links without rewriting the original text", () => {
  render(
    <MentionText
      body="Hey @DevOps Agent 2, check staging"
      mentions={[{ principal_id: "a", start: 4, end: 19 }]}
      participants={[
        { ...agent, kind: "agent" as const, id: "a", name: "DevOps Agent 2" },
      ]}
    />,
  );
  expect(screen.getByRole("link", { name: "@DevOps Agent 2" })).toHaveAttribute(
    "href",
    "/agents/a",
  );
});
