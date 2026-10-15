# Management console

[Contributor documentation](README.md) · [User guides](https://mailwake.oritx.com/en/docs/)

The browser favicon and shared console brand mark use the same envelope and notification dot using the console’s indigo primary (#3F5BD8), pale-blue accent (#8FA8FF) and white, independent of the iOS App icon. Its SVG source and 32×32 PNG fallback live in `web/src/assets/core-icon.*`; Vite fingerprints both and the existing `/assets/*` route serves them locally.

## Management interface

Initial setup opens with a welcome page. Select Continue to start the three steps: create the administrator, add a mailbox, and select folders. The introduction appears in the shared progress header and layout, while the setup counter starts at 1 of 3 for administrator creation. Its brief fade-in respects reduced-motion preferences. Adding a mailbox requires an explicit successful connection test before saving; changes to connection fields require another test. Discovered folders appear in the third step using the existing test result. Finishing or skipping setup opens Notifications. App settings appears above Notifications in the sidebar and manages phone connections and management permissions; notification channels remain independent, so App management can be used with Bark delivery.

The React + TypeScript console uses shadcn/ui, Tailwind v4 and hash routes. The sidebar groups mailbox shortcuts under Mailboxes. The footer contains the GitHub link and administrator account menu. Breadcrumbs and monitoring totals appear in the header; the desktop sidebar can be toggled. Connection budgets appear in mailbox pages and folder selection. The mailbox list shows configured monitoring demand divided by the total connection limit (for example, 1 / 10), using the same folder check formula as the detail page. Details show demand against monitoring capacity (1 / 9), with one slot reserved for discovery and testing. Scheduled checks share one monitoring slot even between runs when their network connection is closed. Until status loads, the list shows an unknown count; failed status reads show an error. Each mailbox opens its monitoring status, with a separate Mailbox settings route and a fixed-height Scan folders dialog with scan, selection and confirmation steps; long lists scroll inside the dialog while its header and actions stay visible; pages cover overview, mailbox connections and folder checks, notification settings, the latest 50 deliveries, runtime logs, administrator settings, API tokens and redacted diagnostics. Deletion and token revocation require inline confirmation.

The console shares all nine language catalogs with the backend. The first load follows Accept-Language; language and theme can be changed in Settings. The theme follows the system by default, with explicit light and dark choices. The sidebar becomes a keyboard-accessible drawer on narrow screens (360px and up). Overview refreshes every five seconds and pauses when hidden. Logs refresh incrementally every three seconds, support pause/filter/export, and stop following when you scroll upward. Exports contain folder names.

Authentication uses HttpOnly cookie sessions. CSRF and newly created API tokens stay in memory; local storage contains only language and theme preferences. Credentials are write-only. Incorrect login credentials return `login_credentials_invalid` with an actionable username-or-password message, while protected requests with missing or expired authentication retain `unauthorized`. Login failures preserve form input and stay on the login page. Validation and network errors preserve form input, revision conflicts offer Reload, and rate limits show retry seconds. Scripts can use `Authorization: Bearer mwk_…`.

The binary embeds hashed JS/CSS assets and Geist fonts. `/` and catalogs use `no-store`; `/assets/*` uses immutable one-year caching. CSP permits same-origin scripts, styles, fonts and connections, plus data images. Component style tags receive precise nonce or hash authorization as described below; script restrictions remain unchanged. Third-party license texts, including the fonts' OFL, are available at `/third-party-notices.txt`.

### Component and styling rules

Use the new-york / Radix / lucide / Tailwind v4 configuration in `web/components.json`, with npm. Read documentation and examples through `npx shadcn@latest docs <component>` before using a component; add missing components with `npx shadcn@latest add <component>`. Reuse installed components and their variants.

- Keep component appearance in `web/src/components/ui/` and theme tokens in `@theme inline` / `:root`. Use semantic colors, `--radius-*` and `--ring`. Use Tailwind layout utilities with `gap-*`; size each page’s `h1` explicitly.
- Keep `styles.css` limited to local font imports, theme variables, document base styles, ordinary-element focus/link rules and reduced-motion preferences. Preserve `a:where(:not([data-slot]))` for ordinary links. Style controls through their component sources and use Spinner for loading indicators.
- Compose forms with FieldGroup / Field. Use NativeSelect for selects and Checkbox for folder selection. Put validation errors in FieldError, hints in FieldDescription, and associate errors through `aria-describedby` / `aria-invalid`. BusyButton uses Spinner with `data-icon="inline-start"`, `disabled` and `aria-busy`.
- Use DropdownMenu radio groups for language and theme controls with `modal={false}`; preserve preference storage and theme application. Use Card for panels, Separator for dividers, Badge variants for statuses, Empty for empty states and Alert for notices.
- Share Sidebar navigation components between desktop and narrow screens. Use `collapsible="none"` with the native drawer described below, preserving mailbox names, counts, the skip link and existing keyboard behavior.
- Keep fonts local and dependencies pinned to exact versions; `node scripts/licenses.mjs` audits licenses. All visible strings, including generated component labels, use the nine shared language catalogs with matching placeholders.

### CSP component authorization

The document receives a fresh random style nonce on each response. `authorizeComponentStyles` supplies it through `get-nonce` before React mounts; Radix scroll-lock styles use that nonce. Sonner 2.0.8 injects fixed CSS without nonce support, so `scripts/csp.mjs` computes its SHA-256 authorization during the build and emits the embedded `style-hashes.json` manifest. Dependency upgrades must pass the extraction check and a production-browser CSP check. Keep `unsafe-inline` and `unsafe-eval` absent from the policy.

Prefer shadcn components for new overlays. Existing native dialogs retain their focus and lifecycle behavior; migrate them when working on their interaction, with keyboard and production CSP verification:

| Source under `web/src/components/` | Purpose and behavior |
| --- | --- |
| `folder-scan-dialog.tsx` | Folder scanning: associated title/description, fixed header/actions, internally scrolling content, title focus on opening and trigger focus on closing; saving blocks closing and Escape. |
| `delivery-details.tsx` | Delivery details: full subject, source, errors and device status; native modal focus containment, Escape dismissal and trigger focus restoration. |
| `native-devices.tsx` | Phone pairing: associated title/description, focus inside the dialog, fixed actions and focus returned to the trigger. |
| `confirm-action-dialog.tsx` | Shared permission revocation and history clearing confirmation: associated title/description, Cancel focused first, trigger focus restored on close, and dismissal blocked while a request runs. |
| `shell.tsx` | Keyboard-accessible narrow-screen navigation drawer; Sidebar’s Sheet/offcanvas branch stays unmounted. |

`useToast` uses the shared shadcn-style Sonner wrapper with native rich colors, top-right positioning and a localized close button inset on the right. A bottom countdown line follows the duration, pauses with hover, pointer interaction and page visibility, and stays hidden for reduced motion. The desktop width is 300px; mobile sizing follows Sonner. Success copy states the result briefly; failure messages retain the cause and next action. Success messages dismiss after four seconds and failures after eight seconds. The wrapper follows the document theme. Field validation and persistent configuration conflicts remain inline; errors inside native modal dialogs remain in the dialog so they stay visible. Sheet remains a Sidebar dependency and uses the same nonce authorization.

### UI checks

`npm run check` includes `web/scripts/check-ui.mjs`. It scans `src/styles.css` and non-test TSX files under `src` for:

- Global `[data-slot=…]` component overrides, `.auth`, `.wizard`, `.spinner` rules and global select rules.
- Raw `<select>` outside `components/ui/native-select.tsx` and raw `<dialog>` outside the five existing native components above.
- Native `type="checkbox"`, `space-y-*` and numbered background color classes such as `bg-green-500`.

These checks complement TypeScript, ESLint and component tests. Verify production overlays against the built Core’s CSP when changing their dependencies or behavior.

Folder discovery localizes standard roles reported by IMAP and falls back to exact common-name matches. Translated labels include the original name; custom paths and subscriptions retain their original values. Discovered folders expand by default. App notification tests require an active push pairing and link to App settings. Settings forms use a centered, limited-width layout. Logs show localized summaries with raw attributes under technical details. Diagnostics displays the software version, build commit and platform together, with an explicit label for missing commit metadata.

The sidebar footer shows the signed-in administrator with an account menu. Preferences use the same compact icon-and-label menus as the login screen for language and theme. The first notification configuration uses the current console language; saved notification language remains independent.

IMAP quick-fill shows QQ, 163, 126, iCloud and Gmail directly; More mail providers groups Yeah, Yahoo and Alibaba Personal Mail under personal mail, and Tencent Exmail and Alibaba Business Mail under business mail. Presets fill only the host and TLS port 993. Configured credentials show the leave-blank hint as an input placeholder.

### Manual history cleanup

Deliveries provides **Clear records**, which permanently removes all finished records, including failures eligible for manual retry. Queued work, automatic retries, active Native status checks and pending activity updates are retained. The operation covers the full history, beyond the latest 50 rows, and preserves mailbox checkpoints and settings.

Runtime logs provides **Clear logs**, which clears the shared Core memory ring across all levels and mailboxes. Container/stderr logs and exported files remain. New logs continue to arrive; sequence IDs stay monotonic, and polling clients discard entries through `cleared_through`. Both actions require confirmation and administrator authentication; browser requests include CSRF protection.

### Form actions and feedback

Form footers align actions to the right of their own card, with the primary save or finish action last. Tests and other secondary actions precede it. Inline input actions and table-row actions stay with their content. Footers wrap on narrow screens.

Successful operations and transient failures use the shared toast. Validation messages remain beside fields; configuration conflicts retain their reload action in the form. Query failures and ongoing connection problems use persistent page alerts. Native modal failures stay inside the modal so they remain visible above the browser's top layer. Multiple messages use Sonner’s toast stack.

Mailbox, subscription and notification settings lock their editable controls while a save is pending. Subscription locking lasts through revision refresh and completion callbacks. Successful saves apply the returned revision and values; failed saves preserve the draft and restore editing. The overview reports healthy status only after a successful status read. Initial loading shows a loading state, and failed reads show an error even when cached status data exists.

The authorized App list combines management devices, notification capability and available push devices. It shows loading until all required reads finish, shows query failures before an empty state, and refreshes errors and cached data through the existing foreground polling. Creating an invitation requires a successful notification capability read.

### Page loading

Login, first-time setup, the authenticated shell and each console page load through explicit dynamic imports. Shared branding and the connection-budget display live in independent components so public screens and mailbox forms use only their own dependencies. The mailbox schema loads with the mailbox editor; shared form helpers keep login and navigation independent of that validation bundle. Full-screen boundaries center loading and load-failure feedback in the viewport, including the transition after administrator creation. Console page boundaries retain the shell and navigation and display feedback within the content area. Loading is announced in the selected language, and ordinary rerenders preserve drafts. Route changes reset the page boundary.

A failed module download or render shows a localized error and an explicit Reload action. The user can navigate to another page while the shell remains available. Reload fetches the current application document, including current asset hashes after a Core update. Pages use same-origin modules under the existing CSP and immutable hashed-asset cache policy. Runtime failures keep the current document open until the user chooses an action.

### Query state and external updates

Mailbox lists, folder monitoring, deliveries and API tokens distinguish a pending first read, a failed read and a successful empty response. Loading ends when a read fails. Connection budgets appear once folder status is available. Shared mailbox/status errors appear in the shell; page-specific query errors remain beside their content. Cached data and unsaved notification inputs stay visible during a failed refresh, together with the failure notice. A successful retry clears the notice.

Overview includes connecting, reconnecting, authentication-required, paused and stopped folders in Needs attention, as well as folder notices and connection errors. Authentication failures link to mailbox settings; other folder states link to monitoring details. Healthy status requires a successful status read with no attention items.

Mailbox detail headings and limits use the same five-second mailbox query as the list and sidebar. An externally deleted mailbox shows the missing-mailbox state. Pristine mailbox forms adopt newer revisions. Dirty forms retain their inputs, show the revision conflict and offer an explicit Reload; connection tests and saves stay blocked until the conflict is resolved. Active tests, saves and reloads retain their input locks. A successful save installs the returned values and revision in the editor and shared cache before refreshing, so a failed follow-up read preserves the saved revision. Backend revision checks remain authoritative for concurrent writes between refreshes.

Mailbox settings remain editable while the monitoring-status query loads or fails. An unavailable monitoring demand is shown explicitly beside the connection limit; client-side budget checks run once the demand is known, and Core validates every save against current subscriptions. Status recovery updates the budget feedback while preserving the form draft.

Successful mailbox saves merge the returned mailbox into each shared list cache, adding a new mailbox or replacing an existing revision. Other mailboxes and newer revisions remain intact. The current language cache is initialized when needed, so the saved mailbox remains accessible even when the subsequent list read fails. The query failure remains visible until reading recovers; the cache contains the mailbox records already known to the console.

### Folder names

All folder name surfaces use `FolderName`: connection-test results, setup selection, scan selection and confirmation, monitoring tables, and delivery sources and details. Standard roles take localization precedence. Other paths use the server-derived leaf name followed by the full original path in muted text; equal names and paths display once. Accessible selection labels include the full path, and subscription payloads retain it unchanged. Custom leaf names remain literal even when they resemble standard folder names.

The shell shares the owner mailbox response's saved `folder_display` across pages without additional discovery requests. Successful scans refresh that metadata. Old records, deleted mailboxes and paths absent from the latest directory display their original path, with existing whole-name role localization. The console never guesses a hierarchy separator.
