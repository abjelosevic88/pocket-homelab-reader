# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer prd_…`, Settings → API & shortcuts), a Pocket Core token, or a Pocket Core app link (`pcx_…`, scopes below). Errors are `{"error": "…"}`. Everything is per person: another person's ids answer 404.

## Lists and articles

| | |
|---|---|
| `GET /articles?view=unread\|all\|today\|saved\|archive\|starred\|history\|highlights&feed&folder&tag&q&oldest&limit&cursor` | `{items, next}`; pass `next` back as `cursor`. `q` is a full-text search (all words, the last may be a prefix, "quoted phrases") |
| `GET /articles/{id}` | One article with `content` (cleaned HTML), `minutes`, `feed`, `highlight_list` |
| `PUT /articles/{id}` | Any of `{read, starred, saved, archived, progress (0–1), tags, note, title}` |
| `POST /articles/state` | `{ids: […], read?, starred?, saved?, archived?}` — many at once |
| `POST /articles/read-all` | `{feed_id?, folder_id?, before?}` — mark a list read |
| `DELETE /articles/{id}` | Remove (a feed post does not come back) |
| `POST /articles/{id}/extract` | Fetch the whole article again, now |
| `POST /saved` | `{url, title?, tags?, note?, html?}` — save a page to read later (201 new, 200 already there: moved to the top). `html` = the page as the browser has it |
| `GET /tags` | `[{tag, count}]` |
| `GET /dashboard` | `{unread, today, saved, starred, highlights, feeds, failing, read_today, saved_today, read_minutes_today}` |
| `GET /day/YYYY-MM-DD` | One day: `{read, saved, highlights, read_count, saved_count, read_minutes}` (what Pocket Day reads) |

## Feeds

| | |
|---|---|
| `GET /feeds` | `{feeds, folders}` with unread counts |
| `POST /feeds` | `{url, folder_id? \| folder?, custom_title?, full_text?, notify?}` — `url` may be a website or YouTube channel; the feed is found and checked at once. 409 when already followed |
| `GET/PUT/DELETE /feeds/{id}` | PUT takes any of `{url, custom_title, folder_id, full_text, notify, paused, refresh_min}`. DELETE keeps starred/saved/highlighted posts as saved pages |
| `POST /feeds/{id}/refresh`, `POST /feeds/refresh` | Check one feed now (`{new, error?}`), or make all due |
| `POST /folders`, `PUT/DELETE /folders/{id}` | `{name, sort}`; deleting a folder keeps its feeds |
| `POST /discover` | `{url}` → `{is_feed, feeds: [{url, title, kind, feed_id}], page: {title, excerpt, image_url, site_name, saved_id?}}` |

## Highlights and rules

| | |
|---|---|
| `GET /highlights?q` | Newest first, with the article's title and address |
| `POST /articles/{id}/highlights` | `{text, note?, color: yellow\|green\|blue\|pink, prefix?, suffix?, position?}` |
| `PUT/DELETE /highlights/{id}` | `{note?, color?}` |
| `GET /highlights/export.md` | Everything as Markdown |
| `POST /articles/{id}/notes` | Write (or update) the article's note in Pocket Notes |
| `GET/POST /rules`, `PUT/DELETE /rules/{id}` | `{name, field: title\|any\|author\|url, words: "a, b", action: read\|star\|save\|tag\|notify, tag, feed_id?, folder_id?, enabled}` |
| `POST /rules/test` | A rule → `{matches, of, examples}` on recent posts |

## AI

`GET /ai/status`, `POST /articles/{id}/summary[?fresh=1]`, `POST /ai/catch-up {feed_id?, folder_id?}`, `POST /articles/{id}/tags/suggest`, `POST /ai/ask {question, article_id?}` — answer `{ok, text}` or `{ok: false, error}`.

## Data

