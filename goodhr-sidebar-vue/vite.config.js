import { cpSync, existsSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const sourceRoot = path.resolve(currentDir, "..");
const outDir = path.resolve(currentDir, "extension");

function ensureParent(filePath) {
  mkdirSync(path.dirname(filePath), { recursive: true });
}

function copyEntry(from, to) {
  const sourcePath = path.resolve(sourceRoot, from);
  const targetPath = path.resolve(outDir, to);

  if (!existsSync(sourcePath)) {
    throw new Error(`Missing required source: ${sourcePath}`);
  }

  ensureParent(targetPath);
  cpSync(sourcePath, targetPath, { recursive: true, force: true });
}

function copyLegacyAssets() {
  return {
    name: "copy-legacy-assets",
    closeBundle() {
      [
        ["icons", "icons"],
        ["sounds", "sounds"],
        ["utils", "utils"],
        ["content_scripts", "content_scripts"],
        ["popup", "popup"],
        ["config.js", "config.js"],
        ["background.js", "legacy/background.js"],
      ].forEach(([from, to]) => copyEntry(from, to));
    },
  };
}

export default defineConfig({
  plugins: [vue(), copyLegacyAssets()],
  publicDir: path.resolve(currentDir, "public"),
  build: {
    outDir,
    emptyOutDir: true,
    rollupOptions: {
      input: {
        sidepanel: path.resolve(currentDir, "sidepanel.html"),
      },
      output: {
        entryFileNames: "assets/[name].js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
});
