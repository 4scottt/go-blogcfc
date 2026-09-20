# What BlogCFC did and where go-blogcfc differs

BlogCFC 5.9.8 is a ColdFusion blog by Raymond Camden, last released in
2011. go-blogcfc reproduces it in Go: the same features, the same
permalink grammar, the same admin vocabulary, the same settings page, on
one static binary and a MariaDB database.

Reproducing a fifteen-year-old application honestly means saying where
the copy is not a copy. This file is that list. Three kinds of difference
appear: things deliberately changed because the original depended on
something that is gone or unsafe, things dropped for the same reason, and
outright bugs that were fixed rather than faithfully ported. Everything
else is meant to behave as BlogCFC behaved, down to the arithmetic of a
search excerpt and the shape of a permalink.

The test contract in `docs/function-points.md` lists every reproduced
function point with the BlogCFC source file it was read from.

## Kept as BlogCFC does it

Entries with three states — draft, scheduled and live — and the
`<more/>` split. Categories with alias permalinks. Comments with
moderation, per-thread subscriptions and one-click approve and delete
links in the owner's mail. Blog-level subscriptions with double opt-in
and per-comment unsubscribe links. Static pages, textblocks, related
entries, users and the five seeded roles. Search with search statistics,
and the excerpt window computed exactly as `search.cfm` computed it
(250 characters before the match, the same arithmetic after it, ellipses
on a cut). RSS 2.0 with enclosures and iTunes tags, RSS 1.0, the Google
sitemap, the views counter, the SES permalink grammar, pagination. Ten
sidebar pods: calendar, archives by subject, monthly archives, recent
entries, recent comments, search, subscribe, tag cloud, RSS button and
pages — with the tag cloud's `entrycount >= 10` threshold and its
25-step scale. The contact form, email-this-entry, enclosure upload and
download tracking, image upload and browse popups, slideshows, the stats
and stats-by-year reports, the settings screen, `?reinit=1`, the XML-RPC
MetaWeblog, Blogger and Movable Type methods, and the resource bundles
(en_US, de_DE, de_AT, de_CH) with BlogCFC's own keys and strings.

`?adminview=1` still shows drafts and future-dated entries, and still
only to a signed-in user — an anonymous visitor asking for it changes
nothing. It is honoured on the listings and on the entry, email-this and
comment-form paths.

## Changed on purpose

