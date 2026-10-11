import { useQueryClient } from "@tanstack/react-query";
import { ClearHistoryButton } from "@/components/clear-history-button";
import { useI18n } from "@/lib/i18n";
import { useDeliveries } from "@/lib/queries";
import { ErrorNotice, PageHeading, Panel } from "@/components/common";
import { DeliveryTable } from "@/components/status-tables";
export function Deliveries() {
  const { t } = useI18n();
  const query = useDeliveries();
  const client = useQueryClient();
  return (
    <>
      <PageHeading
        title={t("ui.deliveries")}
        description={t("ui.deliveries_description")}
        actions={
          <ClearHistoryButton
            kind="deliveries"
            onCleared={async () => {
              await client.cancelQueries({ queryKey: ["deliveries"] });
              await Promise.all([
                client.invalidateQueries({ queryKey: ["deliveries"] }),
                client.invalidateQueries({ queryKey: ["status"] }),
              ]);
            }}
          />
        }
      />
      <ErrorNotice error={query.error} />
      <Panel>
        <DeliveryTable deliveries={query.data?.deliveries ?? []} />
      </Panel>
    </>
  );
}
