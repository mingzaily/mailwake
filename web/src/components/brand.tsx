import { Mail } from "lucide-react";
import { useI18n } from "@/lib/i18n";

export function Brand() {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5 font-semibold">
      <Mail aria-hidden="true" className="size-6 text-primary" />
      <b translate="no">{t("ui.brand")}</b>
    </div>
  );
}
