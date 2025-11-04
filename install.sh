#!/bin/bash

set -e

echo "🌰 Carya Installation Script"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Error: Go is not installed."
    echo "Please install Go from https://go.dev/doc/install"
    exit 1
fi

echo "✓ Go $(go version | awk '{print $3}') detected"

# Build the project
echo "📦 Building Carya..."
go build -o carya ./cmd/carya

echo "✓ Build successful"

# Determine installation directory
INSTALL_DIR=""
if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ]; then
    INSTALL_DIR="$HOME/.local/bin"
else
    mkdir -p "$HOME/.local/bin"
    INSTALL_DIR="$HOME/.local/bin"
fi

# Install the binary
echo "📥 Installing to $INSTALL_DIR..."
cp carya "$INSTALL_DIR/carya"
chmod +x "$INSTALL_DIR/carya"

echo "✓ Carya installed successfully to $INSTALL_DIR/carya"
echo ""

# Install shell completions
echo "📝 Installing shell completions..."

# Detect shell and install completions
SHELL_NAME=$(basename "$SHELL")

case "$SHELL_NAME" in
    bash)
        if [ -d "$HOME/.local/share/bash-completion/completions" ]; then
            COMPLETION_DIR="$HOME/.local/share/bash-completion/completions"
        elif [ -d "/usr/local/etc/bash_completion.d" ]; then
            COMPLETION_DIR="/usr/local/etc/bash_completion.d"
        else
            mkdir -p "$HOME/.local/share/bash-completion/completions"
            COMPLETION_DIR="$HOME/.local/share/bash-completion/completions"
        fi
        "$INSTALL_DIR/carya" completion bash > "$COMPLETION_DIR/carya" 2>/dev/null || true
        echo "✓ Bash completions installed to $COMPLETION_DIR/carya"
        ;;
    zsh)
        if [ -d "$HOME/.zsh/completions" ]; then
            COMPLETION_DIR="$HOME/.zsh/completions"
        else
            mkdir -p "$HOME/.zsh/completions"
            COMPLETION_DIR="$HOME/.zsh/completions"
        fi
        "$INSTALL_DIR/carya" completion zsh > "$COMPLETION_DIR/_carya" 2>/dev/null || true
        echo "✓ Zsh completions installed to $COMPLETION_DIR/_carya"
        if [[ ":$FPATH:" != *":$COMPLETION_DIR:"* ]]; then
            echo "⚠️  Add the following to your ~/.zshrc:"
            echo "    fpath=($COMPLETION_DIR \$fpath)"
            echo "    autoload -Uz compinit && compinit"
        fi
        ;;
    fish)
        COMPLETION_DIR="$HOME/.config/fish/completions"
        mkdir -p "$COMPLETION_DIR"
        "$INSTALL_DIR/carya" completion fish > "$COMPLETION_DIR/carya.fish" 2>/dev/null || true
        echo "✓ Fish completions installed to $COMPLETION_DIR/carya.fish"
        ;;
    *)
        echo "⚠️  Shell completions available for bash, zsh, and fish"
        echo "   Run 'carya completion <shell>' to generate completions"
        ;;
esac

echo ""

# Check if installation directory is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo "⚠️  Warning: $INSTALL_DIR is not in your PATH"
    echo "Add the following line to your ~/.bashrc or ~/.zshrc:"
    echo ""
    echo "    export PATH=\"\$PATH:$INSTALL_DIR\""
    echo ""
fi

echo "✅ Installation complete! Run 'carya' to get started."
