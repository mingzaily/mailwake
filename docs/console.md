# Management console

[简体中文](console.zh-CN.md) · [Documentation](README.md)

## Management interface

Initial setup has three steps: create the administrator, add a mailbox, and select folders. Finishing or skipping setup opens Notifications. App settings appears above Notifications in the sidebar and manages phone connections and management permissions; notification channels remain independent, so App management can be used with Bark delivery.

The React + TypeScript console uses shadcn/ui, Tailwind v4 and hash routes. The sidebar groups mailbox shortcuts under Mailboxes. Its footer shows the current mailbox name and connection budget on that mailbox’s monitoring and settings pages; other pages, including the mailbox list and creation form, hide the budget. Each mailbox opens its monitoring status, with a separate Mailbox settings route and a fixed-height Scan folders dialog with scan, selection and confirmation steps; long lists scroll inside the dialog while its header and actions stay visible; pages cover overview, mailbox connections and folder checks, notification settings, the latest 50 deliveries, runtime logs, administrator settings, API tokens and redacted diagnostics. Deletion and token revocation require inline confirmation.

English and Simplified Chinese share the backend catalogs. The first load follows Accept-Language; language and theme can be changed in Settings. The theme follows the system by default, with explicit light and dark choices. The sidebar becomes a keyboard-accessible drawer on narrow screens (360px and up). Overview refreshes every five seconds and pauses when hidden. Logs refresh incrementally every three seconds, support pause/filter/export, and stop following when you scroll upward. Exports contain folder names.

Authentication uses HttpOnly cookie sessions. CSRF and newly created API tokens stay in memory; local storage contains only language and theme preferences. Credentials are write-only. Validation and network errors preserve form input, revision conflicts offer Reload, and rate limits show retry seconds. Scripts can use `Authorization: Bearer mwk_…`.

The binary embeds hashed JS/CSS assets and Geist fonts. `/` and catalogs use `no-store`; `/assets/*` uses immutable one-year caching. CSP permits only same-origin scripts, styles, fonts and connections, plus data images. The console creates no inline scripts, style tags or style attributes. Third-party license texts, including the fonts' OFL, are available at `/third-party-notices.txt`.

### Component and styling rules

Use the new-york / Radix / lucide / Tailwind v4 configuration in `web/components.json`, with npm. Read documentation and examples through `npx shadcn@latest docs <component>` before using a component; add missing components with `npx shadcn@latest add <component>`. Reuse installed components and their variants.

- Keep component appearance in `web/src/components/ui/` and theme tokens in `@theme inline` / `:root`. Use semantic colors, `--radius-*` and `--ring`. Use Tailwind layout utilities with `gap-*`; size each page’s `h1` explicitly.
- Keep `styles.css` limited to local font imports, theme variables, document base styles, ordinary-element focus/link rules and reduced-motion preferences. Preserve `a:where(:not([data-slot]))` for ordinary links. Style controls through their component sources and use Spinner for loading indicators.
- Compose forms with FieldGroup / Field. Use NativeSelect for selects and Checkbox for folder selection. Put validation errors in FieldError, hints in FieldDescription, and associate errors through `aria-describedby` / `aria-invalid`. BusyButton uses Spinner with `data-icon="inline-start"`, `disabled` and `aria-busy`.
- Use DropdownMenu radio groups for language and theme controls with `modal={false}`; preserve preference storage and theme application. Use Card for panels, Separator for dividers, Badge variants for statuses, Empty for empty states and Alert for notices.
- Share Sidebar navigation components between desktop and narrow screens. Use `collapsible="none"` with the native drawer described below, preserving mailbox names, counts, the skip link and existing keyboard behavior.
- Keep fonts local and dependencies pinned to exact versions; `node scripts/licenses.mjs` audits licenses. All visible strings, including generated component labels, use the English and Simplified Chinese catalogs with matching placeholders.

### CSP overlay exceptions

Keep `style-src 'self'` without `unsafe-inline`. Radix modal Dialog, AlertDialog and Sheet use `react-remove-scroll` / `react-style-singleton` to insert runtime style tags for scroll locking. Those tags are blocked by Core’s CSP; the current modal components provide no supported injection-free mode, and nonce support would require a server-side CSP change. Preserve these three native dialogs:

| Source under `web/src/components/` | Purpose and behavior |
| --- | --- |
| `folder-scan-dialog.tsx` | Folder scanning: associated title/description, fixed header/actions, internally scrolling content, title focus on opening and trigger focus on closing; saving blocks closing and Escape. |
| `native-devices.tsx` | Phone pairing: associated title/description, title focus, internally scrolling content, fixed actions and focus returned to the pairing trigger. |
| `shell.tsx` | Keyboard-accessible narrow-screen navigation drawer; Sidebar’s Sheet/offcanvas branch stays unmounted. |

Destructive actions retain inline confirmation because AlertDialog shares the same injection path. Sonner 2.0.8 inserts style tags even when its static CSS is imported, so `useToast` retains static Alert markup with a close button and five-second dismissal. Keep these exceptions until a replacement passes keyboard/focus checks and produces no CSP violations with the built Core. Sheet remains a generated Sidebar dependency; its built-in labels use the shared catalogs.

### UI checks

`npm run check` includes `web/scripts/check-ui.mjs`. It scans `src/styles.css` and non-test TSX files under `src` for:

- Global `[data-slot=…]` component overrides, `.auth`, `.wizard`, `.spinner` rules and global select rules.
- Raw `<select>` outside `components/ui/native-select.tsx` and raw `<dialog>` outside the three CSP exceptions above.
- Native `type="checkbox"`, `space-y-*` and numbered background color classes such as `bg-green-500`.

These checks complement TypeScript, ESLint and component tests. Verify production overlays against the built Core’s CSP when changing their dependencies or behavior.

Folder discovery localizes standard roles reported by IMAP; custom folders keep their names and subscriptions retain original paths. App notification tests require an active push pairing and link to App settings. Settings forms use a centered, limited-width layout. Logs show localized summaries with raw attributes under technical details. Diagnostics displays the software version, build commit and platform together, with an explicit label for missing commit metadata.

The sidebar footer shows the signed-in administrator with an account menu. Preferences offer language and appearance choices directly. The first notification configuration uses the current console language; saved notification language remains independent.
