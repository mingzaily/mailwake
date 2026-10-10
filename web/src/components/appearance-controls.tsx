import { useState } from "react";
import { Languages, Monitor, Moon, Sun } from "lucide-react";
import { languages, readPreference, savePreference, useI18n } from "@/lib/i18n";
import { applyTheme } from "@/lib/preferences";
import type { Language } from "@/lib/types";
import { Button } from "./ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from "./ui/dropdown-menu";

export function AppearanceControls({ compact = false }: { compact?: boolean }) {
  const { t, language, setLanguage } = useI18n();
  const [theme, setTheme] = useState(readPreference("theme") ?? "system");
  function changeTheme(value: string) {
    setTheme(value);
    savePreference("theme", value);
    applyTheme(value);
  }
  const languageLabel = languages.find(({ code }) => code === language)?.label;
  const ThemeIcon = theme === "light" ? Sun : theme === "dark" ? Moon : Monitor;
  const languageMenu = (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`${t("ui.language")}: ${languageLabel}`}
        >
          <Languages aria-hidden="true" data-icon="inline-start" />
          {languageLabel}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={compact ? "start" : "end"}>
        <DropdownMenuLabel>{t("ui.language")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={language}
          onValueChange={(value) => setLanguage(value as Language)}
        >
          {languages.map(({ code: value, label }) => (
            <DropdownMenuRadioItem key={value} value={value}>
              {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
  const themeMenu = (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={`${t("ui.theme")}: ${t(`ui.theme_${theme}`)}`}
        >
          <ThemeIcon aria-hidden="true" data-icon="inline-start" />
          {t(`ui.theme_${theme}`)}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t("ui.theme")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup value={theme} onValueChange={changeTheme}>
          {["system", "dark", "light"].map((value) => (
            <DropdownMenuRadioItem key={value} value={value}>
              {t(`ui.theme_${value}`)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
  if (compact)
    return (
      <div className="flex flex-wrap justify-center gap-2">
        {languageMenu}
        {themeMenu}
      </div>
    );
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm font-medium">{t("ui.language")}</span>
        {languageMenu}
      </div>
      <div className="flex items-center justify-between gap-4">
        <span className="text-sm font-medium">{t("ui.theme")}</span>
        {themeMenu}
      </div>
    </div>
  );
}
