import { useState } from "react";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { Button } from "@/components/ui/button";
import { ConfirmActionDialog } from "@/components/confirm-action-dialog";

export function ClearHistoryButton({
  kind,
  onCleared,
}: {
  kind: "deliveries" | "logs";
  onCleared: () => void | Promise<void>;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function clear() {
    setBusy(true);
    setError(undefined);
    try {
      await api(`/${kind}`, { method: "DELETE" });
      await onCleared();
      setOpen(false);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {t(`ui.clear_${kind}`)}
      </Button>
      {open && (
        <ConfirmActionDialog
          title={t(`ui.clear_${kind}`)}
          description={t(`ui.clear_${kind}_confirm`)}
          confirmLabel={t("ui.clear")}
          busy={busy}
          error={error}
          onConfirm={() => void clear()}
          onClose={() => {
            setOpen(false);
            setError(undefined);
          }}
        />
      )}
    </>
  );
}
