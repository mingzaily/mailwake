const roles = new Set([
  "inbox",
  "drafts",
  "sent",
  "trash",
  "junk",
  "archive",
  "all",
  "flagged",
]);
export function folderLabel(
  name: string,
  metadata: Record<string, string>,
  t: (key: string) => string,
) {
  const role = name.toUpperCase() === "INBOX" ? "inbox" : metadata[name];
  return roles.has(role) ? t(`ui.folder_${role}`) : name;
}
