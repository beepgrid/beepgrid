import type { Attachment } from "./contracts";
import { workspaceURL } from "./navigation";
import { Icon } from "./ui";
export function fileSize(size: number) {
  return size >= 1024 * 1024
    ? `${(size / (1024 * 1024)).toFixed(1)} MB`
    : `${Math.max(1, Math.ceil(size / 1024))} KB`;
}
export function FileLinks({ files }: { files?: Attachment[] }) {
  if (!files?.length) return null;
  return (
    <div className="message-files">
      {files.map((file) => (
        <a
          className="message-file"
          key={file.id}
          href={workspaceURL("/api/v1/attachments/" + file.id)}
          download
        >
          <span className="file-mark">
            <Icon name="file" size={19} />
          </span>
          <span className="file-label">
            <strong>{file.file_name}</strong>
            <small>{fileSize(file.size)}</small>
          </span>
          <Icon name="download" size={16} />
        </a>
      ))}
    </div>
  );
}
