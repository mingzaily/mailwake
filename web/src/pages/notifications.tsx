import { useI18n } from "@/lib/i18n";
import { PageHeading, Panel } from "@/components/common";
import { NativeDevices } from "@/components/native-devices";
import { DeliveryEditor } from "@/components/delivery-form";
export function Notifications() {
  const { t } = useI18n();
  return (
    <div className="mx-auto flex w-full max-w-[960px] flex-col gap-5">
      <PageHeading
        title={t("ui.notifications")}
        description={t("ui.notifications_description")}
      />
      <Panel title={t("ui.channel")}>
        <div className="p-4 md:p-5">
          <DeliveryEditor />
        </div>
      </Panel>
      <NativeDevices pairingEnabled={false} />
    </div>
  );
}
