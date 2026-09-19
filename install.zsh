#!/usr/bin/env -S zsh -f
#
# agent-env installer.
#
# Downloads the agent-env release tarball for this platform, verifies its
# sha256, and installs the binary. Safe to run through a pipe:
#
#   curl -fsSL https://raw.githubusercontent.com/yanickxia/agent-env/master/install.zsh | zsh
#
# The script never reads stdin, so the piped form works.

emulate -L zsh
setopt errexit nounset pipefail

readonly error_prefix="agent-env install:"
readonly repo="yanickxia/agent-env"
readonly default_bin_dir="${HOME}/.local/bin"

usage() {
  cat <<'EOF'
Usage:
  zsh install.zsh [install|uninstall] [--version vX.Y.Z] [--bin-dir DIR] [-h|--help]

Commands:
  install     Download the agent-env release tarball for this platform, verify
              its sha256, and install the binary. This is the default when no
              command is given (so `curl ... | zsh` installs).
  uninstall   Remove the installed agent-env binary.

Options:
  --version vX.Y.Z   Install a specific release tag instead of the latest.
  --bin-dir DIR      Directory to install into (default: $HOME/.local/bin).
  -h, --help         Show this help.

Environment:
  BIN_DIR            Alternative way to set the install directory.

Platforms:
  darwin/arm64, darwin/amd64, linux/amd64, linux/arm64.

Examples:
  curl -fsSL https://raw.githubusercontent.com/yanickxia/agent-env/master/install.zsh | zsh
  zsh install.zsh install --version v0.1.0 --bin-dir /tmp/bin
  zsh install.zsh uninstall
EOF
}

fatal() {
  print -u2 -- "$error_prefix $1"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fatal "missing required command: $1"
}

detect_platform() {
  local os_raw arch_raw
  os_raw="$(uname -s)"
  arch_raw="$(uname -m)"

  case "$os_raw" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) fatal "unsupported operating system: $os_raw (supported: darwin, linux)" ;;
  esac

  case "$arch_raw" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64) arch="amd64" ;;
    *) fatal "unsupported architecture: $arch_raw (supported: arm64, amd64)" ;;
  esac
}

parse_args() {
  local command_seen=0
  while (( $# > 0 )); do
    case "$1" in
      -h|--help|help)
        usage
        exit 0
        ;;
      install|uninstall)
        if (( command_seen )); then
          print -u2 -- "$error_prefix command already set to '$command_name'"
          usage >&2
          exit 1
        fi
        command_name="$1"
        command_seen=1
        shift
        ;;
      --version)
        if (( $# < 2 )); then
          print -u2 -- "$error_prefix --version requires a value"
          usage >&2
          exit 1
        fi
        version="$2"
        shift 2
        ;;
      --version=*)
        version="${1#--version=}"
        shift
        ;;
      --bin-dir)
        if (( $# < 2 )); then
          print -u2 -- "$error_prefix --bin-dir requires a value"
          usage >&2
          exit 1
        fi
        bin_dir_opt="$2"
        shift 2
        ;;
      --bin-dir=*)
        bin_dir_opt="${1#--bin-dir=}"
        shift
        ;;
      --)
        shift
        if (( $# > 0 )); then
          print -u2 -- "$error_prefix unexpected argument(s): $*"
          usage >&2
          exit 1
        fi
        break
        ;;
      *)
        print -u2 -- "$error_prefix unknown command or option: $1"
        usage >&2
        exit 1
        ;;
    esac
  done

  if [[ -n "$version" && "$version" != v* ]]; then
    fatal "--version expects a tag like v0.1.0, got '$version'"
  fi
}

resolve_bin_dir() {
  if [[ -n "$bin_dir_opt" ]]; then
    bin_dir="$bin_dir_opt"
  elif [[ -n "${BIN_DIR:-}" ]]; then
    bin_dir="$BIN_DIR"
  else
    bin_dir="$default_bin_dir"
  fi
}

