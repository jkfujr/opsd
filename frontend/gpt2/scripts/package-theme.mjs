import { readFile, writeFile, readdir, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join, relative } from 'node:path';
import { crc32, deflateRawSync } from 'node:zlib';
const root = fileURLToPath(new URL('../', import.meta.url));
const artifacts = join(root, '../../artifacts/frontend/gpt2');
const { version } = JSON.parse(await readFile(join(root, '../../package.json'), 'utf8'));
const manifest = { format_version: 1, id: 'frontend-gpt2', name: 'OPSD Mosona', version, api_version: 2, min_opsd_version: version, login: 'theme' };
const entries = [{ name: 'theme.json', data: Buffer.from(JSON.stringify(manifest, null, 2) + '\n') }];
async function walk(path) { for (const entry of (await readdir(path, { withFileTypes: true })).sort((a,b) => a.name.localeCompare(b.name))) { const file = join(path, entry.name); if (entry.isDirectory()) await walk(file); else if (entry.isFile()) entries.push({ name: relative(root, file).replaceAll('\\','/'), data: await readFile(file) }); } }
await walk(join(root, 'dist'));
for (const name of await readdir(join(root, 'public/fonts'))) {
  if (/^(OFL-.*\.txt|README\.md)$/.test(name)) entries.push({ name: `dist/fonts/${name}`, data: await readFile(join(root, 'public/fonts', name)) });
}
const records = [], directory = []; let offset = 0;
for (const entry of entries) {
  const name = Buffer.from(entry.name), compressed = deflateRawSync(entry.data), checksum = crc32(entry.data);
  const local = Buffer.alloc(30); local.writeUInt32LE(0x04034b50); local.writeUInt16LE(20,4); local.writeUInt16LE(0x800,6); local.writeUInt16LE(8,8); local.writeUInt32LE(checksum,14); local.writeUInt32LE(compressed.length,18); local.writeUInt32LE(entry.data.length,22); local.writeUInt16LE(name.length,26);
  const central = Buffer.alloc(46); central.writeUInt32LE(0x02014b50); central.writeUInt16LE(20,4); central.writeUInt16LE(20,6); central.writeUInt16LE(0x800,8); central.writeUInt16LE(8,10); central.writeUInt32LE(checksum,16); central.writeUInt32LE(compressed.length,20); central.writeUInt32LE(entry.data.length,24); central.writeUInt16LE(name.length,28); central.writeUInt32LE(offset,42);
  records.push(local,name,compressed); directory.push(central,name); offset += local.length + name.length + compressed.length;
}
const size = directory.reduce((n,b) => n+b.length,0), end = Buffer.alloc(22); end.writeUInt32LE(0x06054b50); end.writeUInt16LE(entries.length,8); end.writeUInt16LE(entries.length,10); end.writeUInt32LE(size,12); end.writeUInt32LE(offset,16);
const output = join(artifacts, `opsd-frontend-gpt2-${version}.zip`); await mkdir(artifacts, { recursive:true }); await writeFile(output,Buffer.concat([...records,...directory,end])); await writeFile(join(artifacts,'theme.json'), JSON.stringify(manifest,null,2)+'\n'); console.log(output);
