import type { Request } from "./contracts";

// This is presentation grouping only. Each request keeps its identity and decision.
// In particular, no approval or task can ever be grouped as a reusable answer.
export function groupQuestions(requests: Request[]) {
  const groups: Request[][] = [];
  const positions = new Map<string, number>();
  for (const request of requests) {
    const key =
      request.kind === "question" &&
      request.state === "open" &&
      request.shared_key
        ? JSON.stringify([
            request.shared_key,
            request.title,
            request.body,
            request.choices || [],
          ])
        : JSON.stringify(["individual", request.id]);
    const position = positions.get(key);
    if (position === undefined) {
      positions.set(key, groups.length);
      groups.push([request]);
    } else groups[position].push(request);
  }
  return groups;
}
