const express = require('express');
const path = require('path');
const fs = require('fs');
const os = require('os');
const cors = require('cors');

const { db } = require('./src/db');
const { registerApi } = require('./src/api');
const { scanMedia } = require('./src/scanner');
const { seed } = require('./src/seed');

const PORT = process.env.PORT || 8080;
const app = express();

app.use(cors());
app.use(express.json({ limit: '10mb' }));
app.use(express.urlencoded({ extended: true }));

// ensure dirs
for (const d of ['media/movies', 'media/series', 'public/posters', 'data']) {
  const p = path.join(__dirname, d);
  if (!fs.existsSync(p)) fs.mkdirSync(p, { recursive: true });
}

// seed demo data on first run (kept even after scan)
seed(false);
// scan local files (merge, non-destructive)
try { scanMedia(false); } catch (e) { console.error('scan error', e.message); }

registerApi(app);

// static frontend
app.use(express.static(path.join(__dirname, 'public')));
app.get('*', (req, res, next) => {
  if (req.path.startsWith('/api/')) return next();
  res.sendFile(path.join(__dirname, 'public', 'index.html'));
});

// unknown API routes -> JSON, not HTML
app.use('/api', (req, res) => res.status(404).json({ error: 'Unknown API endpoint' }));

// JSON errors, never stack-trace HTML
// eslint-disable-next-line no-unused-vars
app.use((err, req, res, next) => {
  console.error('[api error]', req.method, req.path, '-', err.message);
  res.status(500).json({ error: 'Server error' });
});

const server = app.listen(PORT, '0.0.0.0', () => {
  const nets = os.networkInterfaces();
  const ips = [];
  for (const arr of Object.values(nets)) for (const i of arr) if (i.family === 'IPv4' && !i.internal) ips.push(i.address);
  console.log('\n  🎬 LanFlix running!\n');
  console.log(`  Local:   http://localhost:${PORT}`);
  ips.forEach(ip => console.log(`  LAN:     http://${ip}:${PORT}   <-- open this on TV / phones`));
  console.log('\n  Default admin: admin@lan.local / admin123');
  console.log('  Demo user:   demo@lan.local / demo1234');
  console.log('\n  Put video files in ./media/movies and ./media/series, then Admin → Rescan.\n');
});
server.on('error', (err) => {
  if (err && err.code === 'EADDRINUSE') {
    console.error(`\n  ❌ Port ${PORT} is already in use — LanFlix is already running in another terminal.`);
    console.error(`  Stop the other one, or use a different port:  PORT=9000 npm start\n`);
    process.exit(1);
  }
  throw err;
});
