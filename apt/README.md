# Quark apt Repository

## Installation

Add the repository and install Quark:

```bash
curl -fsSL https://crazy-vedic.github.io/Quark/apt/quark-archive-key.gpg \
  | sudo gpg --dearmor -o /usr/share/keyrings/quark-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/quark-archive-keyring.gpg] https://crazy-vedic.github.io/Quark/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/quark.list
sudo apt update && sudo apt install quark
```

## Verification

The repository is signed with a GPG key. Your system will automatically verify the signature using the key installed above.

To manually verify the Release file:

```bash
gpg --verify apt/dists/stable/Release.gpg apt/dists/stable/Release
```
