import { useState } from "react";
import { Languages, Monitor, Moon, Sun } from "lucide-react";
import { readPreference, savePreference, useI18n } from "@/lib/i18n";
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
  const ThemeIcon = theme === "light" ? Sun : theme === "dark" ? Moon : Monitor;
  return (
    <div className={`flex flex-wrap gap-2 ${compact ? "justify-center" : ""}`}>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            aria-label={`${t("ui.language")}: ${t(`ui.language_${language}`)}`}
          >
            <Languages aria-hidden="true" data-icon="inline-start" />
            {t(`ui.language_${language}`)}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuLabel>{t("ui.language")}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuRadioGroup
            value={language}
            onValueChange={(value) => setLanguage(value as Language)}
          >
            {["en", "zh-CN"].map((value) => (
              <DropdownMenuRadioItem key={value} value={value}>
                {t(`ui.language_${value}`)}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
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
    </div>
  );
}
