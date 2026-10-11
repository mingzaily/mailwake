import { useEffect, useId, useRef } from "react";
import { useI18n } from "@/lib/i18n";
import type { Delivery } from "@/lib/types";
import { Button } from "./ui/button";
import { Time } from "./common";

export function DeliveryDetails({
  item,
  onClose,
}: {
  item: Delivery;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef(document.activeElement as HTMLElement | null);
  const title = useId();
  useEffect(() => {
    dialog.current?.showModal();
    const element = trigger.current;
    return () => element?.focus();
  }, []);
  const fields = [
    [
      t("ui.subject"),
      item.message?.test
        ? t("ui.delivery_test")
        : item.message?.subject == null
          ? t("ui.subject_unavailable")
          : item.message.subject || t("ui.subject_empty"),
    ],
    [t("ui.delivery_id"), item.id],
    [
      t("ui.delivery_source"),
      item.message?.test
        ? "—"
        : item.message
          ? `${item.message.mailbox_label || item.message.mailbox_id} / ${item.message.folder}`
          : t("ui.source_unavailable"),
    ],
    [t("ui.state"), t(`state.${item.state}`)],
    [t("ui.attempts"), item.attempts],
    [t("ui.error"), item.last_error?.message ?? "—"],
    [t("ui.created_at"), <Time key="created" value={item.created_at} />],
    ...(item.accepted_at
      ? [
          [
            t("ui.accepted_at"),
            <Time key="accepted" value={item.accepted_at} />,
          ],
        ]
      : []),
  ];
  return (
    <dialog
      ref={dialog}
      aria-labelledby={title}
      onClose={onClose}
      className="m-auto max-h-[calc(100dvh-2rem)] w-[min(40rem,calc(100vw-2rem))] overflow-auto rounded-lg border bg-card p-6 text-card-foreground backdrop:bg-[var(--dialog-backdrop)]"
    >
      <div className="flex flex-col gap-6">
        <h2 id={title} className="text-lg font-semibold">
          {t("ui.delivery_details")}
        </h2>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-4 text-sm">
          {fields.map(([label, value]) => (
            <div key={String(label)} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="whitespace-pre-wrap break-words">{value}</dd>
            </div>
          ))}
        </dl>
        {item.devices?.map((device) => (
          <section
            key={device.pairing_id}
            className="flex flex-col gap-2 border-t pt-4 text-sm"
          >
            <h3 className="font-medium">
              {device.device_name || t("ui.phone_unnamed")}
            </h3>
            <p>{t(`state.${device.status}`)}</p>
            {device.error_code && (
              <p className="whitespace-pre-wrap break-words">
                {t(device.error_code)}
              </p>
            )}
            <p className="break-all text-muted-foreground">
              {device.pairing_id}
            </p>
            {device.relay_id && (
              <p className="break-all text-muted-foreground">
                {device.relay_id}
              </p>
            )}
          </section>
        ))}
        <div className="flex justify-end">
          <Button variant="outline" onClick={onClose}>
            {t("ui.close")}
          </Button>
        </div>
      </div>
    </dialog>
  );
}
