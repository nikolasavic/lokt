#!/bin/sh
#
# Install lokt - file-based lock manager
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nikolasavic/lokt/main/scripts/install.sh | sh
#
# Environment variables:
#   LOKT_VERSION     - Version to install (default: latest)
#   LOKT_INSTALL_DIR - Install directory (default: ~/.local/bin or /usr/local/bin)
#
# Supported platforms:
#   - darwin (macOS): amd64, arm64
#   - linux: amd64, arm64
#

set -e

REPO="nikolasavic/lokt"
GITHUB_API="https://api.github.com"
GITHUB_RELEASES="https://github.com/$REPO/releases"

# Colors (disabled if not a terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BOLD=''
    NC=''
fi

info() {
    printf "${GREEN}==>${NC} %s\n" "$1"
}

warn() {
    printf "${YELLOW}Warning:${NC} %s\n" "$1" >&2
}

error() {
    printf "${RED}Error:${NC} %s\n" "$1" >&2
    exit 1
}

# Detect operating system
detect_os() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        darwin)
            echo "darwin"
            ;;
        linux)
            echo "linux"
            ;;
        mingw*|msys*|cygwin*)
            error "Windows is not supported. Lokt uses Unix file locking semantics."
            ;;
        *)
            error "Unsupported operating system: $os"
            ;;
    esac
}

# Detect architecture
detect_arch() {
    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        *)
            error "Unsupported architecture: $arch"
            ;;
    esac
}

# Find a download tool (curl or wget)
detect_downloader() {
    if command -v curl >/dev/null 2>&1; then
        echo "curl"
    elif command -v wget >/dev/null 2>&1; then
        echo "wget"
    else
        error "Neither curl nor wget found. Please install one of them."
    fi
}

# Find a checksum tool
detect_checksum_tool() {
    if command -v sha256sum >/dev/null 2>&1; then
        echo "sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        echo "shasum"
    elif command -v openssl >/dev/null 2>&1; then
        echo "openssl"
    else
        error "No SHA256 tool found. Please install sha256sum, shasum, or openssl."
    fi
}

# Download a file
# Usage: download URL OUTPUT_FILE
download() {
    url="$1"
    output="$2"
    downloader=$(detect_downloader)

    case "$downloader" in
        curl)
            curl -fsSL "$url" -o "$output"
            ;;
        wget)
            wget -q "$url" -O "$output"
            ;;
    esac
}

# Download to stdout
# Usage: download_stdout URL
download_stdout() {
    url="$1"
    downloader=$(detect_downloader)

    case "$downloader" in
        curl)
            curl -fsSL "$url"
            ;;
        wget)
            wget -qO- "$url"
            ;;
    esac
}

# Verify SHA256 checksum
# Usage: verify_checksum FILE EXPECTED_HASH
verify_checksum() {
    file="$1"
    expected="$2"
    tool=$(detect_checksum_tool)

    case "$tool" in
        sha256sum)
            actual=$(sha256sum "$file" | cut -d' ' -f1)
            ;;
        shasum)
            actual=$(shasum -a 256 "$file" | cut -d' ' -f1)
            ;;
        openssl)
            actual=$(openssl dgst -sha256 "$file" | awk '{print $NF}')
            ;;
    esac

    if [ "$actual" != "$expected" ]; then
        error "Checksum mismatch!
  Expected: $expected
  Actual:   $actual
This could indicate a corrupted download or a security issue."
    fi
}

