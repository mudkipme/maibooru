#!/usr/bin/env bash
# Packages the Linux app that `mygo build` made into an AppImage.
#
#   scripts/appimage.sh <build/linux-amd64|build/linux-arm64> <out.AppImage>
#
# Needs appimagetool on PATH (or $APPIMAGETOOL) and the AppImage runtime of
# the target's architecture in $APPIMAGE_RUNTIME, so that one machine
# packages both architectures.
set -euo pipefail

src=${1:?the directory mygo build made, as build/linux-amd64}
out=${2:?the AppImage to write}
tool=${APPIMAGETOOL:-appimagetool}
runtime=${APPIMAGE_RUNTIME:?the AppImage runtime of the target architecture}

case "$(basename "$src")" in
  *-amd64) arch=x86_64 ;;
  *-arm64) arch=aarch64 ;;
  *) echo "unknown architecture of $src" >&2; exit 1 ;;
esac

desktop=$(find "$src" -maxdepth 1 -name '*.desktop' | head -n 1)
[ -n "$desktop" ] || { echo "no desktop entry in $src" >&2; exit 1; }
name=$(basename "$desktop" .desktop)

appdir=$(mktemp -d)/$name.AppDir
trap 'rm -rf "$(dirname "$appdir")"' EXIT
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/applications" \
  "$appdir/usr/share/icons/hicolor/256x256/apps" "$appdir/usr/share/icons/hicolor/512x512/apps"

install -m 755 "$src/$name" "$appdir/usr/bin/$name"
install -m 644 "$desktop" "$appdir/usr/share/applications/$name.desktop"
install -m 644 "$desktop" "$appdir/$name.desktop"
install -m 644 "$src/$name-256x256.png" "$appdir/usr/share/icons/hicolor/256x256/apps/$name.png"
install -m 644 "$src/$name-512x512.png" "$appdir/usr/share/icons/hicolor/512x512/apps/$name.png"
install -m 644 "$src/$name.png" "$appdir/$name.png"
ln -s "$name.png" "$appdir/.DirIcon"
cat > "$appdir/AppRun" <<EOF
#!/bin/sh
here=\$(dirname "\$(readlink -f "\$0")")
exec "\$here/usr/bin/$name" "\$@"
EOF
chmod 755 "$appdir/AppRun"

# appimagetool is an AppImage too: unpacked, it needs no FUSE.
ARCH=$arch APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream --runtime-file "$runtime" "$appdir" "$out"