| BlogCFC | go-blogcfc | Why |
|---|---|---|
| `offset`, a naive hour count | `timezone`, an IANA zone; datetimes stored in UTC | correctness: permalink dates, the calendar's today and the editor's `posted` field are all computed in one real zone |
| Image captcha of three characters | An arithmetic challenge; `usecaptcha` and the skip-for-signed-in-authors rule kept | the image captcha was unreadable to a screen reader and trivial to a bot |
| Comment defence calling hosted spam services | Honeypot, signed timestamp, URL count and a word list, all local | same defences, no dependency on a service that may no longer answer |
| A scheduler task per entry, plus an unauthenticated notify page | One sweep a minute over `released = 1, posted <= now, mailed = 0` | no OS scheduler, and no open endpoint that mails on demand |
| PDF print view through the CFML document engine | `/print/{id}`, HTML with a print stylesheet | no PDF engine; the same content, and the browser prints it |
| A separate mobile skin chosen by user-agent sniffing | Responsive CSS on the one layout | the skin was broken as shipped; one layout is one thing to test |
| Flash MP3 player | `<audio controls>` | Flash is gone |
| Pod manager that wrote `.cfm` files | A fixed set of widgets with show/order in settings | uploading code through an admin form is not a feature worth keeping |
| File manager rooted at the web root | Scoped to `DATA_DIR`, traversal refused outright | a file manager over the web root is a shell |
| An error mail on every exception | Structured error logs and traces | the telemetry pipeline is the error channel |
| SHA-512 over salt and password | bcrypt | no import of existing users is planned, so nothing forces the old scheme |
| Several SQL dialects and a `tableprefix` | MariaDB only | one dialect, tested against a real server |
| A `blog` tenant column on every table | Dropped | one blog per deployment |
| Entry flags as Yes/No dropdowns | Checkboxes | a boolean is a checkbox |
| An entry could not be saved without a category | The rule is dropped | it blocked the first entry on an empty blog; the categories field is still there |
| Comment form fields `comments` and `rememberMe` | `comment` and `remember`, with `captcha` and `captchatoken` for the challenge | stable, predictable names for automation |
| Comment bodies stored escaped and printed raw, with every URL-looking string linked | Bodies escaped on the way out, paragraphs formatted as the original did (newlines become `<br />`), and only `http`/`https` links linkified | the original would run a `javascript:` website field on every reader's page |
| Comment list with no count | Header `Comments (N)` and a numbered line per comment | |
| Gravatars shown whatever `allowgravatars` said | `allowgravatars` honoured on the public page | the setting existed and did nothing |
| Contact and email-this put the visitor's address in `From` | Mail is sent from the blog (`failto`/`owneremail`) with the visitor's address in the body | mail from a forged sender is mail that gets dropped |
| Search statistics logged on every page of results | Logged once, on a fresh search | paging through results was inflating the term counts |
| Pings fired on every save of an already-released entry | Pings fire once, on an entry's first release; a scheduled entry pings when the sweep mails it | |
| `sendemail = no` still marked the entry mailed | An unmailed entry stays unmarked; a release with zero subscribers is marked, with a recipient count of 0 | |
| Comment notification's admin-only flag followed the `moderate` setting | It follows the comment's own moderated state | an author's own live comment reaches the thread's subscribers |
| Approving a comment mailed the owner again | Approving tells the thread, not the owner who just approved | |
| The admin comments list hid comments held for moderation | It shows them | the screen that edits a comment should show the comments needing editing |
| Password form fields with ad-hoc names | `password` and `password2` (the original names are still accepted); the button reads `Update` | |
| Settings yes/no keys as free-form text | Two-option selects, labels title-cased without trailing colons | |
| Slideshow captions edited in one combined form | Captions are preserved and shown, but not editable | one form per action; the combined form did several things at once |
| Uploads overwrote a file of the same name silently | Never overwrite: the next free `name-1.ext` is used, and the saved extension follows the sniffed content type | |
| `filebrowse = no` still left the file manager reachable | It is a 403 | |
| The enclosure column held an absolute server path | It holds the file name; the path comes from `DATA_DIR` | a database that names a server's directory layout cannot move |
| Stats counted every comment row | Moderated comments only, subscribe-only rows excluded | the totals disagreed with what the site showed |
| Related-entries picker driven by an endpoint that returned invalid JSON | A multiselect with Filter, Add and Remove over an endpoint that returns real JSON | |
| An unknown entry, category alias, category id or author answered 200 with an empty page | 404 | an address that names nothing is not a page |
| `metaWeblog.getRecentPosts` returned every author's entries | It returns the caller's own, drafts included | matters only on a blog with more than one author |
| `blogger.getUsersBlogs` needed no authentication, and faults carried empty codes | Authentication on every method; real fault codes (4 for auth, -32601 for an unknown method) | |
| The feed carried a hardcoded copyright line and fixed iTunes and media category trees | Dropped; `generator` names go-blogcfc | they named a third party who never agreed to it, on somebody else's blog |

An unknown *page* alias is the one address that still redirects home, as
`page.cfm` did.

## Dropped

Dead in 2026 or unsafe, with the setting kept where BlogCFC's settings
page had one, so an operator who knows the old screen recognises the new
one:

- the third-party bookmark-sharing widget
- tweetbacks (`usetweetbacks` stays on the settings page, inert, and is
  never written)
- trackbacks (the table existed; nothing was ever wired to it)
- the named ping services — `pingurls` stays as a generic list of URLs
  fetched once when an entry is released
- the check for a newer BlogCFC release
- the ad-serving tables, their reports and the media display page
- a second enclosure download handler that did not work
- the variable-dump debug page
- the `include` render plugin, which included any file it was given
- the affiliate product-box render plugin, whose widgets are discontinued
- the feed pod, whose source site is gone
- the `users` configuration key, which nothing read
- the bundled JavaScript UI toolkits, the Flash embedder and the
  Internet Explorer 6 stylesheets
- installers for the databases other than MySQL

## Bugs fixed rather than ported

- Search terms were concatenated into SQL; every query is parameterised.
- A view was counted twice on some paths, once by the page and once by
  the layout; one call site owns it now.
- The RSS 1.0 `dc:subject` was built in a variable that was never reset,
  so every item carried every earlier item's categories too. Each item
  now lists its own.
- The feed stamped `lastBuildDate` from the newest entry and failed on a
  blog with no entries; an empty feed is stamped now.
- The iTunes keywords field was rendered twice in the entry editor.
- The page-categories table was missing from the MySQL installer, so a
  clean install could not categorise a page.
- The XML-RPC response declared ISO-8859-1 over UTF-8 content.
- The Gravatar hash was computed without lowercasing the address on the
  moderation screen, so a commenter had a different avatar there.
- An entry was marked mailed even when the blog had no subscribers at
  all. The mark stays — an entry is mailed once — and the number of
  addresses it reached is recorded beside it.
- The page editor's alias field accepted 100 characters and then saved
  the first 50.
