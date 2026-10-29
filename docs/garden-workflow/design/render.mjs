// Rasterize authored SVGs; keep rendering dependencies outside the application.
import { createRequire } from 'node:module';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const option = name => {
  const at = process.argv.indexOf(name);
  return at < 0 ? null : process.argv[at + 1];
};
const modules = option('--modules');
const require = createRequire(modules ? path.join(path.resolve(modules), 'package.json') : import.meta.url);
const scratch = await fs.mkdtemp(path.join(os.tmpdir(), 'bonsai-garden-render-'));
const xml = text => text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('"', '&quot;');
try {
  const fonts = option('--fonts');
  if (fonts) {
    const config = path.join(scratch, 'fonts.conf');
    await fs.writeFile(config, `<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><dir>${xml(path.resolve(fonts))}</dir><cachedir>${xml(scratch)}</cachedir></fontconfig>`);
    process.env.FONTCONFIG_FILE = config;
  }
  const sharp = require('sharp');
  const manifest = JSON.parse(await fs.readFile(path.join(here, 'manifest.json'), 'utf8'));
  for (const screen of manifest.screens) {
    const svg = path.join(here, 'svg', screen.stem + '.svg');
    const png = path.join(here, 'png', screen.stem + '.png');
    await sharp(svg).png({ compressionLevel: 9 }).toFile(png);
    const meta = await sharp(png).metadata();
    if (meta.width !== screen.width || meta.height !== screen.height) throw new Error('Incorrect raster size: ' + screen.stem);
  }
  const picks = ['manual', 'clarify', 'board', 'table', 'plan-review', 'human-review'];
  const tiles = [];
  const tileW = 600, tileH = 432, gutter = 20;
  for (const [i, key] of picks.entries()) {
    const screen = manifest.screens.find(s => s.key === key && s.cols === (key === 'board' ? 160 : 120));
    const buffer = await sharp(path.join(here, 'png', screen.stem + '.png'))
      .resize(tileW, tileH, { fit: 'contain', background: '#131612' }).png().toBuffer();
    tiles.push({ input: buffer, left: gutter + (i % 2) * (tileW + gutter), top: gutter + Math.floor(i / 2) * (tileH + gutter) });
  }
  await sharp({ create: { width: tileW * 2 + gutter * 3, height: tileH * 3 + gutter * 4, channels: 3, background: '#1b1f19' } })
    .composite(tiles).png({ compressionLevel: 9 }).toFile(path.join(here, 'overview.png'));
  console.log(`Rendered ${manifest.screens.length} PNGs and six-screen overview.`);
} finally {
  await fs.rm(scratch, { recursive: true, force: true });
}
