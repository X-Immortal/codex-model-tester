import fs from "node:fs";
import path from "node:path";
import sharp from "sharp";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const assetsDir = path.resolve(scriptDir, "..", "assets");
await sharp(path.join(assetsDir, "app-icon.svg"))
  .resize(512, 512)
  .png()
  .toFile(path.join(assetsDir, "icon.png"));
fs.chmodSync(path.join(assetsDir, "icon.png"), 0o644);