- The related-entries endpoint emitted something only a forgiving parser
  would read as JSON.
- The round trip that escapes `<code>` blocks for a rich-text client used
  a backreference to the wrong capture group, writing a paragraph tag
  into the tag it was rebuilding.
- The moderation queue listed subscribe-only rows — subscription records
  with no comment in them — as comments waiting for approval.
- The moderation queue rendered in place after approving, so a browser
  refresh approved and mailed a second time. Approving redirects.
- The stats screen's totals disagreed with the site, counting held
  comments and subscribe-only rows.
- The file manager silently rewrote `..` in a path to `/` and carried on.
- The admin menu checked for a `PageAdmin` role that the installer never
  created, so only an Admin ever reached the Pages screens. The role is
  seeded.
- A commenter's website field was linked exactly as typed, `javascript:`
  included.

## URL map

Canonical URLs are BlogCFC's SES grammar without the `index.cfm` prefix:

| Path | Renders |
|---|---|
| `/`, `/?startRow=N` | home, paginated (the cursor keeps its name) |
| `/{year}/{month}`, `/{year}/{month}/{day}` | month and day archives |
| `/{year}/{month}/{day}/{alias}` | one entry |
| `/{categoryalias}` | category listing |
| `/postedby/{username}` | entries by one author |
| `/?mode=entry&entry=`, `/?mode=cat&catid=a,b` | the id-based fallbacks for rows with no alias |
| `/search`, `/search/{term}` | search |
| `/page/{alias}` | static page |
| `/print/{id}` | print view |
| `/rss` | RSS 2.0 and 1.0, with ETag and Last-Modified |
| `/sitemap.xml`, `/robots.txt` | |
| `/contact`, `/send/{id}` | contact form, email this entry |
| `/comments/add/{id}`, `/comments/subscribe/{id}` | the popup forms, which also render full-page |
| `/confirmsubscription`, `/unsubscribe` | double opt-in and both unsubscribe forms |
| `/download/{id}/{file}` | logged enclosure download |
| `/slideshow/{name}` | one slide with prev/next |
| `/xmlrpc`, `/health`, `/static/…` | |
| `/admin/…` | the admin, session required except for the login |

Every legacy `.cfm` address answers **301** to its canonical form:
`index.cfm` and everything that hung off it, `rss.cfm`, `search.cfm`,
`googlesitemap.cfm`, `page.cfm/{alias}`, `print.cfm?id=`,
`addcomment.cfm?id=`, `download.cfm/{id}/{file}`, `admin/index.cfm` and
the old admin pages by name. Redirects are always absolute and built from
`BLOG_BASE_URL`, never from the request's `Host`.

The first path segment is a category or page alias, so a set of names is
reserved and refused as an alias on save: `search`, `page`, `print`,
`rss`, `contact`, `send`, `comments`, `download`, `enclosures`, `images`,
`slideshow`, `postedby`, `admin`, `static`, `health`, `xmlrpc`,
`sitemap.xml` and `robots.txt`.

## Settings

One `settings` table of key and value, seeded with BlogCFC's defaults,
read through a cached accessor. The keys and the fieldsets are the ones
BlogCFC's settings page showed, in the same order:

- **Blog Information** — `blogtitle`, `blogdescription`, `blogkeywords`,
  `owneremail`, `failto`, `blogurl` (read-only; it mirrors
  `BLOG_BASE_URL`)
- **Content** — `commentsfrom`, `maxentries`, `maxentriesadmin`,
  `timezone`, `pingurls`, `locale`
- **Content Controls / Security** — `ipblocklist`, `moderate`,
  `usecaptcha`, `usecfp`, `usetweetbacks` (inert), `trackbackspamlist`,
  `allowgravatars`, `filebrowse`, `imageroot`
- **Data Source and Mail** — shown read-only from the environment, never
  editable in the page
- **Podcasting** — `itunessubtitle`, `itunessummary`, `ituneskeywords`,
  `itunesauthor`, `itunesimage`, `itunesexplicit`
- **Pods** — `pods`, the widget list with show and order

Renamed: `offset` became `timezone`, and holds an IANA zone name instead
of a number of hours.

Dropped: `dsn`, `username`, `password`, `blogdbtype`, `mailserver`,
`mailusername`, `mailpassword` (all of these are environment now),
`tableprefix`, `installed`, `settings`, `users`, `saltalgorithm`,
`saltkeysize` and `hashalgorithm`.

## Data model

MariaDB, InnoDB, utf8mb4. The table names keep BlogCFC's semantics in
plain spelling, without its `tblblog` prefix and without the `blog`
tenant column:

