# HTML message content

`POST /api/v1/app/content` accepts the existing `{ "reference": { ... } }` signed message reference. It requires the device's `mwac_` credential and the owner's `content` scope for a currently subscribed folder. Reading is free. Core uses local authorization and IMAP; it makes no Platform or Pro request.

```json
{
  "text": "Plain-text fallback",
  "html": "<p>HTML body</p>",
  "has_remote_images": false,
  "truncated": false
}
```

`text` and `truncated` are required. `html` is optional (clients also accept null). HTML responses always include `has_remote_images`; Core emits false when HTML is omitted. The HTTP response uses `Cache-Control: no-store`.

## MIME and privacy

Core reads `BODYSTRUCTURE` on a short-lived, read-only IMAP connection, selects the display representations, and fetches their transfer-encoded bytes with `BODY.PEEK[part]`. CID images are fetched from the current message only when the sanitized presentation references them. Ordinary attachments, unused alternatives and unreferenced images are skipped. The total mailbox message may be larger than 1 MiB. The mailbox's unread flags and monitoring cursor stay unchanged. MIME transfer encodings and character sets are decoded. `multipart/alternative` selects the last supported plain and HTML representations. A `multipart/related` presentation uses its `start` root or first part; its CID namespace belongs to that presentation. Named attachments and forwarded messages stay outside the displayed body. HTML-only messages receive a readable text fallback with paragraphs, lists and safe links.

The HTML allowlist keeps common formatting, tables, validated inline and scoped stylesheet CSS, and absolute HTTP(S)/mailto links. Scripts, event handlers, forms, embedded documents, SVG/MathML, redirects, external styles/fonts, audio/video and prefetch hints are removed. The passive CSS subset and its degradation rules are defined below.

Safe absolute HTTP(S) `img` sources, `img`/`picture` source sets and CSS backgrounds remain in HTML. URLs with credentials, relative URLs and other schemes are removed. `has_remote_images` reflects retained image references, including conditional source sets and stylesheet backgrounds in hidden or currently unmatched rules; ordinary hyperlinks do not set it. Core performs no image fetch, DNS probe, proxying or filesystem lookup. The iOS reader blocks remote images until the user permits loading in that reading session or enables its global preference.

CID references resolve only within the current related MIME presentation. PNG, JPEG, GIF and WebP images are checked against their declared MIME, decoded for validation and embedded as data URLs. Duplicate/missing IDs, SVG, damaged images, invalid image transfer encodings, format mismatches and sender-supplied data URLs are removed. An invalid CID resource leaves the readable message available. Alternative text remains available. Resources from sibling presentations and previous requests are inaccessible. Body text, HTML, embedded images and image URLs remain in request memory; they are excluded from storage, notifications, logs and diagnostics.

## Body boundary and root inheritance

The `html` field is one complete sanitized fragment. Its leading generated `style` element and two nested containers form one presentation:

```html
<style>/* validated rules scoped to this message */</style>
<mailwake-html data-mailwake-root="" data-mailwake-scope="mw-opaque-">
  <mailwake-body><!-- sanitized message nodes --></mailwake-body>
</mailwake-html>
```

The example abbreviates the generated CSS and opaque scope. Clients retain the complete fragment, including these containers, attributes and stylesheet. Core replaces `html` and `body` with `mailwake-html` and `mailwake-body`; inline styles, safe class/ID, `dir`, `lang` and decorative attributes remain on the corresponding container. This preserves the separate inheritance levels for authored fonts, text colors, background colors, alignment and direction. `body bgcolor` and `body text` become zero-specificity presentational hints before author styles. The generated block-display defaults have zero specificity and respect native `hidden` states. ARIA hiding continues to affect accessibility only.

`dir` accepts `ltr`, `rtl` and `auto` case-insensitively. Values with surrounding whitespace and other invalid values are omitted, preserving HTML's undefined/inherited state. `lang` preserves an empty language reset or an ASCII language tag of up to 64 bytes: a 1–8 letter primary tag followed by 1–8 alphanumeric subtags. Unsupported language tags are omitted. Both attributes inherit through the original ancestry.

