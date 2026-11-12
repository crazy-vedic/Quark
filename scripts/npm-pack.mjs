#!/usr/bin/env node

import fs from 'fs';
import path from 'path';
import { execSync } from 'child_process';

const version = process.argv[2];

if (!version) {
  console.error('Usage: npm-pack.mjs <version>');
  console.error('Example: npm-pack.mjs 1.2.3');
  process.exit(1);
}

const normalizedVersion = version.replace(/^v/, '');
const platforms = [
  { name: 'linux-x64', binary: 'quark-linux-amd64', os: 'linux', cpu: 'x64' },
  { name: 'linux-arm64', binary: 'quark-linux-arm64', os: 'linux', cpu: 'arm64' },
  { name: 'darwin-x64', binary: 'quark-darwin-amd64', os: 'darwin', cpu: 'x64' },
  { name: 'darwin-arm64', binary: 'quark-darwin-arm64', os: 'darwin', cpu: 'arm64' },
  { name: 'win32-x64', binary: 'quark-windows-amd64.exe', os: 'win32', cpu: 'x64' },
];

function updatePackageJson(filePath, version, directory = null) {
  const content = JSON.parse(fs.readFileSync(filePath, 'utf8'));
  content.version = version;

  if (!content.repository) {
    content.repository = {
      type: 'git',
      url: 'https://github.com/crazy-vedic/Quark',
    };
  }

  if (directory && !content.repository.directory) {
    content.repository.directory = directory;
  }

  fs.writeFileSync(filePath, JSON.stringify(content, null, 2) + '\n');
}

function ensureDir(dir) {
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
}

// Update main package version
const mainPkgPath = path.join('npm/quark/package.json');
updatePackageJson(mainPkgPath, normalizedVersion, 'npm/quark');

const optionalDeps = {};

// Process each platform
for (const platform of platforms) {
  const platformDir = path.join('npm/platforms', platform.name);
  const binDir = path.join(platformDir, 'bin');
  ensureDir(binDir);

  // Copy binary
  const srcBinary = path.join('dist/binaries', platform.binary);
  const destBinary = path.join(binDir, platform.binary === 'quark-windows-amd64.exe' ? 'quark.exe' : 'quark');

  if (!fs.existsSync(srcBinary)) {
    console.error(`Binary not found: ${srcBinary}`);
    process.exit(1);
  }

  fs.copyFileSync(srcBinary, destBinary);
  if (platform.os !== 'win32') {
    fs.chmodSync(destBinary, 0o755);
  }

  // Update platform package.json
  updatePackageJson(path.join(platformDir, 'package.json'), normalizedVersion, path.join('npm/platforms', platform.name));

  // Add to optionalDependencies
  optionalDeps[`@quark-client/quark-${platform.name}`] = normalizedVersion;
}

// Update main package optionalDependencies only if --with-deps flag is passed
if (process.argv[3] === '--with-deps') {
  const mainContent = JSON.parse(fs.readFileSync(mainPkgPath, 'utf8'));
  mainContent.optionalDependencies = optionalDeps;
  fs.writeFileSync(mainPkgPath, JSON.stringify(mainContent, null, 2) + '\n');
  console.log(`✓ npm packages prepared for version ${normalizedVersion}`);
  console.log(`  OptionalDependencies: ${Object.keys(optionalDeps).join(', ')}`);
} else {
  console.log(`✓ npm packages prepared for version ${normalizedVersion}`);
  console.log(`  Platforms: ${platforms.map(p => p.name).join(', ')}`);
  console.log(`  Run with --with-deps flag to add optionalDependencies`);
}
