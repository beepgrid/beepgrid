import { useResource } from "../data";
import type { Request } from "../contracts";
import { useRoute, navigate } from "../navigation";
import { Button, ErrorView, Loading } from "../ui";
import { RequestCard } from "../RequestCard";
import Conversations from "./Conversations";

export default function Inbox() {
  const route = useRoute();
  return route.page === "requests" && route.id ? (
    <RequestView id={route.id} />
  ) : (
    <Conversations />
  );
}
function RequestView({ id }: { id: string }) {
  const q = useResource<Request>("/requests/" + id);
  return q.isPending ? (
    <Loading />
  ) : !q.data ? (
    <ErrorView error={q.error} retry={() => void q.refetch()} />
  ) : (
    <section className="request-standalone">
      <Button
        onClick={() => navigate("/conversations/" + q.data!.conversation_id)}
      >
        Open conversation
      </Button>
      <RequestCard request={q.data} />
    </section>
  );
}
