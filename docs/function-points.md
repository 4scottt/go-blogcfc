# Function points

The test contract of go-blogcfc: every row is one thing BlogCFC does that
the rewrite must do too, with the BlogCFC source it was read from and the
kind of test that proves it. `TestFunctionPointsCovered` reads the ids
from this file and fails when any id has no test whose name contains it
(`TestFP_P01_…`). Rows are added as milestones land; a row is never
removed, only marked dropped with a reason.

Legend for the *test* column: **u** unit (pure Go), **h** handler test
with `httptest` against a real MariaDB (`TEST_DSN`), **w** the Playwright
walk in `scripts/walk.mjs`, **g** golden HTML/XML.

### P. Public site

| Id | Function point | BlogCFC source | Test |
|---|---|---|---|
| P01 | Home lists the newest `maxentries` released, non-future entries, newest first | index.cfm, getEntries | h |
| P02 | Drafts and future-dated entries are hidden; `?adminview=1` as admin shows drafts | getmode.cfm | h |
| P03 | Pagination with `startRow`, links keep the SES path and query | index.cfm | h,g |
| P04 | Entry view by `/Y/M/D/alias`, by `?mode=entry&entry=`; 404 otherwise | parseses.cfm, makeLink | h |
| P05 | Generated permalinks match BlogCFC's shape (alias → date path, no zero padding, in the blog's zone; no alias → id form) | makeLink | u |
| P06 | Category listing by alias and by `?mode=cat&catid=` including comma lists | makeCategoryLink | h |
| P07 | Month and day archives | parseses.cfm | h |
| P08 | Posted-by listing | makeUserLink | h |
| P09 | List view shows `body` and a `[more]` link when `morebody` exists; entry view shows both | index.cfm | h,g |
| P10 | Entry header: title link, month/day badge, author, category links, comment count anchor | index.cfm | g |
| P11 | Entry footer metadata line: posted date and time, views, comment count, print link, download link when enclosure | index.cfm | g |
| P12 | Views counted once per visitor session per entry; print view does not count | logView, session.viewedpages | h |
| P13 | Related entries block on a single entry, bidirectional, no future entries | getRelatedBlogEntries | h |
| P14 | Comments list with Gravatar (md5 of lowercased email, size 64, default image), paragraphs and linkified URLs | index.cfm, ParagraphFormat2, replaceLinks | h,g |
| P15 | "Comments not allowed" message when the entry disallows them | index.cfm | h |
| P16 | Empty states: no entries, no entries for criteria | index.cfm, rb | h |
| P17 | Static page with layout, without layout, unknown alias redirects home | page.cfm | h |
| P18 | Print view renders body and morebody, code blocks as `<pre>` | print.cfm | h,g |
| P19 | Search: term across title/body/morebody, optional category, excerpt window −250/+500 with highlights, pagination, `/search/{term}` shortcut | search.cfm | h |
| P20 | Search logs to search stats unless paging | logSearch | h |
| P21 | Contact form validates name, email, comments, challenge; mails owner with remote address | contact.cfm | h |
| P22 | Email-this-entry validates, mails recipient, cc owner, includes permalink and notes | send.cfm | h |
| P23 | Slideshow: one image per page, caption, prev/next, clamped index | slideshow.cfm | h |
| P24 | Enclosure download logs (entry, ip, referrer, agent, online flag) and redirects | download.cfm | h |
| P25 | robots.txt, sitemap.xml (root hourly 0.8, entries with lastmod, pages weekly 0.5) | googlesitemap.cfm | h,g |
| P26 | Layout: title with additional title per mode, meta description/keywords, RSS alternate link, frame buster | layout.cfm | g |
| P27 | Legacy `.cfm` URLs 301 to canonical (index.cfm, index.cfm/Y/M/D/alias, rss.cfm, page.cfm/alias, search.cfm, admin/index.cfm) | — | h |
| P28 | Responsive at phone width, no horizontal scroll | (mobile skin) | w |

### D. Pods (sidebar)

| Id | Function point | Source | Test |
|---|---|---|---|
| D01 | Calendar: localized month grid, week start per locale, active days link to the day archive, today highlighted in the blog's zone, prev/next month links | pods/calendar.cfm | h,g |
| D02 | Archives by subject: category, entry count, per-category RSS link | pods/archives.cfm | h |
| D03 | Monthly archives (5 years) with counts | pods/monthlyarchives.cfm | h |
| D04 | Recent entries (5) | pods/recent.cfm | h |
| D05 | Recent comments (5): entry title, name, 100-char excerpt, `[more]` anchor | pods/recentcomments.cfm | h |
| D06 | Search box pod | pods/search.cfm | g |
| D07 | Subscribe pod: validates email, creates token, mails confirmation, "already subscribed" path | pods/subscribe.cfm | h |
| D08 | Tag cloud: categories with ≥10 entries, five size classes by the 25-step scale | pods/tagcloud.cfm | u,h |
| D09 | RSS button pod, pages pod | pods/rss.cfm, pages.cfm | g |
| D10 | Pod visibility and order from settings; defaults calendar, subscribe, recent comments, recent, archives | pods.xml, getpods.cfm | h |

