#!/usr/bin/env bash
# Publie le contenu de dist/ dans la Release GitHub vX.Y.Z (version lue dans main.go).
# La Release est créée si elle n'existe pas ; les fichiers déjà présents sont remplacés.
# Usage : ./build.sh && ./release.sh [fichier-de-notes.md]
# Utilise uniquement l'API REST de GitHub via « gh api ».
set -euo pipefail
cd "$(dirname "$0")"
REPO=emerick-marchese/Inter-Disk
VERSION=$(sed -n 's/.*appVersion = "\(.*\)".*/\1/p' main.go)
TAG="v$VERSION"

[ -f dist/CableDrop-Setup.exe ] || { echo "dist/ est vide : lancez d'abord ./build.sh" >&2; exit 1; }
COMMIT=$(git rev-parse HEAD)
git fetch -q origin main
[ "$(git rev-parse FETCH_HEAD)" = "$COMMIT" ] || { echo "Poussez d'abord ce commit sur main (git push origin main)" >&2; exit 1; }

NOTES=${1:-}
if [ -z "$NOTES" ]; then
  NOTES=$(mktemp)
  cat > "$NOTES" <<NOTES_EOF
## Installation (Windows)

Téléchargez **CableDrop-Setup.exe** ci-dessous et installez-le **sur les deux PC**.

Toutes les explications sont dans le [README](https://github.com/$REPO#readme).
NOTES_EOF
fi

id=$(gh api "repos/$REPO/releases/tags/$TAG" --jq .id 2>/dev/null) || id=""
if [ -z "$id" ]; then
  echo "Création de la Release $TAG…"
  id=$(gh api -X POST "repos/$REPO/releases" -f tag_name="$TAG" -f target_commitish="$COMMIT" \
        -f name="CableDrop $VERSION" -F body=@"$NOTES" -F make_latest=true --jq .id)
else
  echo "Mise à jour de la Release $TAG…"
fi

for f in dist/*; do
  name=$(basename "$f")
  old=$(gh api "repos/$REPO/releases/$id/assets" --jq ".[] | select(.name == \"$name\") | .id")
  [ -n "$old" ] && gh api -X DELETE "repos/$REPO/releases/assets/$old"
  gh api -X POST "https://uploads.github.com/repos/$REPO/releases/$id/assets?name=$name" \
     -H "Content-Type: application/octet-stream" --input "$f" --jq '"  ✓ " + .name'
done
echo "https://github.com/$REPO/releases/tag/$TAG"