`GET /export.json` (feeds, folders, kept articles with text, highlights, rules), `GET /export.opml`, `GET /export.csv`, `POST /import` (multipart `file` or raw body; OPML, a Pocket Reader or Karakeep export, Netscape bookmarks HTML or a CSV with a `url` column — recognised by itself, `?kind=` to force; the counts come back both under `counts` and at the top level), `POST /import/opml` (the OPML as the raw body), `GET /backup` (admins, the whole SQLite file). Imports only add; a second run of the same file adds nothing.

`GET /pocket/portable` describes these for Pocket Move: exports under the link scope `move:export`, imports under `move:import`.

## Google Reader API

At `/api/greader.php` (and `/api/greader`), for RSS apps: `accounts/ClientLogin` (Email + a personal token as Passwd), `reader/api/0/{token, user-info, subscription/list, subscription/edit, subscription/quickadd, tag/list, unread-count, stream/items/ids, stream/items/contents, stream/contents/…, edit-tag, mark-all-as-read, rename-tag, disable-tag}`. Folders are labels; the read-later list is the label `Read later`.

## Pocket Core links

Offers `reading:read` (dashboard, articles and one article without your note on it, feeds, tags, highlights, a day) and `reading:write` (save pages, follow feeds; remove only what that app saved). Uses `notes:write` in Pocket Notes and Pocket AI.

## Search, Day and Hub

Reader says in its manifest (category `Media`) that it can be searched, has a day, a widget and takes a quick line, and answers on the four routes every Pocket app uses for that (`docs/capabilities.md` in Pocket Core), so Pocket Search, Pocket Day and Pocket Hub find it on their own. Each has its own link scope that opens that one route and nothing else; `reading:read` and `reading:write` do not open them. A signed-in person or a personal token can call them too, and they work without Pocket Core. Addresses (`url`) are paths in the app. None of them shows your own notes on an article.

| Route | Link scope | |
|---|---|---|
| `GET /pocket/search?q&limit` | `pocket:search` | `{items}`: the saved pages and feed posts `GET /articles?q=` finds (title, text and tags; every word, the last may be a prefix). Each has `id`, `title`, `snippet` (the site, then how the article starts), `kind: "article"`, `date` (when you saved it; a post you did not save: when it came out), `url` (`/a/{id}`) and `link` (the article's own address). What you kept (read later, archive, starred, highlighted) and a title that has the words come first, each newest first. `limit` 1–50, 8 by default. Bangs in Pocket Search: `!reader`, `!read` |
| `GET /pocket/day/{date}?tz` | `pocket:day` | `{date, summary, items, metrics}`: one item for each article of that day, `kind` `finished` (`done: true`), `read` or `saved`, with `time`, `sub` (the site), `minutes` (the reading time of what was read) and `url`. An article is finished on the day you marked it done; one you never marked, on the day you read it to the end. It shows once, as the most telling of the three. `summary`: "Finished 1 article, read 2 more, saved 3 for later and highlighted 4 passages"; `metrics`: "Reading time" and "Highlights". The same reading as `GET /day/{date}`; with `tz` the day is that time zone's, else the server's. A day with nothing has no items and no summary |
| `GET /pocket/widget` | `pocket:widget` | `{line, metrics, items, url: "/saved"}`: the five pages saved last that still wait on the read-later list, `detail` "The Slow Kitchen · 4 min". `line` is "12 to read", "12 to read, 30 unread in your feeds" or "Nothing waiting to be read"; `metrics`: "To read", "Unread in feeds" (when you follow feeds) and "Reading today". An empty list with no feeds answers no line, items or metrics |
| `POST /pocket/capture` | `pocket:capture` | `{request_id, kind: "article", text, title?}` saves the one web address (`http://` or `https://`) in the line to read later, as `POST /saved` does, and answers `201 {id, title, url}`. The other words become your note on it; `title` names it, else the page's own title is used. An address you already have is put back on the read-later list instead of added twice (its note is kept; the words become the note when it had none). `400 {error}` when there is no address ("paste a web address to save it") or more than one. The same `request_id` (16–80 of `A-Z a-z 0-9 _ -`) again answers the same and saves nothing. The page is fetched before the answer, as in `POST /saved` (up to 30 seconds for a slow site; a page that does not answer is saved by its address and fetched later) |