do_install() {
  need_cmd curl
  need_cmd tar
  need_cmd shasum
  detect_platform
  resolve_bin_dir

  local asset="agent-env_${os}_${arch}.tar.gz"
  local base
  if [[ -n "$version" ]]; then
    base="https://github.com/${repo}/releases/download/${version}"
  else
    base="https://github.com/${repo}/releases/latest/download"
  fi
  local url="${base}/${asset}"
  local sum_url="${url}.sha256"

  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/agent-env-install.XXXXXX")" || fatal "cannot create a temporary directory"
  trap 'rm -rf "${tmpdir:-}"' EXIT

  print -r -- "agent-env install: downloading ${url}"
  if ! curl -fsSL "$url" -o "${tmpdir}/${asset}"; then
    fatal "download failed: ${url} (does the release asset exist for ${os}/${arch}?)"
  fi
  if ! curl -fsSL "$sum_url" -o "${tmpdir}/${asset}.sha256"; then
    fatal "checksum download failed: ${sum_url}"
  fi

  # Verify sha256. The checksum file is "<hash>  <filename>", so `shasum -c`
  # from inside tmpdir works. On failure, print expected/actual for a clear
  # diagnostic instead of a bare "FAILED".
  if ! (cd "$tmpdir" && shasum -a 256 -c "${asset}.sha256" >/dev/null 2>&1); then
    local expected actual
    expected="$(awk '{print $1}' "${tmpdir}/${asset}.sha256")"
    actual="$(shasum -a 256 "${tmpdir}/${asset}" | awk '{print $1}')"
    fatal "sha256 verification failed for ${asset}
  expected: ${expected}
  actual:   ${actual}
Refusing to install a corrupted or tampered download."
  fi
  print -r -- "agent-env install: sha256 verified"

  if ! tar -xzf "${tmpdir}/${asset}" -C "$tmpdir"; then
    fatal "failed to extract ${asset}"
  fi
  local staged="${tmpdir}/agent-env_${os}_${arch}/agent-env"
  if [[ ! -f "$staged" ]]; then
    fatal "binary not found in archive: agent-env_${os}_${arch}/agent-env"
  fi

  mkdir -p "$bin_dir" || fatal "cannot create install directory: ${bin_dir}"
  local target="${bin_dir}/agent-env"

  # Replace whatever is there (a regular file, or the old chezmoi symlink).
  if [[ -e "$target" || -L "$target" ]]; then
    print -r -- "agent-env install: replacing existing ${target}"
    rm -f "$target" || fatal "cannot remove existing ${target}"
  fi

  # Atomic placement: copy into a sibling temp file, chmod, then mv.
  local tmp_target="${bin_dir}/.agent-env.tmp.$$"
  cp "$staged" "$tmp_target" || fatal "cannot copy binary into ${bin_dir}"
  chmod 755 "$tmp_target" || fatal "cannot chmod ${tmp_target}"
  mv -f "$tmp_target" "$target" || fatal "cannot install to ${target}"

  print -r -- "agent-env install: installed ${target}"
  "$target" version
}

do_uninstall() {
  resolve_bin_dir
  local target="${bin_dir}/agent-env"

  if [[ -L "$target" || -f "$target" ]]; then
    rm -f "$target" || fatal "cannot remove ${target}"
    print -r -- "agent-env install: removed ${target}"
  elif [[ -d "$target" ]]; then
    fatal "refusing to remove a directory: ${target}"
  elif [[ -e "$target" ]]; then
    fatal "refusing to remove a non-regular file: ${target}"
  else
    print -r -- "agent-env install: not installed at ${target} (nothing to do)"
  fi
}

# --- main ---------------------------------------------------------------------
command_name="install"
version=""
bin_dir_opt=""
bin_dir=""
tmpdir=""

parse_args "$@"

case "$command_name" in
  install) do_install ;;
  uninstall) do_uninstall ;;
  *) fatal "unknown command: $command_name" ;;
esac