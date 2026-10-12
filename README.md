# 🎬 LanFlix — Netflix clone for your local LAN

Full Netflix-style experience, served from **your own computer** to everyone on the same Wi-Fi.
You manage everything locally (users, content, analytics). No cloud, no subscription.

## ✨ Features (Netflix parity, local edition)

| Netflix feature | LanFlix |
|---|---|
| Login / accounts | ✅ Email + password, first account becomes admin |
| Who's watching profiles (5/account) | ✅ Avatars, Kids mode, PIN lock |
| Home rows, hero billboard, Top 10 | ✅ Trending, New, Top 10, genre rows |
| Search + genres + Movies / TV Shows | ✅ Instant search, genre chips |
| Detail page + episodes + More like this | ✅ Modal with cast, ratings, seasons |
| Player: resume, autoplay next, skip intro, subtitles | ✅ Custom Netflix-style player: play/pause, ±10s, seek bar + buffer, speed (0.5–2×), volume, CC menu, fullscreen, PiP, prev/next episode, episodes drawer, auto-hide, shortcuts, screen lock |
| Continue Watching + progress sync per profile | ✅ Saved every 5s, resume prompt |
| My List, 👍👎 ratings | ✅ Per-profile |
| Kids mode | ✅ Hides 16+/18+ |
| TV remote navigation | ✅ Arrow keys + Enter, TV-friendly UI |
| Mobile / responsive | ✅ |
| **Local CRM (admin)** | ✅ Users, content metadata + posters, views analytics, storage, rescan |

Streaming = **direct-play over LAN** (no transcoding → fast, low CPU). Use **.mp4 (H.264)** for universal browser/TV play. `.mkv` is served but most browsers can't play it.

## 🚀 Run (2 minutes)

```bash
cd ghhhg
npm install
npm start
```

You'll see:

```
Local: http://localhost:8080
LAN:   http://192.168.1.5:8080   <-- open this on TV / phones
```

Open the **LAN URL** on any device on the same Wi-Fi. No internet required (demo posters need internet once).

**Logins (seeded):**
- Admin: `admin@lan.local` / `admin123`
- Demo family: `demo@lan.local` / `demo1234`

## 📁 Add your own movies (you have local files)

```
media/
  movies/
    Dune Part Two (2024)/
      Dune Part Two (2024).mp4
      poster.jpg            (optional)
      backdrop.jpg          (optional)
      film.vtt              (optional subtitles)
      metadata.json         (optional: description, genres, cast…)
    Avatar (2009).mp4       (loose files also work)
  series/
    Breaking Bad/
      poster.jpg
      S01E01 Pilot.mp4
      S01E02 Cat's in the Bag.mp4
      Season 2/S02E01.mp4   (subfolders OK)
```

`metadata.json` example:
```json
{
  "description": "A noble family…",
  "genres": ["Sci-Fi", "Adventure"],
  "cast": ["Timothée Chalamet", "Zendaya"],
  "director": "Denis Villeneuve",
  "maturity_rating": "U/A 13+",
  "year": 2024
}
```

Then: open app → **🛠 CRM → 🔄 Rescan media** (or `npm run scan`). Or `POST /api/admin/scan` as admin.

**Easiest: drag & drop.** Open **🛠 CRM → Content**, pick Movies or TV Series, and drop the folder straight onto the drop zone (or click it to browse). Files upload to `media/`, get scanned automatically, and appear in the catalog — with a progress bar for big files.

Supported video: `.mp4 .webm .mkv .mov .avi .m4v` • posters: `.jpg .png .webp` • subs: `.vtt` (same folder, same basename).

## 🛠 CRM (managed by you, locally)

Open **🛠 CRM** in the top nav (admin only):
- **Overview** — users, plays, storage used, 7-day chart, top content, restore-demo-catalog button
- **Users** — create/disable/reset-password/delete users, change roles (LAN-only accounts; you can't demote/disable yourself)
- **Content** — **add** titles (direct video links), edit metadata/sources/featured flag, convert movie↔series, manage series episodes, upload poster/backdrop, delete. Open any title's Edit to **attach more files** — drop files/folders or use Files…/Folder… pickers; episode uploads auto-create episode rows for series, video uploads link (or replace) the movie file
- **Views** — who watched what, when

Data lives in `data/lanflix.db` (SQLite). Back it up by copying that file while the server is stopped.
For testing / experiments, point the server at a copy: `LANFLIX_DB=/tmp/test.db npm start`.

## 🌐 LAN / TV tips

- Server binds `0.0.0.0:8080` — reachable on your LAN. Allow Node through the firewall if prompted.
- Same Wi-Fi required. For 4K direct-play, wired Ethernet or 5 GHz Wi-Fi recommended.
- Smart TV: open the TV **browser** → type the LAN URL → fullscreen. Arrow keys navigate.
- Custom port: `PORT=9000 npm start`
- Reset demo: `npm run reset` (wipes DB, reseeds admin + demo catalog)

## 🔌 API (for a future mobile app)

All JSON under `/api/*`. Auth: `Authorization: Bearer <jwt>`.
Profile-scoped routes require the token + a `profileId` you own (kids filter can't be bypassed with someone else's profile).
- `POST /api/auth/login|signup`, `GET /api/auth/me`
- `GET/POST/PUT/DELETE /api/profiles`
- `GET /api/browse/rows?profileId=`, `GET /api/content?search=&genre=&type=`, `GET /api/content/:id`, `GET /api/content/:id/episodes`
- `GET/POST/DELETE /api/mylist`, `POST /api/rate`, `POST /api/progress`, `POST /api/content/:id/view`, `GET /api/history`
- `GET /api/stream/movie/:id`, `GET /api/stream/episode/:id` (HTTP Range)
- Admin: `GET /api/admin/stats|users|content|views`, `POST /api/admin/scan`, `POST /api/admin/seed-demo`, `POST /api/admin/content`, `PUT /api/admin/content/:id`, `DELETE /api/admin/content/:id`, `POST /api/admin/episodes`, `DELETE /api/admin/episodes/:id`

## 📦 Project layout

```
server.js          Express + static + LAN boot
src/db.js          SQLite schema
src/auth.js        JWT + bcrypt
src/scanner.js     media/ folder scanner
src/seed.js        admin + demo catalog
src/api.js         all REST routes + range streaming
public/            Netflix UI (no build step)
media/             ← put your videos here
data/              SQLite DB (auto-created)
```
