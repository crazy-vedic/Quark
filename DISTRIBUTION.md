# Quark Distribution Channels

This document covers the process for distributing Quark through winget and npm, as well as existing channels (Homebrew, apt).

---

## Distribution Channels

| Channel | Install Command | Managed By | Automatic | Notes |
|---------|-----------------|-----------|-----------|-------|
| **Direct** | `curl -sL ... \| bash` | GitHub Releases | ✅ | Fastest, no gatekeeper |
| **Homebrew** | `brew install quark` | `homebrew-quark` tap | ✅ | macOS/Linux |
| **apt** | `apt install quark` | GitHub Pages (`gh-pages`) | ✅ | Debian/Ubuntu |
| **winget** | `winget install Quark.Quark` | microsoft/winget-pkgs | ❌ | Windows, manual PR required |
| **npm** | `npm i -g @quark-client/quark` | npm registry | ✅ | All platforms, OIDC trusted publishing |

---

## Release Automation

When you merge a PR with `Release: vX.Y.Z` in the body:

1. **tag-on-merge.yml** validates version, creates tag, and pushes
2. **release job** builds binaries, creates GitHub Release, uploads artifacts
3. **publish-npm job** automatically publishes to npm registry (OIDC)
4. **submit-winget job** either:
   - Opens PR to microsoft/winget-pkgs (if `WINGET_PAT` configured)
   - Prints manual submission instructions (if no token)

---

## npm Distribution

### Setup (One-Time)

1. **npm organization** (`quark-client`) — already created
2. **OIDC trusted publishing** — requires package to exist first:
   ```bash
   # Build and pack a prerelease
   node scripts/npm-pack.mjs 1.0.0-rc.1
   
   # Publish from npm/ directory
   cd npm
   npm publish platforms/linux-x64 --tag next
   npm publish platforms/linux-arm64 --tag next
   npm publish platforms/darwin-x64 --tag next
   npm publish platforms/darwin-arm64 --tag next
   npm publish platforms/win32-x64 --tag next
   npm publish quark --tag next
   ```

3. **Configure OIDC** in npm settings (after package exists):
   - Visit https://www.npmjs.com/settings/tokens
   - "Automation" → "Configure OIDC"
   - Add GitHub as trusted publisher:
     - Provider: GitHub
     - Owner: `crazy-vedic`
     - Repository: `Quark`
     - Save

### Release Process

Automatic on every release:
- Platform packages published first (to avoid dangling optionalDependencies)
- Main package published last
- Provenance recorded by npm

### Users

```bash
# Install globally
npm i -g @quark-client/quark

# Run with npx
npx @quark-client/quark

# Update
npm i -g @quark-client/quark@latest
```

### Architecture

```
@quark-client/quark (main)
├── bin/quark.js (shim)
├── optionalDependencies:
│   ├── @quark-client/quark-linux-x64
│   ├── @quark-client/quark-linux-arm64
│   ├── @quark-client/quark-darwin-x64
│   ├── @quark-client/quark-darwin-arm64
│   └── @quark-client/quark-win32-x64
```

npm installs only the package matching `process.platform` and `process.arch`. The shim routes to the matching binary.

---

## Winget Distribution

### First-Time Setup

The first submission to `microsoft/winget-pkgs` requires manual steps. After approval, subsequent releases are easier (manifest auto-generated, auto-submitted if token configured).

### Manual Submission Steps

These steps apply to the first release or if `WINGET_PAT` is not configured.

#### 1. Prepare