# Get the latest release version from GitHub
get_latest_version() {
    url="$GITHUB_API/repos/$REPO/releases/latest"

    # Try with GitHub token if available (helps with rate limiting)
    if [ -n "$GITHUB_TOKEN" ]; then
        version=$(download_stdout "$url" | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    else
        version=$(download_stdout "$url" | grep '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
    fi

    if [ -z "$version" ]; then
        error "Failed to fetch latest version. GitHub API may be rate-limited.
Try setting GITHUB_TOKEN or specify LOKT_VERSION manually."
    fi

    echo "$version"
}

# Determine install directory
# Returns the directory path (caller handles sudo if needed)
get_install_dir() {
    # 1. User-specified directory
    if [ -n "$LOKT_INSTALL_DIR" ]; then
        echo "$LOKT_INSTALL_DIR"
        return
    fi

    # 2. Existing install — upgrade in-place
    existing=$(command -v lokt 2>/dev/null || true)
    if [ -n "$existing" ]; then
        dirname "$existing"
        return
    fi

    # 3. ~/.local/bin if it exists and is writable
    local_bin="$HOME/.local/bin"
    if [ -d "$local_bin" ] && [ -w "$local_bin" ]; then
        echo "$local_bin"
        return
    fi

    # 4. Create ~/.local/bin if $HOME is writable
    if [ -w "$HOME" ]; then
        mkdir -p "$local_bin"
        echo "$local_bin"
        return
    fi

    # 5. Fall back to /usr/local/bin (may need sudo)
    echo "/usr/local/bin"
}

# Check if directory is writable
is_writable() {
    dir="$1"
    if [ -d "$dir" ]; then
        [ -w "$dir" ]
    else
        # Check if parent is writable (for creating the dir)
        parent=$(dirname "$dir")
        [ -w "$parent" ]
    fi
}

main() {
    info "Installing lokt..."

    # Detect platform
    os=$(detect_os)
    arch=$(detect_arch)
    info "Detected platform: ${os}/${arch}"

    # Determine version
    version="${LOKT_VERSION:-}"
    if [ -z "$version" ]; then
        info "Fetching latest version..."
        version=$(get_latest_version)
    fi
    info "Version: $version"

    # Strip 'v' prefix for archive name (goreleaser uses version without v)
    version_num="${version#v}"

    # Build download URLs
    archive_name="lokt_${version_num}_${os}_${arch}.tar.gz"
    archive_url="$GITHUB_RELEASES/download/$version/$archive_name"
    checksums_url="$GITHUB_RELEASES/download/$version/checksums.txt"

    # Create temp directory
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT

    # Download archive and checksums
    info "Downloading $archive_name..."
    download "$archive_url" "$tmp_dir/$archive_name"

    info "Downloading checksums..."
    download "$checksums_url" "$tmp_dir/checksums.txt"

    # Extract expected checksum for our archive
    expected_checksum=$(grep "$archive_name" "$tmp_dir/checksums.txt" | cut -d' ' -f1)
    if [ -z "$expected_checksum" ]; then
        error "Could not find checksum for $archive_name in checksums.txt"
    fi

    # Verify checksum
    info "Verifying checksum..."
    verify_checksum "$tmp_dir/$archive_name" "$expected_checksum"
    info "Checksum verified!"

    # Extract archive
    info "Extracting..."
    tar -xzf "$tmp_dir/$archive_name" -C "$tmp_dir"

    # Determine install directory
    install_dir=$(get_install_dir)
    info "Install directory: $install_dir"

    # Install binary
    if is_writable "$install_dir"; then
        mkdir -p "$install_dir"
        cp "$tmp_dir/lokt" "$install_dir/lokt"
        chmod +x "$install_dir/lokt"
    else
        info "Requesting sudo to install to $install_dir..."
        sudo mkdir -p "$install_dir"
        sudo cp "$tmp_dir/lokt" "$install_dir/lokt"
        sudo chmod +x "$install_dir/lokt"
    fi

    # Verify installation
    if [ -x "$install_dir/lokt" ]; then
        info "lokt installed successfully!"

        # Check if install_dir is in PATH
        case ":$PATH:" in
            *":$install_dir:"*)
                # Already in PATH
                "$install_dir/lokt" version
                ;;
            *)
                warn "$install_dir is not in your PATH"
                printf "\nAdd it to your shell config:\n"
                printf "  ${BOLD}export PATH=\"%s:\$PATH\"${NC}\n\n" "$install_dir"
                printf "Then run: %slokt version%s\n" "$BOLD" "$NC"
                ;;
        esac
    else
        error "Installation failed - binary not found at $install_dir/lokt"
    fi
}

# Wrap in main function to prevent partial execution
main "$@"
