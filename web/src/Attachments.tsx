import { useEffect, useState } from "react";
import { api } from "./api";
type Attachment = { id: string; file_name: string; size: number };
export function Attachments({ conversation }: { conversation: string }) {
  const [files, setFiles] = useState<Attachment[]>([]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    setFiles([]);
    void api<Attachment[]>("/conversations/" + conversation + "/attachments")
      .then(setFiles)
      .catch((e) => setError(e.message));
  }, [conversation]);
  return (
    <div className="attachments">
      <div>
        {files.map((f) => (
          <a
            key={f.id}
            href={"/api/v1/attachments/" + f.id}
            download={f.file_name}
          >
            {f.file_name} <small>{Math.ceil(f.size / 1024)} KB</small>
          </a>
        ))}
      </div>
      <label className="attachment-upload">
        {busy ? "Uploading…" : "Attach a file"}
        <input
          type="file"
          disabled={busy}
          accept=".txt,.json,.pdf,.png,.jpg,.jpeg,.webp"
          onChange={async (e) => {
            const file = e.target.files?.[0];
            if (!file) return;
            setBusy(true);
            setError("");
            try {
              const data = new FormData();
              data.append("file", file);
              const r = await fetch(
                "/api/v1/conversations/" + conversation + "/attachments",
                { method: "POST", body: data },
              );
              const x = await r.json();
              if (!r.ok) throw new Error(x.error?.message ?? "Upload failed");
              setFiles((old) => [...old, x]);
            } catch (e) {
              setError((e as Error).message);
            } finally {
              setBusy(false);
            }
          }}
        />
      </label>
      <small>5 MiB maximum. Files are private to this conversation.</small>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