### F. Feeds

| Id | Function point | Source | Test |
|---|---|---|---|
| F01 | RSS 2.0 channel fields (title, link, description, language from locale, pubDate, lastBuildDate, generator, editor/webmaster) and per item title, link, description, categories, pubDate, guid, author | generateRSS | g |
| F02 | `mode=short` 250-char stripped excerpt with `...`; `mode=full` full body | rss.cfm | h |
| F03 | Cap at 15 items; filters `mode2=day|month|cat|entry` | generateRSS | h |
| F04 | Enclosure element with url, length, type; iTunes and media tags when audio/mpeg | generateRSS | g |
| F05 | RSS 1.0 (`version=1`) RDF shape with dc:date and dc:subject | generateRSS | g |
| F06 | ETag and Last-Modified; 304 on If-None-Match / If-Modified-Since | rss.cfm | h |
| F07 | Only released, non-future entries appear | rss.cfm | h |

### C. Comments and subscriptions

| Id | Function point | Source | Test |
|---|---|---|---|
| C01 | Add comment: required name and comment, valid email, valid website if given, `http://` placeholder stripped | addcomment.cfm | h |
| C02 | Sanitising and truncation (name, email 50; website 255), HTML escaped | addComment | u |
| C03 | Refused when the entry disallows comments or does not exist | addComment | h |
| C04 | Spam word list blocks comments whose text, name, website or email contain a term (case-insensitive); IP block list with `*` wildcards | addComment | u,h |
| C05 | Honeypot, minimum fill time, maximum URL count, challenge; all skipped when logged in | cfformprotect, captcha | h |
| C06 | Moderation: `moderate=yes` stores unmoderated; unmoderated comments hidden from lists and counts | addComment, getComments | h |
| C07 | Remember-me cookies for name, email, website | addcomment.cfm | h |
| C08 | `subscribe` flag; unchecking retro-clears earlier subscriptions by that email on the entry | addComment | h |
| C09 | Thread subscription without a comment (`subscribeonly`), excluded from listings and counts, skips spam checks | addsub.cfm | h |
| C10 | Notification mail to thread subscribers and owner: `%unsubscribe%` per recipient, owner gets Delete (killcomment) and Approve links, `commentsFrom` override, admin-only when moderating | notifyEntry | h |
| C11 | One-click kill by token and approve by id from the mail links | index.cfm | h |
| C12 | Approving in moderation re-notifies subscribers (not the admin) | moderate.cfm | h |
| C13 | Blog subscription double opt-in: token, confirm sets verified, unknown token handled | addSubscriber, confirmSubscription | h |
| C14 | Unsubscribe by commentID+email (thread) and by email+token (blog) | unsubscribe.cfm | h |
| C15 | New released entry mails verified subscribers (title, url, author, body, continued link, unsubscribe) and marks `mailed`; skipped when `sendemail` is no | mailEntry | h |
| C16 | Scheduled release: a released entry with a future date is mailed by the sweep when its time comes, once | cfschedule, notify.cfm | h |
| C17 | Release pings: each `pingurls` entry is GET-requested on release (fake server) | ping.cfc | h |

### A. Admin

