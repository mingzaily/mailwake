import coreIcon from "@/assets/core-icon.svg";
import { useI18n } from "@/lib/i18n";

export function Brand() {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5 font-semibold">
      <img
        src={coreIcon}
        alt=""
        width={24}
        height={24}
        className="size-6 shrink-0"
      />
      <b translate="no">{t("ui.brand")}</b>
    </div>
  );
}
