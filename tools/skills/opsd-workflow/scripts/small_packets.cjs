// 仅在普通 SSH 传输失败后显式启用。
const net = require('node:net');
const port = Number(process.argv[3]);
if (!process.argv[2] || !Number.isInteger(port) || port < 1 || port > 65535) {
  process.stderr.write('用法：node small_packets.cjs 主机 端口\n');
  process.exit(1);
}
const socket = net.connect(port, process.argv[2]);
socket.setNoDelay(true);
socket.on('error', (error) => { process.stderr.write(error.message + '\n'); process.exit(1); });
socket.on('data', (data) => { if (!process.stdout.write(data)) socket.pause(); });
process.stdout.on('drain', () => socket.resume());
socket.on('end', () => process.stdout.end());
socket.on('connect', async () => {
  try {
    for await (const data of process.stdin) {
      for (let offset = 0; offset < data.length; offset += 512) {
        await new Promise((resolve, reject) => socket.write(data.subarray(offset, offset + 512), (error) => error ? reject(error) : resolve()));
        await new Promise((resolve) => setTimeout(resolve, 2));
      }
    }
    socket.end();
  } catch (error) { process.stderr.write(error.message + '\n'); process.exit(1); }
});
