import { useQuery } from "@tanstack/react-query";
import { api, downloadJSON } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Diagnostics as Report } from "@/lib/types";
import { ErrorNotice, PageHeading, Panel } from "@/components/common";
import { Button } from "@/components/ui/button";
export function Diagnostics() {
  const { t } = useI18n();
  const query = useQuery({
    queryKey: ["diagnostics"],
    queryFn: () => api<Report>("/diagnostics"),
  });
  return (
    <div className="mx-auto flex w-full max-w-[960px] flex-col gap-5">
      <PageHeading
        title={t("ui.diagnostics")}
        description={t("ui.diagnostics_description")}
      />
      <ErrorNotice error={query.error} />
      <Panel title={t("ui.build_information")}>
        <div className="p-4 md:p-5">
          <dl className="grid grid-cols-[110px_minmax(0,1fr)] gap-3.5 md:grid-cols-[160px_minmax(0,1fr)]">
            <dt className="text-muted-foreground">{t("ui.version")}</dt>
            <dd className="wrap-anywhere font-mono text-[13px] tabular-nums">
              {query.data?.build.version ?? "—"}
            </dd>
            <dt className="text-muted-foreground">{t("ui.revision")}</dt>
            <dd className="wrap-anywhere font-mono text-[13px] tabular-nums">
              {query.data?.build.revision || t("ui.not_provided")}
            </dd>
            <dt className="text-muted-foreground">{t("ui.platform")}</dt>
            <dd className="wrap-anywhere font-mono text-[13px] tabular-nums">
              {query.data
                ? `${query.data.build.os} / ${query.data.build.arch} / ${query.data.build.go_version}`
                : "—"}
            </dd>
          </dl>
        </div>
      </Panel>
      <Panel title={t("ui.redacted_report")}>
        <div className="p-4 md:p-5 flex flex-col gap-4">
          <p className="text-muted-foreground">{t("ui.diagnostics_hint")}</p>
          <div>
            <Button
              disabled={!query.data}
              onClick={() =>
                downloadJSON(query.data, "mailwake-diagnostics.json")
              }
            >
              {t("ui.download_report")}
            </Button>
          </div>
        </div>
      </Panel>
    </div>
  );
}