| Id | Function point | Source | Test |
|---|---|---|---|
| A01 | Login with username/password, 500 ms delay on failure, logout, all admin routes gated | admin/Application.cfc | h,w |
| A02 | Dashboard: version, top entries by views in the last 7 days, `?reinit=1` banner | admin/index.cfm | h |
| A03 | Entries list: keyword filter, sort by column and direction, page size `maxentriesadmin`, bulk delete, View link with `adminview` | admin/entries.cfm | h |
| A04 | Non-ReleaseEntries users see only drafts | admin/entries.cfm | h |
| A05 | Entry create and edit: title, body with `<more/>` split and re-join (leading `<more/>` rejected), categories, new category inline (AddCategory role), alias auto from title, posted parsed in the blog's zone | admin/entry.cfm | h,w |
| A06 | Entry flags: allowcomments, sendemail, released (read-only without ReleaseEntries); a draft released with a past date gets `posted` = now | admin/entry.cfm | h |
| A07 | Enclosure upload with unique names, manual filename, delete, mimetype and size recorded | admin/entry.cfm | h |
| A08 | iTunes fields (subtitle, keywords, summary, duration) | admin/entry.cfm | h |
| A09 | Related entries picker: filter by text and category via `/admin/proxy` JSON, save set, delete-then-insert | admin/entry.cfm, proxy.cfm | h |
| A10 | Preview renders without saving | admin/entry.cfm | h |
| A11 | Crash-recovery draft of title and body kept client-side for new entries | admin/entry.cfm | w |
| A12 | Categories list and CRUD: name 50, alias auto and validated, duplicate name refused, delete purges links | admin/category.cfm | h,w |
| A13 | Comments list with search on text or name, bulk delete; comment edit (name, email, website, text, subscribed, moderated) | admin/comments.cfm, comment.cfm | h |
| A14 | Moderation queue with live count in the menu, approve, bulk delete | admin/moderate.cfm | h |
| A15 | Subscribers: list with total/verified counts, delete, verify by hand, remove unverified | admin/subscribers.cfm | h |
| A16 | Mail all verified subscribers with a per-recipient unsubscribe block | admin/mailsubscribers.cfm | h |
| A17 | Users list and CRUD (username immutable), password change only when the field changed, role assignment | admin/user.cfm | h |
| A18 | Self-service password change with old-password check | admin/updatepassword.cfm | h |
| A19 | Role checks: Admin implies all; AddCategory, ManageCategories, ManageUsers, ReleaseEntries, PageAdmin gate their screens | isBlogAuthorized | h |
| A20 | Settings page: every fieldset and key of §10, validations (title, base URL, emails, numerics, locale), newline-list round trip, spam list sorted on save | admin/settings.cfm | h |
| A21 | Pages CRUD with alias, showlayout, categories | admin/page.cfm | h |
| A22 | Textblocks CRUD and `<textblock label>` substitution in bodies | admin/textblock.cfm, render | h |
| A23 | Pod manager: show and order | admin/pods.cfm | h |
| A24 | Slideshows: create (name validated), rename, formal name, image upload and delete | admin/slideshow.cfm | h |
| A25 | File manager scoped to `DATA_DIR`: list, upload, download, delete; `..` refused | admin/filemanager.cfm | h |
| A26 | Image upload popup (gif/jpg/png only) and image browser popup that insert an `<img>` into the body | admin/imgwin.cfm, imgbrowse.cfm | h,w |
| A27 | Downloads report by date range | admin/downloads.cfm | h |
| A28 | Stats: general, top views, category stats, top by comments, top categories by comments, top search terms, top commenters; by year | admin/stats.cfm, statsbyyear.cfm | h |
| A29 | `?reinit=1` flushes caches; caches invalidated on entry, comment, page, settings writes | scopecache | h |

### R. Rendering

| Id | Function point | Source | Test |
|---|---|---|---|
| R01 | `<code>` blocks highlighted (Chroma) in `div.code`; `<pre class="codePrint">` escaped in print | renderEntry, ColdFish | u,g |
| R02 | Image enclosures prepend `div.autoImage`; mp3 enclosures render `<audio>` | renderEntry | u |
| R03 | Paragraph wrapping on blank lines (XHTMLParagraphFormat) unless ignored | renderEntry | u |
| R04 | `makeTitle` slugifier: `&` → `and`, entities stripped, non-alphanumerics stripped, spaces → `-`, case kept | makeTitle | u |
| R05 | Strings from the bundle for `locale`; en_US and de_DE; unknown key falls back to en_US | resourcebundle.cfc | u |
| R06 | Dates and month/day names localized; calendar week start | localeUtils | u |

### X. XML-RPC

| Id | Function point | Source | Test |
|---|---|---|---|
| X01 | Codec: XML-RPC ↔ Go values, faults, UTF-8 prolog | xmlrpc.cfc | u |
| X02 | Auth on every call; bad password → fault | xmlrpc.cfm | h |
| X03 | `blogger.getUsersBlogs`, `metaWeblog.getUsersBlogs` | | h |
| X04 | `metaWeblog.getCategories`, `mt.getCategoryList` | | h |
| X05 | `metaWeblog.getRecentPosts` (drafts visible to the author), `metaWeblog.getPost` | | h |
| X06 | `metaWeblog.newPost`, `editPost` with category translation and cache flush | | h |
| X07 | `blogger.deletePost` | | h |
| X08 | `metaWeblog.newMediaObject` writes to enclosures under md5 name, returns url | | h |
| X09 | `mt.getPostCategories`, `mt.setPostCategories` | | h |
| X10 | `parseMarkup` toggle escapes/unescapes `<code>` | | u |

### O. Operations and platform

| Id | Function point | Test |
|---|---|---|
| O01 | Migrations idempotent from empty and from the previous version; seed roles; admin seeded from `ADMIN_PASSWORD` only when no users exist | h |
| O02 | `/health` 200 without auth; 503 when the DB is unreachable; `healthcheck` subcommand exit codes | h |
| O03 | Starts and serves with no `OTEL_*` env; with env set and no collector, no error above debug within the first 60 s | h |
| O04 | HTTP metrics: a request produces an `http.server.request.duration` histogram point in seconds with `http.response.status_code` (in-memory reader) | u |
| O05 | Every link, feed URL and mail URL is built from `BLOG_BASE_URL`, not Host | h |
| O06 | Session cookie is HMAC-signed, HttpOnly, Secure when the base URL is https; tampering logs out | u |
| O07 | Image builds for linux/amd64 and linux/arm64, runs as non-root, `DATA_DIR` writable, under 30 MiB | CI |
| O08 | The acceptance walk (§14) passes with zero console errors, zero page errors, zero ≥400 own-origin sub-resources | w |
