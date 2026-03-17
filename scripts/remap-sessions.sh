#!/usr/bin/env bash
# remap-sessions.sh — Pull old-path sessions from R2, remap to new paths, merge into ~/.claude
set -euo pipefail

CLAUDE_DIR="${HOME}/.claude"
TEMP_DIR="/tmp/r2-remap"
CLAUDE_SYNC="claude-sync"

# Mapping: old prefix -> new prefix
declare -A MAPPING=(
    ["-home-siyuan-workspace-Human-Replacement"]="-mnt-novita2-siyuan-workspace-Human-Replacement"
    ["-home-siyuan-workspace-Preference-Data-Annotation-Platform"]="-mnt-novita2-siyuan-workspace-Preference-Data-Annotation-Platform"
    ["-home-siyuan-workspace-research-utils"]="-mnt-novita2-siyuan-workspace-research-utils"
    ["-home-siyuan-workspace-hws"]="-mnt-novita2-siyuan-workspace-hws"
    ["-home-siyuan-workspace-data"]="-mnt-novita2-siyuan-workspace-data"
    ["-home-siyuan-workspace-agent-readings"]="-mnt-novita2-siyuan-workspace-agent-readings"
    ["-home-siyuan-workspace"]="-mnt-novita2-siyuan-workspace"
)

echo "=== Step 1: Pull old-path sessions to temp dir ==="
rm -rf "${TEMP_DIR}"
mkdir -p "${TEMP_DIR}"
# Note: pull filter uses PREFIX matching (not glob)
$CLAUDE_SYNC pull --target "${TEMP_DIR}" "projects/-home-siyuan-workspace"

echo ""
echo "=== Step 2: Remap directory names ==="
cd "${TEMP_DIR}/projects"
for old_name in -home-siyuan-workspace-*; do
    [ -d "$old_name" ] || continue

    new_name=""
    # Try longest match first (more specific paths)
    for old_prefix in $(echo "${!MAPPING[@]}" | tr ' ' '\n' | awk '{print length, $0}' | sort -rn | cut -d' ' -f2-); do
        if [[ "$old_name" == "${old_prefix}"* ]]; then
            suffix="${old_name#${old_prefix}}"
            new_name="${MAPPING[$old_prefix]}${suffix}"
            break
        fi
    done

    if [ -z "$new_name" ]; then
        echo "  SKIP: $old_name (no mapping)"
        continue
    fi

    echo "  REMAP: $old_name -> $new_name"
    mv -- "$old_name" "$new_name"
done

echo ""
echo "=== Step 3: Merge into ~/.claude/projects (skip existing) ==="
cd "${TEMP_DIR}/projects"
for dir in -mnt-novita2-*; do
    [ -d "$dir" ] || continue
    target="${CLAUDE_DIR}/projects/${dir}"
    mkdir -p "$target"
    # cp -rn = no-clobber (don't overwrite existing files)
    cp -rn "${dir}/"* "$target/" 2>/dev/null || true
    echo "  MERGED: $dir ($(find "./$dir" -type f | wc -l) files)"
done

echo ""
echo "=== Step 4: Push remapped sessions ==="
# Note: push filter uses PREFIX matching (not glob)
$CLAUDE_SYNC push "projects/-mnt-novita2-siyuan-workspace"

echo ""
echo "=== Step 5: Delete old-path sessions from R2 ==="
echo "Run manually after verifying:"
echo "  $CLAUDE_SYNC delete \"projects/-home-siyuan-workspace-*/**\" --dry-run"
echo "  $CLAUDE_SYNC delete \"projects/-home-siyuan-workspace-*/**\""

echo ""
echo "=== Done ==="
