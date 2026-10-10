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
const nameRoles: Record<string, string> = {
  inbox: "inbox",
  drafts: "drafts",
  sent: "sent",
  "sent messages": "sent",
  "sent items": "sent",
  trash: "trash",
  "deleted messages": "trash",
  "deleted items": "trash",
  junk: "junk",
  spam: "junk",
  archive: "archive",
  archives: "archive",
  "all mail": "all",
  flagged: "flagged",
  starred: "flagged",
};
export function folderLabel(
  name: string,
  metadata: Record<string, string>,
  t: (key: string) => string,
) {
  const role = metadata[name] ?? nameRoles[name.toLowerCase()];
  return roles.has(role) ? t(`ui.folder_${role}`) : name;
}