`entries`, `categories`, `entry_categories`, `comments`, `subscribers`,
`related_entries`, `pages`, `page_categories`, `textblocks`,
`search_stats`, `enclosure_downloads`, `roles`, `users`, `user_roles`,
`settings`, `schema_migrations`.

Ids are 36-character UUID strings in a `char(36)`, which is BlogCFC's own
shape one character wider — its CFML UUIDs were 35 characters. (The feed
truncated each `catid` to 35 characters for that reason; here there is no
cap.) An importer from an existing BlogCFC database is not written and
not planned, but the column names, the id shape and the settings keys are
deliberately close enough that one could map the old tables onto these
without inventing anything: entries, their categories and their comments
line up row for row, ids can be carried across as they are, and
`posted` values need converting from the old blog's `offset` into UTC.
Two things cannot come across as they are: password hashes, which are
bcrypt now, and enclosure columns, which hold a file name rather than a
server path.

Migrations are embedded in the binary and run at startup; they are
idempotent, so a container that is destroyed and recreated is fine.

## Behaviours worth knowing

**Entry states.** Draft is `released = 0`; scheduled is `released = 1`
with a future `posted`; live is released and not future. Listings, feeds,
the sitemap, the calendar and every count use live entries only.

**The `<more/>` split** is an editor concern: the body is split on save
and re-joined on edit, and a body that starts with `<more/>` is an error.
List views show the part before it and a `[more]` link; the entry page
shows both parts.

**Release side effects**, in this order: if the entry is released and not
future and `sendemail` is on, subscribers are mailed now; if it is
released and future, the sweep will do it; if it is released and not
future, the ping URLs are fetched. The sweep runs once a minute over
`released = 1, posted <= now, mailed = 0`, mails those entries, sets
`mailed` and records how many addresses it reached. An entry published
over XML-RPC goes through the same hook as one saved in the admin.

**The comment pipeline**, in this order: the entry exists → it allows
comments → (for a real comment, not a subscribe-only row) the word list
and the IP block list → the honeypot, the timing check, the URL count and
the arithmetic challenge, all skipped for a signed-in author → insert,
moderated unless the `moderate` setting says otherwise or the author is
signed in, with a fresh kill token → clear any subscription if the
subscribe box is off → notify. Notifications go to the thread's
subscribers and to the owner unless the comment says otherwise, minus the
person who wrote it; the owner's mail carries Delete and, when the
comment is held, Approve links, and each subscriber's carries their own
unsubscribe link. Those links need no login, as they did not before.

**Antispam points** are the ones the original's configuration file
shipped: honeypot 3, timing 2, too many URLs 3, a spam word 2, and a
failure limit of 3. So the honeypot or the URL count blocks on its own,
timing or a single word hit does not, and any two weak signals do. The
timestamp must be between 5 seconds and an hour old, and more than six
URLs fails.

**Views** are counted once per visitor per entry. A signed cookie holds
the set of entries already seen (the newest 200, for 30 days) in place of
BlogCFC's session variable, so the count survives a restart. The print
view never counts, and no request counts twice.

**Caching.** One in-process cache holds the home page, the feeds and the
pods. Any write flushes it, and so does `?reinit=1`, which only a
signed-in admin can ask for. The permalink and category-alias maps are
rebuilt on the same flush.

**Timezone.** Everything is stored in UTC. The `timezone` setting decides
the zone entries are shown in, the zone a `posted` date typed into the
editor is read in, and the zone the permalink's date segments and the
calendar's "today" are computed in.

**Gravatar** URLs are `https://www.gravatar.com/avatar/` plus the MD5 of
the lowercased, trimmed address, with `s=64` on the public page (80 in
mail), `r=pg`, and the blog's own default image. HTTPS now, and the
lowercasing happens everywhere.

## The look

BlogCFC shipped the Arclite theme, which is GPL. This repository is
Apache-2.0, so none of it is copied: no stylesheet, no sprite, no font.
The look is recreated with original CSS from the theme's visible
structure — the dark brown page, the cream content band, the two 70/30
columns, the blue links and their pink hover, the white logo on the tall
dark header, the bordered small-caps section headers, the yellow
highlight on today's calendar cell and on a search hit, the five tag-cloud
sizes. Headings use system font stacks; there are no web fonts.

The DOM is kept: `#page`, `#header`, `#pagetitle`, `#nav`, `#main`,
`#main-content`, `#sidebar`, `li.block`, `div.post` with
`.post-title`/`.post-date`/`.post-author`/`.post-content`/`.post-metadata`,
`li.comment`, `#menu` and `#content` in the admin, `body#popUpFormBody`
on the popups. A screenshot comparison, an automated walk and anyone
reading the HTML see the same skeleton they saw before.

The mobile skin is gone; the one layout is responsive, and the popup
forms render as full pages as well as in a popup window.
