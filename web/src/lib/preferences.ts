export function applyTheme(theme: string) {
  const selected =
    theme === "system"
      ? matchMedia("(prefers-color-scheme: light)").matches
        ? "light"
        : "dark"
      : theme;
  document.documentElement.dataset.theme = selected;
}
