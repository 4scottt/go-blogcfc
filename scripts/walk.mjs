// Headless acceptance walk: a real browser reads the blog, signs in to the
// admin, creates a category and a released entry, sees the entry on the
// home page and (from M5 on) in the feed, adds a comment (M3), logs out.
// It mirrors oldbox's blogcfc-walk.mjs step for step so the as-is app and
// this rewrite are walked alike. Fails on any browser console error, page
// error, or own-origin sub-resource answering >= 400.
//
//   WALK_URL         http://localhost:8081 (default)
//   ADMIN_PASSWORD   the admin's password (default admin, matching scripts/dev.sh)
//   WALK_GATE_USER / WALK_GATE   basic-auth credentials when walking through an oldbox gate
//   WALK_BROWSER     chrome (default: Chrome channel) | chromium (Playwright's own, for CI)
//   WALK_HEADED=1    show the browser
//   WALK_STEPS       how many steps to run (default: all that this milestone supports)
//   PLAYWRIGHT_DIR   where `playwright` is installed (default: scripts/.walk, then a global resolve)
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
function loadPlaywright() {
  const dirs = [process.env.PLAYWRIGHT_DIR, path.join(here, ".walk"), here].filter(Boolean);
  for (const d of dirs) {
    try { return createRequire(path.join(d, "node_modules", "/"))("playwright"); } catch {}
  }
  return createRequire(import.meta.url)("playwright");
}
const { chromium } = loadPlaywright();

const url = (process.env.WALK_URL || "http://localhost:8081").replace(/\/$/, "");
const adm = process.env.ADMIN_PASSWORD || "admin";
const steps = Number(process.env.WALK_STEPS || 5);
const headed = process.env.WALK_HEADED === "1";
const stamp = new Date().toISOString().slice(11, 19).replace(/:/g, "");
const category = `Walk ${stamp}`;
const title = `Hello from the walk ${stamp}`;
const problems = [];
const note = (s) => console.log(`  ${s}`);

async function expectText(page, text, what) {
  await page.waitForFunction((t) => document.body && document.body.innerText.includes(t), text, { timeout: 30_000 })
    .catch(() => { throw new Error(`${what}: the page does not say "${text}" (at ${page.url()})`); });
}

const launchOpts = { headless: !headed, slowMo: headed ? 250 : 0 };
if ((process.env.WALK_BROWSER || "chrome") === "chrome") launchOpts.channel = "chrome";
const browser = await chromium.launch(launchOpts);
let failed = null;
try {
  const ctxOpts = {};
  if (process.env.WALK_GATE) ctxOpts.httpCredentials = { username: process.env.WALK_GATE_USER || "oldbox", password: process.env.WALK_GATE };
  const ctx = await browser.newContext(ctxOpts);
  const page = await ctx.newPage();
  const own = new URL(url).host;
  page.on("requestfailed", (r) => { if (new URL(r.url()).host === own) problems.push(`${r.failure()?.errorText || "failed"}: ${r.url()}`); });
  page.on("response", (r) => { if (r.status() >= 400 && r.request().resourceType() !== "document" && new URL(r.url()).host === own) problems.push(`${r.status()}: ${r.url()}`); });
  page.on("console", (m) => { if (m.type() === "error") problems.push(`console: ${m.text().slice(0, 200)} (${m.location()?.url || "no url"})`); });
  page.on("pageerror", (e) => problems.push(`page: ${String(e).slice(0, 200)}`));
  page.setDefaultTimeout(30_000);

  console.log(`go-blogcfc walk against ${url}`);
  // 1. the blog
  await page.goto(`${url}/`);
  const blogTitle = await page.title();
  if (!blogTitle) throw new Error("the blog home has no title");
  note(`blog: ${blogTitle}`);

  // 2. admin sign-in
  await page.goto(`${url}/admin/`);
  await page.fill('input[name="username"]', "admin");
  await page.fill('input[name="password"]', adm);
  await page.getByRole("button", { name: "Login" }).or(page.locator('input[type="submit"][value="Login"]')).first().click();
  await expectText(page, "Welcome to BlogCFC Administrator", "admin sign-in");
  note("admin: signed in");

  // 3. a category
  await page.goto(`${url}/admin/categories/new`);
  await page.fill('input[name="name"]', category);
  await page.fill('input[name="alias"]', category.toLowerCase().replace(/[^a-z0-9]+/g, "-"));
  await page.getByRole("button", { name: "Save" }).or(page.locator('input[type="submit"][value="Save"]')).first().click();
  await page.goto(`${url}/admin/categories`);
  await expectText(page, category, "the categories list");
  note(`category created: ${category}`);

  // 4. a released entry in it
  await page.goto(`${url}/admin/entries/new`);
  await page.fill('input[name="title"]', title);
  await page.fill('textarea[name="body"]', `<p>Posted by the headless walk at ${new Date().toISOString()}.</p>`);
  await page.selectOption('select[name="categories"]', { label: category });
  const released = page.locator('input[name="released"]');
  if (await released.count() && !(await released.first().isChecked())) await released.first().check();
  await page.getByRole("button", { name: "Save" }).or(page.locator('input[type="submit"][value="Save"]')).first().click();
  await page.goto(`${url}/admin/entries`);
  await expectText(page, title, "the entries list");
  note(`entry saved: ${title}`);

  // 5. on the blog
  await page.goto(`${url}/`);
  await expectText(page, title, "the blog home");
  note("entry on the blog");

  if (steps >= 6) {
    const rss = await ctx.request.get(`${url}/rss?mode=full`);
    const feed = await rss.text();
    if (rss.status() !== 200 || !feed.includes(title)) throw new Error(`the feed: ${rss.status()}, entry ${feed.includes(title) ? "present" : "missing"}`);
    note("entry in the feed");
  }
  if (steps >= 7) {
    await page.click(`text=${title}`);
    await page.click('a[href*="/comments/add/"]');
    await page.fill('input[name="name"]', "Walker");
    await page.fill('input[name="email"]', "walker@example.com");
    await page.fill('textarea[name="comment"]', "The rewrite reads well.");
    await page.getByRole("button", { name: "Post Comment" }).first().click();
    await page.goto(`${url}/`);
    await page.click(`text=${title}`);
    await expectText(page, "The rewrite reads well.", "the comment on the entry");
    note("comment on the entry");
  }
  await page.goto(`${url}/admin/logout`);
  note("admin: signed out");
  await ctx.close();
} catch (e) {
  failed = e;
} finally {
  await browser.close();
}
if (failed) { console.error(`FAIL: ${failed.message}`); process.exit(1); }
if (problems.length) { console.error(`FAIL: ${problems.length} browser problem(s)`); for (const p of problems) console.error(`  ${p}`); process.exit(1); }
console.log(`PASS: ${steps} steps, no console errors, no page errors, no failed own-origin resources`);
