import { Component, Suspense, type ReactNode } from "react";
import { useI18n } from "@/lib/i18n";
import { Alert, AlertDescription } from "./ui/alert";
import { Button } from "./ui/button";

class PageErrorBoundary extends Component<
  { children: ReactNode; fallback: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

export function PageBoundary({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  return (
    <PageErrorBoundary
      fallback={
        <Alert variant="destructive">
          <AlertDescription>
            <p>{t("ui.page_load_failed")}</p>
            <Button variant="outline" onClick={() => window.location.reload()}>
              {t("ui.reload")}
            </Button>
          </AlertDescription>
        </Alert>
      }
    >
      <Suspense
        fallback={
          <p role="status" className="py-6 text-muted-foreground">
            {t("ui.loading")}
          </p>
        }
      >
        {children}
      </Suspense>
    </PageErrorBoundary>
  );
}
