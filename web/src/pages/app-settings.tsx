import { useI18n } from "@/lib/i18n";
import { PageHeading } from "@/components/common";
import { ManagementDevices } from "@/components/native-devices";

export function AppSettings() {
  const { t } = useI18n();
  return (
    <div className="mx-auto flex w-full max-w-[960px] flex-col gap-5">
      <PageHeading
        title={t("ui.app_settings")}
        description={t("ui.app_settings_description")}
      />
      <ManagementDevices />
    </div>
  );
}
