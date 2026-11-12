# Manual Setup Requirements for Distribution Plan

All code fixes have been implemented. Complete these manual setup tasks to enable the full release pipeline.

## 1. GitHub Personal Access Token (PAT) - **REQUIRED**

The release workflow needs a PAT to trigger subsequent workflows (like apt publishing).

**Steps:**
1. Go to https://github.com/settings/tokens
2. Click "Generate new token" → "Generate new token (classic)"
3. Give it a name: `RELEASE_PAT`
4. Select scopes: Check **`repo`** (full repo access)
5. Click "Generate token"
6. Copy the token (you won't see it again!)
7. Go to https://github.com/crazy-vedic/Quark/settings/secrets/actions
8. Click "New repository secret"
9. Name: `RELEASE_PAT`
10. Value: Paste the token
11. Click "Add secret"

**Why:** The `GITHUB_TOKEN` cannot trigger new workflow runs. We use `RELEASE_PAT` instead.

---

## 2. GitHub Pages Setup - **REQUIRED**

The apt repository will be published to `gh-pages` branch on GitHub Pages.

**Steps:**
1. Go to https://github.com/crazy-vedic/Quark/settings/pages
2. Under "Build and deployment":
   - Source: Select **`Deploy from a branch`**
   - Branch: Select **`gh-pages`** with **`/ (root)`** folder
3. Click "Save"
4. Wait 1-2 minutes for GitHub to enable Pages
5. You should see a URL like: `https://crazy-vedic.github.io/Quark/`

**Why:** The apt repository will be hosted at this URL. Users can run:
```bash
curl -fsSL https://crazy-vedic.github.io/Quark/add-repo.sh | bash
```

---

## 3. Homebrew TAP Token - **VERIFY**

Check that the Homebrew tap token exists.

**Steps:**
1. Go to https://github.com/crazy-vedic/Quark/settings/secrets/actions
2. Verify that `HOMEBREW_TAP_TOKEN` exists in the list
3. If missing:
   - You'll need a token with permission to push to the `homebrew-quark` tap repository
   - This should have been created during M3 (Homebrew setup)
   - Contact the tap maintainer if needed

**Why:** Homebrew formula auto-publishes to the `crazy-vedic/homebrew-quark` tap.

---

## 4. APT Repository GPG Signing - **OPTIONAL**

For signed apt packages (recommended for production).

**Steps:**
1. (If you don't have a GPG key) Generate one:
   ```bash
   gpg --full-generate-key
   # Choose: RSA, 4096 bits, key valid for 2y, enter name/email, generate passphrase
   # Then export the key ID (shown in output, like ABCD1234)
   ```

2. Export your secret key (base64):
   ```bash
   gpg --export-secret-keys YOUR_KEY_ID | base64 --wrap=0
   ```
   Copy the entire base64 output.

3. Go to https://github.com/crazy-vedic/Quark/settings/variables/actions (for `APT_GPG_KEY_ID`) and https://github.com/crazy-vedic/Quark/settings/secrets/actions (for the others)

4. Add repository secrets and variables:
   - **Secret:** `APT_GPG_KEY` → Value: base64-encoded key (from step 2)
   - **Variable:** `APT_GPG_KEY_ID` → Value: `YOUR_KEY_ID`
   - **Secret or Environment Variable:** `APT_GPG_KEY_PASSPHRASE` → Your GPG passphrase
     - Can be set as a GitHub secret, or passed as an environment variable during the workflow run

5. The `publish-apt.yml` workflow will automatically detect and use these for signing.

**Why:** Signed packages prevent tampering and allow users to verify authenticity. Environment variable support allows flexible passphrase management without storing in GitHub secrets.

---

## Verification Checklist

- [ ] `RELEASE_PAT` secret exists
- [ ] GitHub Pages enabled on `gh-pages` branch
- [ ] `HOMEBREW_TAP_TOKEN` secret exists
- [ ] (Optional) GPG secrets configured if using signed apt

---

## What Happens Next

Once these are set up:

1. **Create a release PR** with:
   ```markdown
   Release: v0.1.0
   ```

2. **Merge the PR** → Triggers `tag-on-merge.yml`
   - Creates git tag `v0.1.0`
   - Generates completion files
   - Runs GoReleaser (creates archives)
   - Extracts binaries and regenerates checksums
   - Publishes all assets to GitHub release
   - Triggers `publish-apt.yml`

3. **APT repository** is updated
   - Packages published to `gh-pages` branch
   - Available at https://crazy-vedic.github.io/Quark/

4. **Homebrew formula** auto-publishes
   - Updates `homebrew-quark` tap
   - Available via `brew install crazy-vedic/quark/quark`

---

## Troubleshooting

**Workflows don't trigger after merging a PR?**
- Verify `RELEASE_PAT` secret is set correctly
- Check if the PR title contains `Release: vX.Y.Z` in the body
- Re-merge if needed

**GitHub Pages not working?**
- Verify Pages is enabled on the `gh-pages` branch
- Wait a few minutes after enabling
- Check https://crazy-vedic.github.io/Quark/ (should show index.html)

**Apt repo not working?**
- Check `publish-apt.yml` workflow logs for errors
- Verify GPG secrets are correct (if using signed packages)
- Check if `dists/` folder is being generated in `gh-pages` branch

**Homebrew formula not updating?**
- Verify `HOMEBREW_TAP_TOKEN` is set
- Check if the `homebrew-quark` tap repository exists
- Manually trigger the `homebrew-tap-validation.yml` workflow if needed
