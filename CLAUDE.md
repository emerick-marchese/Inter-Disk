# Inter-Disk (CableDrop)

Application Go de transfert de fichiers entre deux PC, par câble Ethernet ou par Wi-Fi.
Voir README.md pour l'utilisation.

## Règles Git du propriétaire

- Toutes les modifications sont commitées et **poussées directement sur `main`**
  (`git push origin main`), sans branche intermédiaire ni pull request, sauf demande
  contraire explicite.
- `CableDrop-Setup.exe` (à la racine) est l'installateur Windows téléchargé par les
  utilisateurs depuis GitHub. Après toute modification du code, augmentez `appVersion`
  dans `main.go`, lancez `./build.sh` (il régénère `CableDrop-Setup.exe`), puis commitez
  l'installateur avec le code.

## Développement

- Tests : `go test ./... && go vet ./...`
- Compilation de toutes les plateformes et de l'installateur : `./build.sh`
  (nécessite NSIS : `apt install nsis`). Les fichiers de `dist/` sont ignorés par Git.
- L'interface web est dans `ui/index.html` (embarquée dans le binaire via `go:embed`).
- Icône et ressources Windows : `assets/` et `rsrc_windows_*.syso`. Pour les régénérer
  (par exemple à un changement de version), utilisez `go-winres simply --arch amd64,arm64
  --icon assets/cabledrop.png --manifest cli --product-name CableDrop
  --product-version X.Y.Z --file-version X.Y.Z`.
- Installateur : `installer/CableDrop.nsi` (NSIS, français). Il installe dans
  Program Files, ajoute la règle du pare-feu et crée les raccourcis.
- Textes de l'interface et de la console en français.
- Tests manuels sur une seule machine : `-config-dir`, `-port`, `-ui-port`,
  `-discovery-port`, `-announce-ports`. La variable `CABLEDROP_IFACE_KINDS=eth0=wifi,...`
  force le type d'une carte.
