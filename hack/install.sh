#!/bin/sh
set -e

REPO="cloudygreybeard/jumpgate"
INSTALL_DIR="${JUMPGATE_INSTALL_DIR:-$HOME/bin}"
BINARY="jumpgate"

main() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
    esac

    case "$os" in
        linux|darwin) ;;
        *) echo "Unsupported OS: $os (use install.ps1 for Windows)" >&2; exit 1 ;;
    esac

    if [ -n "$JUMPGATE_VERSION" ]; then
        version="$JUMPGATE_VERSION"
    else
        version=$(curl -sI "https://github.com/$REPO/releases/latest" \
            | grep -i '^location:' \
            | sed 's|.*/v||;s/[[:space:]]//g')

        if [ -z "$version" ]; then
            echo "Failed to determine latest version" >&2
            exit 1
        fi
    fi

    tarball="${BINARY}_${version}_${os}_${arch}.tar.gz"
    url="https://github.com/$REPO/releases/download/v${version}/${tarball}"

    echo "Installing $BINARY v$version ($os/$arch)..."

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    curl -sL "$url" -o "$tmpdir/$tarball"
    tar xzf "$tmpdir/$tarball" -C "$tmpdir" "$BINARY"

    mkdir -p "$INSTALL_DIR"
    mv "$tmpdir/$BINARY" "$INSTALL_DIR/$BINARY"
    chmod +x "$INSTALL_DIR/$BINARY"

    echo "Installed $INSTALL_DIR/$BINARY (v$version)"
    echo ""

    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            echo "Add $INSTALL_DIR to your PATH:"
            echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
            echo ""
            ;;
    esac

    echo "Next steps:"
    echo "  jumpgate init --paste       # bootstrap from a local jumpgate payload"
    echo "  jumpgate init --from <dir>  # init from a site pack"
    echo "  jumpgate --help"
}

main
