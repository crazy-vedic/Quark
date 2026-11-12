#!/usr/bin/env node

const os = require('os');
const path = require('path');
const fs = require('fs');
const { execSync } = require('child_process');

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

let binPath;
try {
  // Try require.resolve first (for local node_modules)
  let found = false;
  try {
    const pkgPath = require.resolve(pkgName);
    binPath = path.join(path.dirname(pkgPath), '..', 'bin', platform === 'win32' ? 'quark.exe' : 'quark');
    found = true;
  } catch {
    // Fall back to looking in npm's global node_modules
    const globalNodeModulesParent = path.resolve(__dirname, '../..', '..');
    const pkgDir = path.join(globalNodeModulesParent, pkgName);
    const pkgJsonPath = path.join(pkgDir, 'package.json');
    if (fs.existsSync(pkgJsonPath)) {
      binPath = path.join(pkgDir, 'bin', platform === 'win32' ? 'quark.exe' : 'quark');
      found = true;
    }
  }
  if (!found) throw new Error('package not found');
} catch (err) {
  console.error(`quark: platform package not found (${pkgName})`);
  console.error('This may happen if npm installed the main package without the matching platform package.');
  console.error('Try: npm install -g @quark-client/quark@latest');
  process.exit(1);
}

try {
  execSync(binPath + ' ' + process.argv.slice(2).join(' '), {
    stdio: 'inherit',
    shell: platform === 'win32',
  });
} catch (err) {
  process.exit(err.status || 1);
}