Root `bgcolor` and `text` use HTML's [legacy color rules](https://html.spec.whatwg.org/multipage/common-microsyntaxes.html#rules-for-parsing-a-legacy-colour-value) within the existing color allowlist: 3–8 hexadecimal digits after `#`, or 1–24 ASCII letters. Named colors retain their meaning; other admitted values convert to opaque RGB. `transparent` supplies no legacy color hint, so inheritance applies. Authored CSS colors keep CSS syntax and precedence, including alpha colors and `transparent`.

The containers represent email content within the client's document. The client owns viewport metadata, host canvas and readable default foreground/background colors. Authored colors and backgrounds remain paired at their original levels; the client should let unstyled descendants inherit. Whole-document canvas propagation, browser default body margins, unsupported selectors/properties and root-font units are outside this embedded subset. In particular, declarations using `rem`, `rex`, `rch`, `rcap`, `ric` or `rlh` are discarded as a unit: those lengths would refer to the host document root. Earlier supported declarations or inherited values then apply. Element-relative `em`, percentages and ordinary pixel declarations continue to work.

## Responsive styles and passive visibility

Inline CSS and `style` elements use the existing CSS parser and property/value allowlists. A stylesheet's type must be absent, exactly empty or an ASCII case-insensitive match for `text/css`; surrounding whitespace makes it ineligible for both rule collection and default-set selection. Its optional `media` attribute uses the same conditions as `@media` and is preserved as an enclosing media block.

