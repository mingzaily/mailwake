import { useI18n } from "@/lib/i18n";
import { useDeliveries } from "@/lib/queries";
import { ErrorNotice, PageHeading, Panel } from "@/components/common";
import { DeliveryTable } from "@/components/status-tables";
export function Deliveries() {
  const { t } = useI18n();
  const query = useDeliveries();
  return (
    <>
      <PageHeading
        title={t("ui.deliveries")}
        description={t("ui.deliveries_description")}
      />
      <ErrorNotice error={query.error} />
      <Panel title={t("ui.recent_deliveries")}>
        <DeliveryTable deliveries={query.data?.deliveries ?? []} />
      </Panel>
    </>
  );
}
