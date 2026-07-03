#!/bin/bash
set -euo pipefail

# Quark install script
# Usage: curl -sL https://raw.githubusercontent.com/crazy-vedic/Quark/main/install.sh | bash
# Or: curl -sL ... | bash -s -- --version v1.0.0 --platform linux-amd64

REPO="crazy-vedic/Quark"
BINARY="quark"

# Default values
VERSION="latest"
PLATFORM=""

append_block_if_missing() {
    local target_file="$1"
    local marker="$2"
    local block="$3"
    local end_marker="# <<< quark completion <<<"

    mkdir -p "$(dirname "$target_file")"
    touch "$target_file"
    if grep -Fq "$marker" "$target_file" 2>/dev/null; then
        local tmp_file block_file
        tmp_file="$(mktemp)"
        block_file="$(mktemp)"
        printf "%s\n" "$block" > "$block_file"
        awk -v marker="$marker" -v end_marker="$end_marker" -v block_file="$block_file" '
            index($0, marker) {
                while ((getline line < block_file) > 0) {
                    print line
                }
                close(block_file)
                in_block = 1
                next
            }
            in_block && index($0, end_marker) {
                in_block = 0
                next
            }
            !in_block {
                print
            }
        ' "$target_file" > "$tmp_file"
        rm -f "$block_file"
        mv "$tmp_file" "$target_file"
        return 1
    fi
    printf "\n%s\n" "$block" >> "$target_file"
    return 0
}

install_shell_completion() {
    if [[ "$PLATFORM" == windows-* ]]; then
        return 0
    fi
    if [[ ! -x "$INSTALL_PATH" ]]; then
        return 0
    fi

    local shell_name
    shell_name="$(basename "${SHELL:-}")"

    case "$shell_name" in
        zsh|bash|fish)
            "$INSTALL_PATH" completion "$shell_name" --setup
            ;;
        *)
            echo "==> Skipping shell completion setup (unsupported shell: ${shell_name:-unknown})"
            ;;
    esac
}

install_warp_completion() {
    if [[ "$PLATFORM" == windows-* ]]; then
        return 0
    fi
    if [[ ! -x "$INSTALL_PATH" ]]; then
        return 0
    fi

    local plugin_dir plugin_file tmp_file
    plugin_dir="${HOME}/.warp/plugins/${BINARY}"
    plugin_file="${plugin_dir}/main.js"

    if ! mkdir -p "$plugin_dir"; then
        echo "==> Warning: failed to create Warp completion directory: ${plugin_dir}"
        return 0
    fi

    tmp_file="$(mktemp "${plugin_file}.tmp.XXXXXX")"
    if "$INSTALL_PATH" __warp_completion_plugin > "$tmp_file"; then
        if mv "$tmp_file" "$plugin_file"; then
            echo "==> Installed Warp completions to ${plugin_file}"
            echo "==> In Warp, run /reload-plugins or restart Warp"
            return 0
        fi
    fi

    rm -f "$tmp_file"
    echo "==> Warning: failed to install Warp completions"
    return 0
}

has_warp_installed() {
    if [[ -d "${HOME}/.warp" ]]; then
        return 0
    fi
    if command -v warp >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

prompt_install_warp_completion() {
    if [[ "$PLATFORM" == windows-* ]]; then
        return 0
    fi
    if ! has_warp_installed; then
        return 0
    fi
    if [[ ! -r /dev/tty ]]; then
        echo "==> Skipping Warp completion prompt (no interactive TTY available)"
        return 0
    fi

    local reply
    printf "Do you want to install Warp auto-complete for quark? [Y/n] (Y) " > /dev/tty
    IFS= read -r reply < /dev/tty || reply=""
    reply="$(printf '%s' "$reply" | tr '[:upper:]' '[:lower:]')"
    case "$reply" in
        ""|y|yes)
            install_warp_completion
            ;;
        *)
            echo "==> Skipped Warp completion setup"
            ;;
    esac
}

install_autocomplete() {
    install_shell_completion
    prompt_install_warp_completion
}

prompt_install_shell_completion() {
    if [[ "$PLATFORM" == windows-* ]]; then
        return 0
    fi
    if [[ ! -t 1 ]]; then
        echo "==> Skipping shell completion prompt (stdout is not a terminal)"
        return 0
    fi
    if [[ ! -r /dev/tty ]]; then
        echo "==> Skipping shell completion prompt (no interactive TTY available)"
        return 0
    fi

    local reply
    printf "Do you want to install auto-complete for quark? [Y/n] (Y) " > /dev/tty
    IFS= read -r reply < /dev/tty || reply=""
    reply="$(printf '%s' "$reply" | tr '[:upper:]' '[:lower:]')"
    case "$reply" in
        ""|y|yes)
            install_autocomplete
            ;;
        *)
            echo "==> Skipped shell completion setup"
            ;;
    esac
}

if [[ "${QUARK_INSTALL_LIBRARY_MODE:-}" == "1" ]]; then
    return 0 2>/dev/null || exit 0
fi

