import { useEffect, useState } from "react";
import {
  keepPreviousData,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { api, APIError, configureAPI, setCSRF } from "./lib/api";
import {
  I18nContext,
  isLanguage,
  loadCatalog,
  readPreference,
  savePreference,
  useI18n,
} from "./lib/i18n";
import { navigate, useRoute } from "./lib/router";
import { AuthScreen } from "./components/auth-screen";
import { Shell } from "./components/shell";
import { ErrorNotice, ToastProvider } from "./components/common";
import { SetupWizard } from "./components/setup";
import { AppSettings } from "./pages/app-settings";
import { Settings } from "./pages/settings";
import { Diagnostics } from "./pages/diagnostics";
import { applyTheme } from "./lib/preferences";
import { Notifications } from "./pages/notifications";
import { Deliveries } from "./pages/deliveries";
import { Logs } from "./pages/logs";
import { Overview } from "./pages/overview";
import { Mailboxes } from "./pages/mailboxes";
import type { Language } from "./lib/types";

const client = new QueryClient({
  defaultOptions: {
    queries: { retry: false, refetchOnWindowFocus: true },
    mutations: { retry: false },
  },
});
function Console() {
  const { t } = useI18n();
  const [page, id, view] = useRoute();
  const [authenticated, setAuthenticated] = useState(false);
  const [setup, setSetup] = useState(false);
  const [wizard, setWizard] = useState(false);
  const [error, setError] = useState<unknown>();
  const session = useQuery({
    queryKey: ["session"],
    queryFn: () =>
      api<{ csrf_token: string; username: string }>("/session", {
        public: true,
      }),
    refetchOnWindowFocus: false,
  });
  useEffect(() => {
    if (session.data) {
      setCSRF(session.data.csrf_token);
      setAuthenticated(true);
    }
    if (session.error instanceof APIError)
      setSetup(session.error.failure.code === "setup_required");
  }, [session.data, session.error]);
  useEffect(() => {
    configureAPI({
      onExpired: () => {
        setAuthenticated(false);
        setCSRF("");
        client.clear();
        navigate("login");
      },
    });
  }, []);
  async function logout() {
    try {
      await api("/session", { method: "DELETE" });
      setCSRF("");
      setAuthenticated(false);
      client.clear();
      navigate("login");
    } catch (error) {
      setError(error);
    }
  }
  if (session.isPending && !authenticated)
    return (
      <div className="flex min-h-svh items-center justify-center p-6 md:p-10">
        <p>{t("ui.loading")}</p>
      </div>
    );
  if (!authenticated)
    return (
      <AuthScreen
        setup={setup}
        error={
          session.error &&
          (!(session.error instanceof APIError) ||
            (session.error.status !== 401 &&
              session.error.failure.code !== "setup_required"))
            ? session.error
            : undefined
        }
        onSuccess={() => {
          void session.refetch();
          setAuthenticated(true);
          setWizard(setup);
          setSetup(false);
          navigate(setup ? "setup" : "overview");
        }}
      />
    );
  if (wizard)
    return (
      <SetupWizard
        done={() => {
          setWizard(false);
          navigate("notifications");
        }}
      />
    );
  return (
    <Shell
      username={session.data?.username}
      page={page}
      mailboxId={id}
      logout={() => void logout()}
    >
      <ErrorNotice error={error} />
      <ConsolePage page={page} id={id} view={view} />
    </Shell>
  );
}
function ConsolePage({
  page,
  id,
  view,
}: {
  page: string;
  id?: string;
  view?: string;
}) {
  switch (page) {
    case "mailboxes":
      return <Mailboxes id={id} view={view} />;
    case "app_settings":
      return <AppSettings />;
    case "notifications":
      return <Notifications />;
    case "deliveries":
      return <Deliveries />;
    case "logs":
      return <Logs />;
    case "settings":
      return <Settings />;
    case "diagnostics":
      return <Diagnostics />;
    default:
      return <Overview />;
  }
}
function LocalizedConsole() {
  const [language, setLanguage] = useState<Language | undefined>(() => {
    const saved = readPreference("language");
    return isLanguage(saved) ? saved : undefined;
  });
  const catalog = useQuery({
    queryKey: ["catalog", language],
    queryFn: () => loadCatalog(language),
    staleTime: Infinity,
    placeholderData: keepPreviousData,
  });
  useEffect(() => {
    const theme = readPreference("theme") ?? "system";
    applyTheme(theme);
    const media = matchMedia("(prefers-color-scheme: light)");
    const update = () => applyTheme(readPreference("theme") ?? "system");
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (!catalog.data) return;
    document.documentElement.lang = catalog.data.language;
    configureAPI({ language: catalog.data.language });
  }, [catalog.data]);
  if (!catalog.data)
    return (
      <main className="flex min-h-svh items-center justify-center p-6 md:p-10">
        <button
          onClick={() => void catalog.refetch()}
          aria-busy={catalog.isPending}
        >
          Mailwake
        </button>
      </main>
    );
  return (
    <I18nContext.Provider
      value={{
        ...catalog.data,
        setLanguage: (value) => {
          savePreference("language", value);
          setLanguage(value);
        },
      }}
    >
      <ToastProvider>
        <Console />
      </ToastProvider>
    </I18nContext.Provider>
  );
}
export function App() {
  return (
    <QueryClientProvider client={client}>
      <LocalizedConsole />
    </QueryClientProvider>
  );
}