Multiple active stylesheets retain source order. Core resolves embedded stylesheet sets before merging: untitled sheets remain active, and titled sheets match the default set by exact title. The first nonempty title on a CSS style element in the active HTML source tree selects that set; nonempty `meta http-equiv="default-style"` declarations update it in source order. Template and foreign subtrees are excluded. Media/CSS filtering preserves this selection, including when the preferred sheet is empty or discarded. Inactive sets are removed before URL/CID resolution. The selection follows the [CSSOM stylesheet-set rules](https://drafts.csswg.org/cssom/#css-style-sheet-set); the output contains only the selected rules, so host-document stylesheet sets cannot change them. Metadata elements and external stylesheets remain excluded from the output; script-driven or user-selected alternate sets are outside this static subset.

Unknown at-rules, including `@import`, `@font-face`, `@supports`, layers, keyframes and container queries, discard their entire contents. Nested supported `@media` blocks retain their original boundaries. Inline style attributes accept top-level declarations; every at-rule, including `@media`, is discarded with its contents before URL/CID resolution. CSS nesting inside a style rule is unsupported; following ordinary declarations remain eligible. Inline attributes recover invalid qualified-rule text at the next top-level semicolon. Custom-property values are discarded through their semicolon, including any value blocks.

Supported selectors are allowlisted HTML element names, `html`, `body`, `*`, `.class`, `#id`, `:root`, compounds, comma lists, and descendant/child/adjacent/general-sibling combinators. Attribute selectors, other pseudo-classes/elements, namespaces and escaped identifiers discard the entire rule, including a mixed supported/unsupported selector list. Class and ID names use ASCII letters, digits, `_` and `-`, start with a letter/underscore or a single hyphen followed by a letter/underscore, and contain at most 64 bytes. Unsupported class tokens and IDs are removed.

Core prefixes retained classes and IDs with an opaque message scope. Supported selectors match the original parsed tree, including elements later discarded or unwrapped. Core binds the resulting retained targets to generated `data-mailwake-match` tokens. Moving a style element or removing a sibling therefore preserves the original child, descendant and sibling matches. Identical selectors share a binding within the message.

Each emitted selector starts with `:where([data-mailwake-match~="opaque-token"])`, selecting only Core-generated targets with zero specificity. Core then appends one unconditional `:is(#_,*)`, `:is(._,*)` or `:is(mw,*)` for each ID, class/pseudo-class or type unit in the original selector. The universal alternatives match every bound target; these simple units carry the original specificity without evaluating structural relationships again in the browser. Each member of a comma list keeps its own specificity. Media conditions, source order, inline precedence and `!important` remain native browser behavior. The [Selectors Level 4 specificity rules](https://www.w3.org/TR/selectors-4/#specificity-rules) define both constructs. Sender-supplied binding attributes are discarded.

These bindings describe a static email snapshot; Core never rewrites matching from the cleaned tree. Clients preserve the generated attributes when embedding, refreshing or replacing image elements. They may change image URLs and reader presentation, while keeping the bound element identities and authored class/ID relationships intact. Scope/token formats are implementation details. The reader uses standards mode and supports `:where` and `:is`; whole-document quirks-mode matching is outside this subset.

The property allowlist covers:

| Group | Properties |
| --- | --- |
| Color/background | `color`, `background`, `background-color`, `background-image`, `background-position`, `background-repeat`, `background-size` |
| Borders/tables | `border`, `border-top/right/bottom/left`, `border-color/style/width`, `border-collapse/spacing/radius`, `table-layout` |
| Layout/spacing | `box-sizing`, `display`, `float`, `clear`, `width/height`, `min-width/height`, `max-width/height`, `margin` and its four sides, `padding` and its four sides, `vertical-align` |
| Text | `font`, `font-family/size/style/weight`, `line-height`, `letter-spacing`, `text-align/decoration/indent/transform`, `white-space`, `word-break/wrap`, `overflow-wrap`, `list-style-type/position`, `direction` |
| Passive visibility | `opacity`, `visibility`, `overflow`, `overflow-x`, `overflow-y` |

`visibility` accepts `visible`, `hidden` and `collapse`; overflow accepts `visible`, `hidden`, `clip`, `scroll` and `auto` (one or two values for the shorthand); direction accepts `ltr` and `rtl`. These also accept `inherit`, `initial`, `unset` and `revert`. Other values are dropped. Hidden preview text, nested hiding, clipping and media-selected blocks remain in the DOM for the browser to resolve. A descendant `visibility:visible` follows native CSS visibility inheritance; a hidden ancestor's `display:none` still removes its entire layout subtree.

The existing passive value grammar accepts literals, dimensions, percentages, color/font strings, arithmetic punctuation, `rgb/rgba/hsl/hsla`, `calc/min/max/clamp`, and linear/radial/repeating gradients. Browser CSS validation resolves property-specific literal syntax. Escapes, variables, unknown functions, active properties and raw-text delimiters are rejected. URLs are permitted only in `background`/`background-image` and pass the same URL/CID checks as images. Supported declarations after a malformed declaration remain eligible; incomplete accepted blocks, allowlisted functions and strings close at end of input. Core extracts a trailing `!important` token pair as declaration priority before serializing the value, then emits the priority after the closing delimiters. This preserves WebKit EOF recovery for incomplete functions while keeping quoted text and priority tokens inside explicitly closed functions within their original values. Core serializes the closing delimiters before joining declarations or style elements, so EOF recovery stays within its original input. Lexical container boundaries isolate error recovery: a malformed final rule inside a media block leaves the following sibling outside that block. Strings, comments and URLs retain their token boundaries. Mismatched or unexpected closing delimiters discard that CSS input. Unsupported declarations/rules fall back through the remaining cascade within their original containers.

### Media conditions

The finite [media-query subset](https://www.w3.org/TR/mediaqueries-4/#mq-syntax) accepts comma-separated alternatives of positive features joined by `and`, optionally prefixed with `all`, `screen`, `print` or `only` plus one of those types:

| Feature | Accepted values |
| --- | --- |
| `width`, `height`, and their `min-`/`max-` forms | Nonnegative decimal `px`, `em`, `rem`, or unitless `0` |
| `resolution`, `min-resolution`, `max-resolution` | Positive decimal `dpi`, `dppx`, `dpcm` |
| `orientation` | `portrait`, `landscape` |
| `prefers-color-scheme` | `light`, `dark` |

Numbers are at most 100,000. Conditions normalize ASCII whitespace and case. Negation, range comparison syntax, `or`, unknown features, escaped identifiers and functions are unsupported. One unsupported alternative discards the entire condition and its stylesheet/source container. Media-query `em`/`rem` keep their standard initial-font interpretation, independent of the email's authored root font. Width queries use the actual reader layout viewport.

## Responsive image selection

Core retains `picture`, its original direct `source` children and `img` in their original order. Sources under other original parents are discarded. Unsupported wrappers directly inside a picture become attribute-free spans, preserving the boundary between its sources and nested fallback images. Other unsupported wrappers keep the usual unwrapping behavior. The client preserves these parent/child relationships when replacing image nodes. It validates each source's conditions before resolving its candidate URLs or CID bytes:

| Attribute | Supported behavior |
| --- | --- |
| `source media` | The media subset above; absent/empty means unconditional. Unsupported conditions remove the entire source. |
| `source type` | Absent/empty, `image/png`, `image/jpeg`, `image/gif` or `image/webp`; normalize ASCII whitespace/case. Parameters and other types remove the entire source so the next supported source or fallback `img` can apply. |
| `img/source sizes` | A comma-separated list of supported media-feature conditions followed by a nonnegative length, with an optional final unconditional length. Media types are excluded here. Lengths support `px`, `em`, `rem`, `vw`, `vh`, `vmin`, `vmax`, unitless `0`, or flat `calc` sums/differences of up to eight such terms. Percentages, `auto`, nested functions and unknown conditions are unsupported. Empty/absent follows the browser default. |
| `img/source srcset` | Retain safe HTTP(S)/same-presentation CID candidates and supported positive width (`w`) or density (`x`) descriptors. Invalid URLs/descriptors remove their candidates. Browser selection uses the retained conditions and the reader viewport/pixel density. |
| Invalid `sizes` | Remove the entire `source`, or for `img` remove both `sizes` and `srcset` while retaining a safe `src` fallback. Conditional source sets stay conditional. |
| Image metadata | Preserve safe width/height, explicit empty or nonempty alt, and existing decorative semantics. `media`/`type` on `img` or `picture` have no supported selection role and are omitted. |

Candidate types and sizes are selection hints, following the [HTML image source-size contract](https://html.spec.whatwg.org/multipage/images.html#source-size-list). CID bytes still undergo MIME and raster validation. Core never resolves a remote candidate during parsing. The client owns resource permission, loading and caching for every candidate and CSS background, including images selected only after viewport/theme changes.

### iOS integration checklist

- Keep the generated style, both root containers and `data-mailwake-match` attributes when embedding or refreshing the response; preserve bindings when replacing image nodes. Cached HTML from earlier Core builds needs a fresh content request to receive the new contract. The JSON response schema is unchanged.
- Give media queries the actual reader width; verify 353px and both sides of the 630px breakpoint, then a wide viewport. Avoid global overrides of authored widths, display, overflow, margins or image heights. Preserve explicit dimensions and image aspect ratios.
- Let font, language, direction and paired colors inherit through the containers. Supply readable defaults at the host boundary and check authored and unstyled emails in light/dark mode and larger text settings. Theme handling belongs to the client.
- Preserve source order, media/type/sizes/srcset and decoration when routing image URLs through permission/cache handling. Cover CID, remote backgrounds, mixed source sets and unsupported-type fallback. Retained hidden references still require the same network permission.
- Verify the four desktop/mobile switches, centered 610px layout, narrow padding, stacked columns, hidden preview and clipping with the synthetic fixture. Then validate body refresh, reopen, offline cache use and permission revocation in the actual iOS reader.
- The text fallback keeps the existing MIME/plain-text extraction algorithm. It is viewport-independent and does not evaluate CSS; HTML-only alternatives can include both desktop and mobile text. Core parser tests and desktop WebKit comparisons establish the Core contract; final iOS rendering and real-provider acceptance are separate checks.

## Decorative and accessibility attributes

The sanitized `html` preserves the following attributes on retained allowlisted elements. This extends the HTML contract within the existing response fields; the JSON schema and plain-text fallback algorithm stay unchanged.

| Attribute | Accepted input and sanitized output | Client interpretation |
| --- | --- | --- |
| `img alt` | Preserve the supplied value, including empty and whitespace-only strings. Both `alt=""` and a bare `alt` serialize as `alt=""`. An absent attribute stays absent. Other elements discard `alt`. | Empty alt explicitly supplies an empty text alternative. Missing alt supplies no description. Preserve this distinction when classifying images. A named image such as `alt="Claude"` retains its name. |
| `aria-hidden` | Accept `true`, `false` and `undefined`, normalize case and surrounding ASCII whitespace, and discard other values. | `true` hides the accessibility subtree. A descendant `false` cannot restore accessibility beneath a hidden ancestor. `false` and `undefined` also preserve any native hiding such as `hidden`. ARIA alone leaves visual images, background layers and layout intact. |
| `hidden` | Preserve `until-found` case-insensitively. Normalize every other present value to `hidden=""`, following HTML's empty and invalid-value defaults. This includes bare `hidden`, `hidden="hidden"`, `hidden="false"` and padded `" until-found "`. Absence stays absent. | Apply native HTML hidden semantics, including ancestor effects and find-in-page behavior for `until-found`. The sanitizer retains the subtree and lets the renderer resolve native behavior and safe CSS. |
| `role` | Accept only `none`, `presentation`, or ASCII-whitespace-separated lists containing these aliases. Normalize case and separators. Discard empty values and any list containing another token, including interactive or unknown roles. | These aliases remove the element's implicit role. A layout table's presentation role leaves its descendant image descriptions meaningful. The renderer owns required-child role inheritance and native conflict resolution, including focusable links. |

Attributes stay on their original retained elements. Core neither propagates decorative metadata onto descendant images nor removes images based on these attributes. The existing element allowlist still governs retained containers; unsupported wrappers discard their attributes and follow the structural rules under [Responsive image selection](#responsive-image-selection). Duplicate attributes use the first occurrence, and namespace lookalikes are discarded. Other ARIA attributes and role values remain outside this allowlist.

Script, event-attribute and URL restrictions continue to apply alongside the CSS subset above. Remote references in decorative or hidden content still contribute to `has_remote_images`; classification never grants network permission. Core performs no image requests. A renderer that extracts images or backgrounds separately must carry the relevant ancestor semantics while preserving the visual/accessible distinction. Previously cached sanitized HTML requires a fresh content response to receive these attributes.

The semantics follow [WAI-ARIA aria-hidden](https://www.w3.org/TR/wai-aria-1.2/#aria-hidden), [presentation and none roles](https://www.w3.org/TR/wai-aria-1.2/#presentation), and [HTML hidden states](https://html.spec.whatwg.org/multipage/interaction.html#the-hidden-attribute). Core's role allowlist deliberately covers the decorative email cases above.

## Limits and failures

| Boundary | Behavior |
| --- | --- |
| Total read deadline | 20 seconds, including connection, fetching and parsing; timeout returns `504 message_timeout` |
| Selected MIME payload | A shared 1 MiB budget for transfer-encoded plain text, HTML and referenced CID bytes (including base64/quoted-printable overhead); oversize returns `413 message_too_large`. Ordinary attachments do not consume this budget. |
| Text | 64 KiB, UTF-8-safe clipping sets `truncated: true` |
| HTML including data URLs | 256 KiB; oversize omits the entire HTML representation |
| HTML traversal | Depth 256 and 20,000 nodes in the original parsed tree, including discarded subtrees; excess omits HTML |
| CSS aggregate per message | 64 KiB input, 32,768 lexer tokens, 512 inspected style rules and 4,096 inspected declarations across stylesheets/inline styles; excess omits HTML |
| CSS nesting | At most eight delimiter/container levels; excess omits HTML |
| Selectors/classes | At most 32 selectors per list, 128 parser tokens and 16 compounds per selector; unsupported rules are discarded. At most 32 class tokens per element; excess omits HTML. |
| Selector matching | At most 2,000,000 work units per message, charging each original element once per compound plus its class/ID predicates; repeated selectors reuse bindings. Matching uses linear passes without ancestor backtracking; excess omits HTML. |
| Conditions/source sizes | Media input at most 1,024 bytes, eight alternatives and eight `and` parts each; sizes input at most 2,048 bytes and 16 entries; excess follows unsupported-condition degradation |
| CSS output | Each constructed block and the combined stylesheet share the 256 KiB HTML ceiling; encoded CID repetition, selector expansion and generated binding attributes count toward final output |
| Encoded JSON | 512 KiB, including JSON escaping; oversize omits HTML |
| CID image | At most 4096 pixels on either axis and 4,194,304 total pixels; invalid/oversize dimensions remove the reference |
| Embedded image bytes | A referenced image larger than 192 KiB cannot fit its base64 representation in the HTML budget and causes HTML fallback |
| MIME structure | Maximum depth 32 and 256 parts; invalid structure returns `422 message_content_invalid`. Multipart roots selected by a related `start` parameter may require a MIME-header read, bounded to 64 KiB and charged to the same shared read budget. |

HTML fallback returns bounded text, `has_remote_images: false` and `truncated: true`. HTML and data URLs are never byte-clipped. Bad MIME/character encodings return `422 message_content_invalid`. Existing scope (`403 content_permission_required`), revocation (`401`), resource/reference (`404 message_not_found`) and connection/budget (`503 message_unavailable`) behavior remains in force.

## Real-mail integration steps

Use the Core candidate being accepted and a compatible iOS build with HTML reading. Record both source revisions, build versions, provider and device with the results. Earlier rc9/rc10 names belong to the retired development release sequence; use the current release or candidate revision. Use synthetic mail content in a real test mailbox, and keep the mailbox credentials in Core's normal encrypted settings.

1. Configure a dedicated subscribed folder and authorize the iOS device with `content` scope. Keep the iOS **Automatically Load Remote Images** setting off. Obtain new message references from normal Native notifications; use the references exactly as received. The reading endpoint is free; notification delivery keeps its existing Relay entitlement requirement.
2. Send separate messages covering plain text; HTML only; plain/HTML alternatives; nested alternative/related with CID PNG/JPEG/GIF/WebP; and GBK or UTF-8 with base64/quoted-printable. Include tables, inline formatting, and a distinct plain fallback. Add SVG, missing CID and a named attachment as negative cases. Include HTML tags for a script, event handler, iframe and form in a synthetic message to verify removal.
3. Add uniquely named HTTP(S) image paths on a server you control: an `img`, two `srcset` candidates and a CSS background. Use a URL containing a unique per-message token. Keep server access logs visible, and disable any mailbox web preview or third-party scanner that would fetch those URLs during the measurement.
4. Before opening the notification, verify that the message is unread using your provider's flag view or an IMAP `UID FETCH <uid> (FLAGS)` command. Read it through the iOS notification detail. Confirm HTML layout, immediate CID display, blocked remote images and the image permission indicator. Image-server traffic attributable to Core and the blocked iOS session must remain zero. Inspect the response locally if needed: required fields, correct remote flag, `no-store` and total response size. Keep inspection out of shared logs and delete temporary captures afterward.
5. Tap **Load Images**. Confirm that requests now arrive from the iOS reading session for retained images. Navigate away and reopen; the per-message permission resets. Repeat with global automatic loading enabled, then restore the preferred setting. Recheck IMAP flags: content downloads preserve them; an App implementing the read action marks the opened body as read. Test the action separately from downloading content, repeat it, and reopen cached content. A read-action failure must leave the body available with a retry option.
6. Send an HTML body over 256 KiB with selected payloads within 1 MiB, then a message whose escaped JSON exceeds 512 KiB. Verify a readable text response with omitted HTML and `truncated: true`. Attach a multi-megabyte PDF to a small body and confirm that reading succeeds. Add a large unreferenced image or unused alternative and confirm the same behavior. Send selected body/CID payloads whose combined transfer-encoded bytes exceed 1 MiB and verify `413 message_too_large`. Revoke `content` permission or the device credential and verify `403` or `401` respectively.
7. Check Core delivery history, diagnostics and logs for the synthetic body/CID/image URL markers: they must be absent. Delete the test messages and any local response captures when finished.

## Automated verification

The content tests cover representations, encodings, sanitization, image flags, CID formats/isolation, budgets and cancellation. Local TLS IMAP tests exercise selective MIME part fetching, multi-megabyte attachments, skipped unused alternatives/CIDs, exact and cumulative read budgets, absence of image requests, unread flags, missing messages, invalid UIDVALIDITY, moves/deletion, oversize and cancellation. HTTP tests use real owner/device authorization plus the parser: an observable image server and HTTP/DNS traps record zero outbound image operations through response serialization, and an escaping-heavy response verifies the 512 KiB limit. Runtime and native tests exercise signed reference/account boundaries.

The synthetic decorative-email fixture covers a named logo, an ARIA-hidden animation with ordered GIF/PNG background layers, presentation tables, empty-alt social images and an undescribed image. Additional regressions cover bare/absent/duplicate alt, ancestor declarations, invalid values, hidden defaults, unsafe markup and response serialization. The private source email, addresses and login links are excluded from fixtures.

The [synthetic responsive-email fixture](../internal/content/testdata/responsive-email.html) retains the diagnosed template's 610px table, 630px breakpoint, four desktop/mobile switches, padding, centering, stacked columns and clipped content. Its text, links and image identifiers are synthetic; CID images are generated locally for verification. Responsive regressions also cover original-tree selector bindings across relocated/removed/unwrapped nodes, selector scoping/priority, EOF function/string closure, root colors and inheritance, conditional image fallback and original picture relationships, embedded stylesheet-set selection, EOF priority recovery, MIME isolation, parser recovery, malicious CSS and aggregate/output budgets. HTTP/DNS traps and selective IMAP tests include stylesheet resources and discarded conditional candidates.

These fixtures validate Core behavior. Real-provider MIME differences and the Core-to-iOS rendering flow require the manual steps above.

The additional parser and image decoder licenses are bundled in [third-party notices](html-content-third-party.txt).

Published versions and their verification are maintained in [GitHub Releases](https://github.com/mingzaily/mailwake/releases).

## Mailbox read action

The selected mailbox must permit permanent `\Seen` changes. An explicit `PERMANENTFLAGS` list that excludes `\Seen` returns `503 message_unavailable`; `\*` grants keyword creation only. An omitted list follows the all-flags default in RFC 3501 section 6.3.1.

`POST /api/v1/app/content/read` accepts `{ "reference": <message_ref> }` and returns `204 No Content` with `Cache-Control: no-store`. It uses the same device-bound `mwac_` credential, owner `content` scope, signature, account identity and current folder subscription checks as body reading. It is free and has no configuration switch. A successful response means Core observed the target UID with the read flag; the App owns the decision that the user opened the body.

The App calls this endpoint after presenting a readable body, including a locally cached body. Notification delivery, verification-code extraction and `POST /content` alone preserve the existing read flag. Dismissing a notification or opening a preview alone preserves it as well. Local history read/unread actions are separate from this one-way mailbox action.

Core opens one independent writable IMAP connection within the mailbox's existing non-watcher budget and verifies UIDVALIDITY. It fetches the target UID and flags explicitly. An existing message with `\Seen` succeeds immediately, including when permanent write permission is absent. An unread message requires permanent Seen permission (omitted PERMANENTFLAGS uses the protocol default); Core issues `UID STORE <uid> +FLAGS.SILENT (\Seen)` and fetches UID/flags again to confirm the result. STORE update records are optional to this flow; STORE OK alone cannot prove existence because servers may ignore nonexistent UIDs. The operation preserves other flags and monitoring progress and has one 20-second deadline.

An absent UID, changed UIDVALIDITY or invalid reference returns `404 message_not_found`. Only an explicit `NONEXISTENT` folder rejection establishes a missing folder; other selection rejections, unavailable flags, a message still unread after STORE, or an exhausted connection budget return `503 message_unavailable`. A missing scope returns `403 content_permission_required`, an invalid or revoked credential returns `401 unauthorized`, and a timeout returns `504 message_timeout`. The same folder-error distinction applies to body fetching. Provider explanations stay outside public responses and logs.

The [App request diagnostics](api.md#app-request-diagnostics) identify the failing stage and distinguish `already_read` from `marked_read`. A locally cached body remains usable when its remote reference expires. The App owns local presentation and retry behavior.

Local tests cover unread/repeated reads, omitted STORE updates, permission rejection, missing flag data, split FETCH responses, disappearance during STORE, unchanged flags after STORE, UIDVALIDITY changes, moves/deletion after caching, timeouts, other flags and watcher progress. Real Gmail acceptance remains separate: use a synthetic message; open it unread and already read, then move/delete it and reopen the cached body. Export diagnostics immediately after each attempt. Record the Core revision, UTC time, request ID, stage, result and error/response codes; exclude credentials, raw IMAP exchanges, message bodies and signed references. These tests verify the current behavior; they do not establish the cause of an earlier production 404.
