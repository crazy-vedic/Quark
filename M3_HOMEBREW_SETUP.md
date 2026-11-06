# M3: Homebrew Tap Setup Guide

This document describes the setup required for Homebrew distribution via the `crazy-vedic/homebrew-quark` tap.

## Prerequisites

### 1. Create the Homebrew Tap Repository

The tap repository must be created manually on GitHub:

- **Repository name**: `homebrew-quark`
- **Owner**: `crazy-vedic` account
- **Visibility**: Public
- **Description**: "Homebrew tap for Quark"
- **Default branch**: `main`

Repository URL: `https://github.com/crazy-vedic/homebrew-quark`

### 2. Generate and Configure GitHub Token

GoReleaser needs a GitHub token to push to the tap repository:

1. Create a Personal Access Token (PAT) with repo access:
   - Go to GitHub Settings → Developer settings → Personal access tokens
   - Click "Tokens (classic)"
   - Click "Generate new token (classic)"
   - Select scopes: `repo` (full control of private repositories)
   - Copy the token value

2. Add the token to the Quark repository as a secret:
   - Go to Quark repository Settings → Secrets and variables → Actions
   - Click "New repository secret"
   - Name: `HOMEBREW_TAP_TOKEN`
   - Value: paste the token from step 1

### 3. Set Up Tap CI (Optional but Recommended)

The tap repository should include a GitHub Actions workflow to validate formulas:

- Copy the template from `.github/workflows/tap-validation.yml` (in this repo, if created)
- Place it in `homebrew-quark/.github/workflows/tap-validation.yml`
- This runs `brew audit --strict` on every push to validate the formula

## How It Works

1. When a release is created/tagged in the Quark repository, the release workflow runs
2. GoReleaser builds cross-platform binaries
3. GoReleaser automatically creates a pull request in `homebrew-quark` with the updated formula
4. The PR includes:
   - Updated version number
   - New binary checksums
   - Updated download URLs
5. The tap CI validates the formula (if configured)
6. Manual merge and release via the tap's release workflow

## Formula Location

Once created, the formula will be at:
`homebrew-quark/Formula/quark.rb`

## Installation Command

After setup, users can install with:
```bash
brew tap crazy-vedic/quark
brew install quark
```

Or directly:
```bash
brew install crazy-vedic/quark/quark
```

## Verification

Test the formula locally:
```bash
brew install crazy-vedic/quark/quark --verbose
quark version
```

## Troubleshooting

- **Token issues**: Verify the token has `repo` scope and hasn't expired
- **Repository not found**: Ensure `homebrew-quark` exists and is public
- **Formula validation fails**: Check the GitHub Actions output in the tap repo