Ensure your release is published on GitHub with:
- `quark-windows-amd64.zip` (Windows executable)
- `SHA256SUMS` (containing the zip's hash)

#### 2. Fork and Clone

```bash
# Fork microsoft/winget-pkgs on GitHub (one-time)
# Then clone your fork
git clone https://github.com/<your-username>/winget-pkgs.git
cd winget-pkgs
git checkout -b Quark.Quark-v1.0.0  # version number
```

#### 3. Create Manifest Files

Manifests live in: `manifests/q/Quark/Quark/<version>/`

Create three YAML files (example for v1.0.0):

**File 1: `manifests/q/Quark/Quark/1.0.0/Quark.Quark.yaml`**
```yaml
PackageIdentifier: Quark.Quark
PackageVersion: 1.0.0
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.4.0
```

**File 2: `manifests/q/Quark/Quark/1.0.0/Quark.Quark.installer.yaml`**
```yaml
PackageIdentifier: Quark.Quark
PackageVersion: 1.0.0
Platform:
  - Windows.Desktop
MinimumOSVersion: 10.0.0.0
NestedInstallerType: portable
Installers:
  - Architecture: x64
    InstallerUrl: https://github.com/crazy-vedic/Quark/releases/download/v1.0.0/quark-windows-amd64.zip
    InstallerSha256: <HASH_FROM_SHA256SUMS>
    InstallerType: zip
    NestedInstallerFiles:
      - RelativeFilePath: quark.exe
        PortableCommandAlias: quark
ManifestType: installer
ManifestVersion: 1.4.0
```

Replace `<HASH_FROM_SHA256SUMS>` with the SHA256 hash from the release's `SHA256SUMS` file.

**File 3: `manifests/q/Quark/Quark/1.0.0/Quark.Quark.locale.en-US.yaml`**
```yaml
PackageIdentifier: Quark.Quark
PackageVersion: 1.0.0
PackageLocale: en-US
Publisher: Quark
PackageName: Quark
PackageUrl: https://github.com/crazy-vedic/Quark
License: MIT
LicenseUrl: https://github.com/crazy-vedic/Quark/blob/master/LICENSE
ShortDescription: A local-first, keyboard-driven HTTP client
Description: Quark is a local-first, keyboard-driven HTTP client. No cloud. No accounts. No data leaves your machine.
Tags:
  - http
  - client
  - curl
  - rest
  - api
  - keyboard-driven
ManifestType: defaultLocale
ManifestVersion: 1.4.0
```

#### 4. Validate Locally

```bash
# Requires winget CLI (Windows only)
winget validate manifests/q/Quark/Quark/1.0.0
```

All three files must be present and pass schema validation.

#### 5. Test in Windows Sandbox (Recommended)

From the winget-pkgs repo, use the provided test script:

```powershell
# From winget-pkgs root, PowerShell 5+
.\Tools\SandboxTest.ps1 manifests/q/Quark/Quark/1.0.0
```

This installs Quark in an isolated Windows Sandbox and verifies:
- Installation succeeds silently
- `quark version` runs
- Uninstall succeeds

#### 6. Commit and Push

```bash
git add manifests/q/Quark/Quark/1.0.0
git commit -m "Add Quark.Quark version 1.0.0"
git push origin Quark.Quark-v1.0.0
```

#### 7. Open Pull Request

Go to https://github.com/microsoft/winget-pkgs and create a PR from your fork's branch to `microsoft/winget-pkgs:master`.

**PR title:**
```
Add Quark.Quark version 1.0.0
```

**PR body:**
```
Submitting Quark.Quark for inclusion in winget.

- Package: Quark.Quark
- Version: 1.0.0
- Installer: GitHub release asset (zip)
- Homepage: https://github.com/crazy-vedic/Quark
```

#### 8. Wait for Validation

Microsoft's automation will:
- Validate manifest schema
- Check URLs and hashes
- Scan the installer for antivirus issues
- Run installation tests

Labels will appear on the PR:
- ✅ `Validation-Passed` → Ready for manual review
- ❌ `Binary-Validation-Error` → Antivirus flagged; need to sign binary or resubmit
- ❌ `Validation-*-Error` → Fix manifest and push update to same branch

#### 9. Address Feedback

If Microsoft requests changes:
- Edit the manifests
- Push to the same branch
- The PR auto-updates

Common issues:
- `Binary-Validation-Error` → Binary unsigned. Either sign it or wait for false-positive review.
- `Validation-Hash-Mismatch` → SHA256 in manifest doesn't match the zip. Verify the hash.
- `Validation-Indirect-URL` → URL is a redirect. Use direct GitHub release URL.

#### 10. Merge

Once approved, Microsoft merges the PR. The package goes live on winget within hours.

---

### Automated Submission (with WINGET_PAT)

If you add a `WINGET_PAT` secret (GitHub PAT with `public_repo` scope), the `submit-winget` job automatically:
1. Generates manifests from the release tag
2. Forks `microsoft/winget-pkgs` (if needed)
3. Creates a branch
4. Commits manifests
5. Opens PR

Still requires monitoring for validation results and addressing feedback.

---

## Known Risks & Mitigations

### Antivirus False Positives

**Risk:** Go binary with self-update capability may trigger heuristics.

**Mitigations:**
1. Keep the binary unsigned initially
2. If Microsoft's scan flags it, submit to Microsoft Defender for analysis:
   - https://www.microsoft.com/wdsi/filesubmission
3. Resubmit after analysis
4. Long-term: Consider code signing (costs money, adds CI step)

### Update Lag

**Risk:** Delay between GitHub release and winget package availability.

**Mitigation:** Users can fall back to direct install or npm in the meantime. Document this in README.

### Version Mismatch

**Risk:** Manifest version doesn't match folder path or the actual binary.

**Mitigation:** Use the release workflow's `submit-winget` job which auto-generates manifests from the tag.

---

## Troubleshooting

### Q: How do I test the manifest locally without winget?

Run `winget validate <folder>` on Windows, or inspect the YAML files for schema errors.

### Q: Can I skip winget for a release?

Yes. Omit the `Release: vX.Y.Z` line in the PR body (same as skipping any channel).

### Q: How do I update an existing winget package?

Submit a new PR with the higher version number in the same folder structure. Microsoft's bot handles version upgrades.

### Q: What if the binary is flagged by antivirus?

1. Submit to Microsoft Defender: https://www.microsoft.com/wdsi/filesubmission
2. Wait for analysis (usually 48-72 hours)
3. If approved, resubmit to winget-pkgs (same PR, push new commit)
4. If still flagged, consider code signing

---

## Checklist for First Winget Release

- [ ] Release published on GitHub with Windows zip and SHA256SUMS
- [ ] Fork created at `https://github.com/<username>/winget-pkgs`
- [ ] Manifest folder created: `manifests/q/Quark/Quark/<version>/`
- [ ] Three YAML files created and valid
- [ ] `winget validate` passes (on Windows)
- [ ] `Tools/SandboxTest.ps1` passes (on Windows)
- [ ] Committed and pushed to feature branch
- [ ] PR opened to `microsoft/winget-pkgs:master`
- [ ] Validation passed (`Validation-Passed` label)
- [ ] Manual review completed and merged

---

## References

- Microsoft Winget Repository: https://github.com/microsoft/winget-pkgs
- Winget Manifest Schema: https://learn.microsoft.com/en-us/windows/package-manager/package/manifest
- Submission Guide: https://learn.microsoft.com/en-us/windows/package-manager/package/repository
