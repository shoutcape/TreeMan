#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

archive="$TMP_DIR/treeman_linux_amd64.tar.gz"
checksums="$TMP_DIR/treeman_checksums.txt"
printf 'release archive\n' > "$archive"
checksum=$(sha256sum "$archive" | awk '{ print $1 }')
printf '%s  %s\n' "$checksum" "$(basename "$archive")" > "$checksums"

run_verify() {
  TREEMAN_INSTALLER_LIB_ONLY=1 bash -c 'source "$1"; verify_checksum "$2" "$3"' _ \
    "$SCRIPT_DIR/install.sh" "$archive" "$checksums"
}

run_verify

local_install_dir() {
  TREEMAN_LOCAL_INSTALLER_LIB_ONLY=1 bash -c 'source "$1"; local_install_dir' _ "$SCRIPT_DIR/install-local.sh"
}

prefix="$TMP_DIR/prefix"
mkdir -p "$prefix/bin"
touch "$prefix/bin/treeman"
chmod +x "$prefix/bin/treeman"
printf '%s\n' '#!/usr/bin/env bash' 'exit 1' > "$prefix/bin/brew"
chmod +x "$prefix/bin/brew"
detected_prefix=$(PATH="$prefix/bin:$PATH" local_install_dir)
if [[ "$detected_prefix" != "$prefix" ]]; then
  echo "local installer did not detect the active binary prefix" >&2
  exit 1
fi

homebrew_root="$TMP_DIR/homebrew"
homebrew_prefix="$homebrew_root/Cellar/treeman/0.3.25"
mkdir -p "$homebrew_prefix/bin" "$homebrew_root/bin"
touch "$homebrew_prefix/bin/treeman"
chmod +x "$homebrew_prefix/bin/treeman"
ln -s "../Cellar/treeman/0.3.25/bin/treeman" "$homebrew_root/bin/treeman"
mock_bin="$TMP_DIR/mock-bin"
mkdir "$mock_bin"
printf '%s\n' '#!/usr/bin/env bash' "printf '%s\\n' '$homebrew_prefix'" > "$mock_bin/brew"
chmod +x "$mock_bin/brew"
detected_prefix=$(PATH="$mock_bin:$PATH" local_install_dir)
if [[ "$detected_prefix" != "$homebrew_prefix" ]]; then
  echo "local installer did not detect the Homebrew binary prefix" >&2
  exit 1
fi

local_binary="$TMP_DIR/local-treeman"
shell_command_log="$TMP_DIR/shell-command.log"
cat > "$local_binary" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${TREEMAN_SHELL_COMMAND_LOG:?}"
EOF
chmod +x "$local_binary"
homebrew_shell_rc="$TMP_DIR/homebrew.zshrc"
touch "$homebrew_shell_rc"
SHELL=/usr/bin/zsh \
  TREEMAN_LOCAL_BIN="$local_binary" \
  TREEMAN_INSTALL_DIR="$homebrew_prefix" \
  TREEMAN_SHELL_COMMAND_LOG="$shell_command_log" \
  TREEMAN_SKIP_PATH_SETUP=1 \
  TREEMAN_SHELL_RC="$homebrew_shell_rc" \
  bash "$SCRIPT_DIR/install.sh" >/dev/null
if [[ ! -L "$homebrew_root/bin/treeman" ]]; then
  echo "local installer replaced the Homebrew symlink" >&2
  exit 1
fi
if ! cmp -s "$local_binary" "$homebrew_prefix/bin/treeman"; then
  echo "local installer did not update the Homebrew binary target" >&2
  exit 1
fi
if grep -qF "$homebrew_prefix/bin" "$homebrew_shell_rc"; then
  echo "local installer added a versioned Homebrew path to shell configuration" >&2
  exit 1
fi
if ! grep -qFx "shell install --shell zsh --config $homebrew_shell_rc" "$shell_command_log"; then
  echo "local installer did not configure shell integration through the installed binary" >&2
  exit 1
fi
if grep -qF -- '--path' "$shell_command_log"; then
  echo "local installer added a Homebrew versioned path to shell integration" >&2
  exit 1
fi

override_prefix="$TMP_DIR/override"
detected_prefix=$(TREEMAN_INSTALL_DIR="$override_prefix" local_install_dir)
if [[ "$detected_prefix" != "$override_prefix" ]]; then
  echo "local installer ignored TREEMAN_INSTALL_DIR" >&2
  exit 1
fi

printf 'wrong-checksum  %s\n' "$(basename "$archive")" > "$checksums"
if run_verify >/dev/null 2>&1; then
  echo "checksum mismatch unexpectedly passed" >&2
  exit 1
fi

printf 'deadbeef  other.tar.gz\n' > "$checksums"
if run_verify >/dev/null 2>&1; then
  echo "missing checksum entry unexpectedly passed" >&2
  exit 1
fi

echo "PASS"
