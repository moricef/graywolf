#!/bin/sh
# Build a portable Windows x86 package from Linux, without publishing it.
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_dir"
command -v i686-w64-mingw32-gcc >/dev/null || {
    echo 'Install gcc-mingw-w64-i686-win32 and rustup target add i686-pc-windows-gnu.' >&2
    exit 1
}
command -v i686-w64-mingw32-objdump >/dev/null
build_dir=${1:-"$repo_dir/scratch/windows-386"}
mkdir -p "$build_dir"
build_dir=$(CDPATH= cd -- "$build_dir" && pwd)
base_version=$(tr -d '[:space:]' < VERSION)
commit_id=$(git rev-parse HEAD | cut -c1-8)
if ! git diff --quiet HEAD --; then commit_id="$commit_id-dirty"; fi
# Build the SPA before Go embeds it. Rust and Go share the same version stamp.
make web
GOOS=windows GOARCH=386 CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.Version=$base_version -X main.GitCommit=$commit_id" \
    -o "$build_dir/graywolf.exe" ./cmd/graywolf
GRAYWOLF_VERSION="$base_version" GRAYWOLF_GIT_COMMIT="$commit_id" \
CARGO_TARGET_I686_PC_WINDOWS_GNU_LINKER=i686-w64-mingw32-gcc \
RUSTFLAGS="${RUSTFLAGS:-} -C target-feature=+crt-static" \
    cargo build --locked --release --target i686-pc-windows-gnu --bin graywolf-modem
cp target/i686-pc-windows-gnu/release/graywolf-modem.exe "$build_dir/graywolf-modem.exe"
for executable in graywolf.exe graywolf-modem.exe; do
    i686-w64-mingw32-objdump -f "$build_dir/$executable" | grep -q 'file format pei-i386'
done
python3 - "$build_dir" "$base_version" "$commit_id" <<'PY'
from pathlib import Path
import hashlib, sys, zipfile
folder, version, commit = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
readme = f'''Graywolf Windows x86 (32-bit) portable test build
Base version: {version}
Commit: {commit}

Extract the entire ZIP into a writable folder, then run graywolf.exe.
Keep graywolf.exe and graywolf-modem.exe together: both are 32-bit.
The normal web address is http://127.0.0.1:8080.
To use a different port, run: graywolf.exe -http 127.0.0.1:18080

Target: Windows 10 x86 or later. Actual Windows 10 x86 validation is required.
The archive is portable; the existing x64 NSIS installer cannot install it.
This script builds locally and does not publish a GitHub release or OCI image.
'''
(folder/'README-Windows-x86.txt').write_text(readme, encoding='utf-8')
archive = folder/f'graywolf_{version}_windows_x86.zip'
with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:
    for name in ['graywolf.exe','graywolf-modem.exe','README-Windows-x86.txt']:
        z.write(folder/name,name)
    z.write('LICENSE','LICENSE')
checksum = hashlib.sha256(archive.read_bytes()).hexdigest()
(folder/'checksums-windows-x86.txt').write_text(f'{checksum}  {archive.name}\n')
print(archive)
PY
