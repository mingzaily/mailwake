import { lazy } from "react";
import { PageBoundary } from "./components/page-boundary";

const Overview = lazy(() =>
  import("./pages/overview").then((module) => ({ default: module.Overview })),
);
const Mailboxes = lazy(() =>
  import("./pages/mailboxes").then((module) => ({ default: module.Mailboxes })),
);
const AppSettings = lazy(() =>
  import("./pages/app-settings").then((module) => ({
    default: module.AppSettings,
  })),
);
const Notifications = lazy(() =>
  import("./pages/notifications").then((module) => ({
    default: module.Notifications,
  })),
);
const Deliveries = lazy(() =>
  import("./pages/deliveries").then((module) => ({
    default: module.Deliveries,
  })),
);
const Logs = lazy(() =>
  import("./pages/logs").then((module) => ({ default: module.Logs })),
);
const Settings = lazy(() =>
  import("./pages/settings").then((module) => ({ default: module.Settings })),
);
const Diagnostics = lazy(() =>
  import("./pages/diagnostics").then((module) => ({
    default: module.Diagnostics,
  })),
);

export function ConsolePage({
  page,
  id,
  view,
}: {
  page: string;
  id?: string;
  view?: string;
}) {
  let content;
  switch (page) {
    case "mailboxes":
      content = <Mailboxes id={id} view={view} />;
      break;
    case "app_settings":
      content = <AppSettings />;
      break;
    case "notifications":
      content = <Notifications />;
      break;
    case "deliveries":
      content = <Deliveries />;
      break;
    case "logs":
      content = <Logs />;
      break;
    case "settings":
      content = <Settings />;
      break;
    case "diagnostics":
      content = <Diagnostics />;
      break;
    default:
      content = <Overview />;
  }
  return (
    <PageBoundary key={`${page}/${id ?? ""}/${view ?? ""}`}>
      {content}
    </PageBoundary>
  );
}
