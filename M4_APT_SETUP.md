# M4: apt Repository on gh-pages

This document describes setting up an apt package repository served from GitHub Pages.

## Architecture

The apt repository is:
- Built and published to the `gh-pages` branch via GitHub Actions
- Served at: `https://crazy-vedic.github.io/Quark/apt`
- Packages stored in `dists/stable/main/binary-{amd64,arm64}/`
- Managed with `reprepro` (standard Debian tool)

## Prerequisites

### 1. Enable GitHub Pages

In the Quark repository:
- Settings → Pages
- Source: Deploy from a branch
- Branch: `gh-pages`
- Folder: `/ (root)`
- Save

### 2. Install reprepro (for local testing)

```bash
# macOS
brew install reprepro

# Ubuntu/Debian
sudo apt install reprepro

# Fedora/RHEL
sudo dnf install reprepro
```

### 3. Create GPG Key for Signing (Optional but Recommended)

For production, create a dedicated GPG key:

```bash
gpg --full-generate-key
# Select: RSA, 4096 bits, no expiration
# Name: Quark Releases
# Email: releases@github.com
# Passphrase: strong password (store in GitHub secret)

# Export the key
gpg --export-secret-keys KEY_ID > quark-release-key.gpg
gpg --export KEY_ID > quark-release-key.pub

# Add to GitHub secrets (base64 encoded):
cat quark-release-key.gpg | base64 -w0 | pbcopy  # macOS
# Or: cat quark-release-key.gpg | base64 -w0 | xclip -selection clipboard  # Linux
```

Store these as GitHub secrets:
- `APT_GPG_KEY`: base64-encoded private key
- `APT_GPG_KEY_PASSPHRASE`: key passphrase
- `APT_GPG_KEY_ID`: key ID (last 8 chars of fingerprint)

## Repository Structure

```
gh-pages/
├── apt/
│   ├── conf/
│   │   └── distributions  (reprepro config)
│   ├── dists/
│   │   └── stable/
│   │       └── Release (signed)
│   ├── pool/
│   │   └── main/
│   │       └── q/
│   │           └── quark/
│   │               └── quark_X.Y.Z_amd64.deb
│   └── db/ (reprepro database)
└── index.html
```

## Installation

After setup, users install with:

```bash
# Add the repository
echo "deb https://crazy-vedic.github.io/Quark/apt stable main" | \
  sudo tee /etc/apt/sources.list.d/quark.list

# Add the public key (unsigned, no signature check initially)
curl -fsSL https://crazy-vedic.github.io/Quark/apt/KEY.pub | \
  sudo apt-key add -

# Update and install
sudo apt update
sudo apt install quark
```

Or with automatic key management (apt-transport-https):

```bash
sudo apt install apt-transport-https
echo "deb [trusted=yes] https://crazy-vedic.github.io/Quark/apt stable main" | \
  sudo tee /etc/apt/sources.list.d/quark.list
sudo apt update
sudo apt install quark
```

## Workflow Integration

The `.github/workflows/publish-apt.yml` workflow:

1. Triggered on release creation
2. Downloads .deb artifacts from release
3. Initializes/updates reprepro database
4. Adds packages to the `stable` distribution
5. Signs Release file (if GPG configured)
6. Pushes to `gh-pages` branch
7. GitHub Pages automatically serves the files

## Local Testing

Test the repository locally before publishing:

```bash
# Initialize reprepro directory
mkdir -p quark-apt/conf
cat > quark-apt/conf/distributions <<'EOF'
Origin: Quark
Label: Quark
Suite: stable
Codename: stable
Version: 1.0
Components: main
Architectures: amd64 arm64
SignWith: KEY_ID
EOF

# Add a package (unsigned)
cd quark-apt
reprepro -S main includedeb stable ../quark_1.0.0_amd64.deb

# List packages
reprepro list stable

# Export Release file
reprepro -S main createsymlinks
```

## Troubleshooting

- **"reprepro: Does not match Release"**: Database corrupted, delete `db/` directory
- **GPG signature fails**: Verify key ID and passphrase in secrets
- **GitHub Pages not serving**: Verify `gh-pages` branch exists and Pages is enabled
- **apt update fails**: Check sources.list URL matches GitHub Pages serving path

## First Release Process

1. Create and push a tag (e.g., `v1.0.0`)
2. GoReleaser builds and publishes release
3. Release workflow trigger: Publish to apt repository
4. Wait for workflow to complete (check Actions tab)
5. Test: `sudo apt update && apt-cache policy quark`

## Maintenance

- Old packages accumulate in `pool/` but don't affect functionality
- Periodically clean up old versions:
  ```bash
  reprepro list stable
  reprepro remove stable quark_OLD_VERSION_*
  ```
- Monitor disk usage of gh-pages branch (typically < 100MB)
