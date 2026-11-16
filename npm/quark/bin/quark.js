#!/usr/bin/env node

const os = require('os');
const path = require('path');
const fs = require('fs');
const { spawnSync } = require('child_process');

const platform = os.platform();
const arch = os.arch();

let pkgName;
if (platform === 'darwin') {
  if (arch === 'x64') pkgName = '@quark-client/quark-darwin-x64';
  else if (arch === 'arm64') pkgName = '@quark-client/quark-darwin-arm64';
} else if (platform === 'linux') {
  if (arch === 'x64') pkgName = '@quark-client/quark-linux-x64';
  else if (arch === 'arm64') pkgName = '@quark-client/quark-linux-arm64';
} else if (platform === 'win32') {
  if (arch === 'x64') pkgName = '@quark-client/quark-win32-x64';
}

if (!pkgName) {
  console.error(`quark: unsupported platform (${platform} ${arch})`);
  console.error('Quark is available for:');
  console.error('  - Linux x64, arm64');
  console.error('  - macOS x64, arm64');
  console.error('  - Windows x64');
  console.error('Install via: https://github.com/crazy-vedic/Quark#installation');
  process.exit(1);
}

const exeName = platform === 'win32' ? 'quark.exe' : 'quark';

// Locate the platform package directory without importing it as a module
// (it has no `main`). Resolving its package.json via the normal Node lookup
// from this file covers npm's nested layout
// (<main>/node_modules/@quark-client/...) and hoisted/flat layouts. The
// explicit candidates are a fallback for unusual layouts.
function findPackageDir() {
  try {
    return path.dirname(require.resolve(`${pkgName}/package.json`));
  } catch {}
  const mainPkgDir = path.resolve(__dirname, '..');
  const candidates = [
    path.join(mainPkgDir, 'node_modules', pkgName),
    path.resolve(mainPkgDir, '..', '..', pkgName),
  ];
  return candidates.find((d) => fs.existsSync(path.join(d, 'package.json')));
}

const pkgDir = findPackageDir();
if (!pkgDir) {
  console.error(`quark: platform package not found (${pkgName})`);
  console.error('This may happen if npm installed the main package without the matching platform package.');
  console.error('Try: npm install -g @quark-client/quark@latest');
  process.exit(1);
}

const binPath = path.join(pkgDir, 'bin', exeName);
if (!fs.existsSync(binPath)) {
  console.error(`quark: platform package ${pkgName} is installed at ${pkgDir}, but the executable is missing: ${binPath}`);
  console.error('The installation may be corrupt (e.g. interrupted or blocked by antivirus).');
  console.error('Try: npm uninstall -g @quark-client/quark && npm install -g @quark-client/quark@latest');
  process.exit(1);
}

// Pass arguments straight to the binary: no shell, so spaces, quotes, `&`,
// `|`, `$` etc. in arguments (URLs, JSON bodies, headers) arrive verbatim.
const result = spawnSync(binPath, process.argv.slice(2), {
  stdio: 'inherit',
});

if (result.error) {
  console.error(`quark: failed to run ${binPath}: ${result.error.message}`);
  process.exit(1);
}
if (result.signal) {
  // Re-raise so the parent shell sees the same signal (e.g. Ctrl+C).
  process.kill(process.pid, result.signal);
}
process.exit(result.status === null ? 1 : result.status);
