import { useI18n } from "@/lib/i18n";
import { useDeliveries, useMailboxes, useStatus } from "@/lib/queries";
import { ErrorNotice, PageHeading, Panel } from "@/components/common";
import { DeliveryTable, FolderTable } from "@/components/status-tables";
import { Button } from "@/components/ui/button";
export function Overview() {
  const { t } = useI18n();
  const status = useStatus();
  const boxes = useMailboxes();
  const deliveries = useDeliveries();
  const data = status.data;
  const issues = Object.entries(data?.notices ?? {}).map(
    ([scope, failure]) => ({
      key: scope,
      text: t("ui.scoped_issue", {
        object:
          scope === "delivery"
            ? t("ui.notifications")
            : (boxes.data?.mailboxes.find(
                (box) => box.id === scope.split(":")[1],
              )?.label ?? scope.split(":")[1]),
        message: failure.message,
      }),
      href:
        scope === "delivery"
          ? "#/notifications"
          : `#/mailboxes/${scope.split(":")[1]}${scope.startsWith("mailbox:") ? "/settings" : ""}`,
    }),
  );
  for (const folder of data?.folders ?? [])
    if (folder.state === "auth_required")
      issues.push({
        key: `${folder.mailbox_id}/${folder.folder}`,
        text: t("ui.scoped_issue", {
          object: `${folder.mailbox_label} · ${folder.folder}`,
          message: folder.last_error?.message ?? t("state.auth_required"),
        }),
        href: `#/mailboxes/${folder.mailbox_id}/settings`,
      });
  if (data?.delivery.dead)
    issues.push({
      key: "delivery-failed",
      text: t("ui.delivery_failed_count", { count: data.delivery.dead }),
      href: "#/deliveries",
    });
  if (data && !data.channel)
    issues.push({
      key: "channel",
      text: t("ui.channel_missing"),
      href: "#/notifications",
    });
  return (
    <>
      <PageHeading
        title={t("ui.overview")}
        description={t("ui.overview_description")}
        actions={
          <Button asChild variant="outline">
            <a href="#/mailboxes/new">{t("ui.add_mailbox")}</a>
          </Button>
        }
      />
      <div className="grid grid-cols-2 rounded-lg border bg-card md:grid-cols-4">
        {[
          ["mailboxes", boxes.data?.mailboxes.length],
          [
            "active_folders",
            data?.folders.filter((folder) => folder.state === "watching")
              .length,
          ],
          ["pending", data?.delivery.pending],
          ["failed", data?.delivery.dead],
        ].map(([key, value]) => (
          <div
            className="grid gap-1 px-4 py-3 border-border-subtle max-md:even:border-l max-md:nth-[n+3]:border-t md:not-first:border-l"
            key={key}
          >
            <span className="text-xs text-muted-foreground">
              {t(`ui.${key}`)}
            </span>
            <b className="font-mono text-2xl font-medium">{value ?? "—"}</b>
          </div>
        ))}
      </div>
      <Panel title={t("ui.needs_attention")}>
        {issues.length ? (
          issues.map((issue) => (
            <div
              className="flex flex-wrap items-center gap-4 border-b border-border-subtle px-4 py-3 last:border-0"
              key={issue.key}
            >
              <span>{issue.text}</span>
              <a className="whitespace-nowrap md:ml-auto" href={issue.href}>
                {t("ui.resolve")}
              </a>
            </div>
          ))
        ) : (
          <p className="flex flex-wrap items-center gap-4 border-b border-border-subtle px-4 py-3 last:border-0 text-muted-foreground">
            {t("ui.healthy")}
          </p>
        )}
      </Panel>
      <Panel
        title={t("ui.folders")}
        actions={
          <span className="text-muted-foreground text-xs">
            {t("ui.live_refresh")}
          </span>
        }
      >
        <FolderTable folders={data?.folders ?? []} />
      </Panel>
      <Panel
        title={t("ui.recent_deliveries")}
        actions={<a href="#/deliveries">{t("ui.view_all")}</a>}
      >
        <ErrorNotice error={deliveries.error} />
        <DeliveryTable
          deliveries={deliveries.data?.deliveries.slice(0, 6) ?? []}
        />
      </Panel>
    </>
  );
}
