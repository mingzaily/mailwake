import { useEffect, useId, useRef } from "react";
import { useI18n } from "@/lib/i18n";
import { BusyButton, ErrorNotice } from "@/components/common";
import { Button } from "@/components/ui/button";
export function ConfirmActionDialog({
  title,
  confirmLabel,
  description,
  busy,
  error,
  onConfirm,
  onClose,
}: {
  title: string;
  confirmLabel: string;
  description: string;
  busy: boolean;
  error: unknown;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDialogElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const trigger = useRef(document.activeElement as HTMLElement | null);
  const heading = useId();
  const detail = useId();
  useEffect(() => {
    dialog.current?.showModal();
    cancel.current?.focus();
    const element = trigger.current;
    return () => element?.focus();
  }, []);
  return (
    <dialog
      ref={dialog}
      aria-labelledby={heading}
      aria-describedby={detail}
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
      className="m-auto w-[min(28rem,calc(100vw-2rem))] rounded-lg border bg-card p-6 text-card-foreground backdrop:bg-[var(--dialog-backdrop)]"
    >
      <div className="flex flex-col gap-5">
        <h2 id={heading} className="text-lg font-semibold">
          {title}
        </h2>
        <p id={detail} className="text-sm text-muted-foreground">
          {description}
        </p>
        <ErrorNotice error={error} />
        <div className="flex justify-end gap-3">
          <Button
            ref={cancel}
            variant="outline"
            disabled={busy}
            onClick={onClose}
          >
            {t("ui.cancel")}
          </Button>
          <BusyButton variant="destructive" busy={busy} onClick={onConfirm}>
            {confirmLabel}
          </BusyButton>
        </div>
      </div>
    </dialog>
  );
}
