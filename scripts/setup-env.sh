#!/bin/bash
# Setup script for Letraz Utils development environment

set -e

echo "🔧 Setting up development environment for Letraz Utils..."

# Get Go path
GOPATH=$(go env GOPATH)
GOBIN="$GOPATH/bin"

# Check if Go bin directory exists
if [ ! -d "$GOBIN" ]; then
    echo "❌ Go bin directory not found: $GOBIN"
    exit 1
fi

# Detect shell
SHELL_NAME=$(basename "$SHELL")
SHELL_RC=""

case "$SHELL_NAME" in
    "bash")
        SHELL_RC="$HOME/.bashrc"
        if [ -f "$HOME/.bash_profile" ]; then
            SHELL_RC="$HOME/.bash_profile"
        fi
        ;;
    "zsh")
        SHELL_RC="$HOME/.zshrc"
        ;;
    *)
        echo "⚠️  Unknown shell: $SHELL_NAME"
        echo "Please manually add this to your shell configuration:"
        echo "export PATH=\"$GOBIN:\$PATH\""
        exit 0
        ;;
esac

# Check if already in PATH
if command -v air >/dev/null 2>&1; then
    echo "✅ Go tools are already in PATH"
else
    echo "📝 Adding Go bin directory to PATH in $SHELL_RC"
    
    # Add to shell RC file
    echo "" >> "$SHELL_RC"
    echo "# Go tools path (added by Letraz Utils setup)" >> "$SHELL_RC"
    echo "export PATH=\"$GOBIN:\$PATH\"" >> "$SHELL_RC"
    
    echo "✅ Added Go bin directory to PATH"
    echo "🔄 Please run: source $SHELL_RC"
    echo "   Or restart your terminal"
fi

# Install development tools
echo "🛠️  Installing development tools..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/air-verse/air@latest

echo "✅ Development environment setup complete!"
echo ""
echo "🚀 You can now use:"
echo "   make dev    # Start development server"
echo "   make hot    # Start with hot reload"
echo "   make help   # See all commands" 