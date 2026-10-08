import { useI18n } from "@/lib/i18n";
import { PageHeading, Panel } from "@/components/common";
import { NativeDevices, ManagementDevices } from "@/components/native-devices";
import { DeliveryEditor } from "@/components/delivery-form";
export function Notifications() {
  const { t } = useI18n();
  return (
    <>
      <PageHeading
        title={t("ui.notifications")}
        description={t("ui.notifications_description")}
      />
      <Panel title={t("ui.channel")}>
        <div className="p-4 md:p-5">
          <DeliveryEditor />
        </div>
      </Panel>
      <ManagementDevices />
      <NativeDevices pairingEnabled={false} />
    </>
  );
}
