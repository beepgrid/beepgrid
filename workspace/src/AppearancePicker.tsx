import { useState } from "react";
import { useOperation } from "./data";
import { Button, ErrorView, Form, IdentityIcon, Modal } from "./ui";

export default function AppearancePicker({
  kind,
  id,
  name,
  icon = "",
  version,
  onClose,
}: {
  kind: "agent" | "group";
  id: string;
  name: string;
  icon?: string;
  version?: number;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState(icon);
  const op = useOperation();
  const prefix = kind === "agent" ? "mark-" : "role-";
  return (
    <Modal title={`Icon for ${name}`} onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run("/appearance", {
              target: { kind, id },
              icon: selected,
              version,
            })
            .then((r) => {
              if (r) onClose();
            })
        }
      >
        <div
          className="appearance-grid"
          role="group"
          aria-label="Choose an icon"
        >
          {Array.from({ length: 12 }, (_, i) => (
            <button
              key={i}
              type="button"
              aria-label={`Icon ${i + 1}`}
              aria-pressed={selected === prefix + i}
              onClick={() => setSelected(prefix + i)}
            >
              <IdentityIcon
                kind={kind}
                id={id}
                label={name}
                icon={prefix + i}
              />
            </button>
          ))}
        </div>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={() => setSelected("")} disabled={!selected}>
            Use default
          </Button>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            type="submit"
            disabled={op.pending || selected === icon}
          >
            Save icon
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