# Parse arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --platform)
      PLATFORM="$2"
      shift 2
      ;;
    -v|--version-short)
      VERSION="$2"
      shift 2
      ;;
    -p|--platform-short)
      PLATFORM="$2"
      shift 2
      ;;
    --help|-h)
      echo "Quark install script"
      echo ""
      echo "Usage:"
      echo "  curl -sL https://raw.githubusercontent.com/crazy-vedic/Quark/main/install.sh | bash"
      echo ""
      echo "Options (pass as arguments):"
      echo "  --version <version>   Install specific version (default: latest)"
      echo "  --platform <platform> Override platform detection (e.g. linux-amd64, darwin-arm64, windows-amd64)"
      echo ""
      echo "Examples:"
      echo "  curl -sL ... | bash -s -- --version v1.0.0"
      echo "  curl -sL ... | bash -s -- --platform linux-amd64"
      echo "  curl -sL ... | bash -s -- --version v1.0.0 --platform darwin-arm64"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      echo "Run with --help for usage"
      exit 1
      ;;
  esac
done

# If platform is manually specified, validate it
if [[ -n "$PLATFORM" ]]; then
  case "$PLATFORM" in
    linux-amd64|linux-arm64|darwin-amd64|darwin-arm64|windows-amd64)
      ;;
    *)
      echo "Invalid platform: $PLATFORM"
      echo "Supported: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64"
      exit 1
      ;;
  esac
else
  # Auto-detect OS
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$OS" in
      linux)   PLATFORM_OS="linux" ;;
      darwin)  PLATFORM_OS="darwin" ;;
      msys*|cygwin*|mingw*)
          PLATFORM_OS="windows"
          ;;
      *)
          echo "Unsupported OS: $OS"
          echo "Use --platform <platform> to specify manually"
          echo "Supported: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64"
          exit 1
          ;;
  esac

  # Auto-detect architecture
  ARCH=$(uname -m)
  case "$ARCH" in
      x86_64|amd64)  PLATFORM_ARCH="amd64" ;;
      arm64|aarch64) PLATFORM_ARCH="arm64" ;;
      *)
          echo "Unsupported architecture: $ARCH"
          echo "Use --platform <platform> to specify manually"
          echo "Supported: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64"
          exit 1
          ;;
  esac

  PLATFORM="${PLATFORM_OS}-${PLATFORM_ARCH}"
fi

# Windows is amd64 only (no arm64 release yet)
if [[ "$PLATFORM" == "windows-arm64" ]]; then
  echo "Windows ARM64 is not yet supported. Falling back to amd64 with emulation."
  PLATFORM="windows-amd64"
fi

ASSET="${BINARY}-${PLATFORM}"

# Resolve download URL
if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
else
    URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
fi

# Determine install directory
if [[ "$PLATFORM" == windows-* ]]; then
    # Windows: prefer a directory that's likely on PATH
    if command -v cygpath &> /dev/null; then
        # Git Bash / MSYS2: use /usr/bin or ~/bin
        if [ -d /usr/bin ] && [ -w /usr/bin ]; then
            INSTALL_DIR="/usr/bin"
        else
            INSTALL_DIR="$HOME/bin"
            mkdir -p "$INSTALL_DIR"
        fi
    else
        # Native Windows (PowerShell should use install.ps1 instead)
        echo "For native Windows installation, use PowerShell:"
        echo "  Invoke-RestMethod -Uri https://raw.githubusercontent.com/crazy-vedic/Quark/main/install.ps1 | Invoke-Expression"
        exit 1
    fi
    INSTALL_PATH="${INSTALL_DIR}/${BINARY}.exe"
else
    # Unix-like
    if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        INSTALL_DIR="/usr/local/bin"
    elif [ -d "$HOME/.local/bin" ] && [ -w "$HOME/.local/bin" ]; then
        INSTALL_DIR="$HOME/.local/bin"
    else
        INSTALL_DIR="$HOME/bin"
        mkdir -p "$INSTALL_DIR"
    fi
    INSTALL_PATH="${INSTALL_DIR}/${BINARY}"
fi

# Build download commands with auth headers if available.
AUTH="${GITHUB_TOKEN:-${GITHUB_ACCESS_TOKEN:-}}"
CURL=(curl -fsSL)
if [[ -n "$AUTH" ]]; then
    CURL+=(-H "Authorization: token ${AUTH}")
fi

download_with_gh() {
    if ! command -v gh &>/dev/null; then
        return 1
    fi

    local gh_args=(release download --repo "$REPO" --pattern "$ASSET" --output "$INSTALL_PATH" --clobber)
    if [[ "$VERSION" != "latest" ]]; then
        gh_args=(release download "$VERSION" --repo "$REPO" --pattern "$ASSET" --output "$INSTALL_PATH" --clobber)
    fi

    if [[ -n "$AUTH" ]]; then
        GH_TOKEN="$AUTH" gh "${gh_args[@]}"
    else
        gh "${gh_args[@]}"
    fi
}

echo "==> Installing Quark ${VERSION} for ${PLATFORM}"
echo "==> Downloading ${ASSET}"

if ! download_with_gh; then
    "${CURL[@]}" "$URL" -o "$INSTALL_PATH"
fi
chmod +x "$INSTALL_PATH"

echo "==> Installed to ${INSTALL_PATH}"

# Check if it's on PATH
if ! command -v "$BINARY" &> /dev/null; then
    echo ""
    echo "⚠️  ${INSTALL_DIR} is not on your PATH."
    echo "   Add this to your shell profile:"
    echo "   export PATH=\"${INSTALL_DIR}:\$PATH\""
fi

echo ""
echo "✅ Quark ${VERSION} installed!"
if [[ "$PLATFORM" == windows-* ]]; then
    echo "   Run: quark.exe --help"
else
    echo "   Run: quark --help"
fi
echo ""
prompt_install_shell_completion
