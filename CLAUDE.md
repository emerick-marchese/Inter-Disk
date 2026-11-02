# Inter Disk

Application de bureau (Go + Wails) de transfert de fichiers entre deux PC, par câble
Ethernet ou par Wi-Fi. Ancien nom : CableDrop (versions 1.x).
Voir README.md pour l'utilisation.

## Règles Git du propriétaire

- Toutes les modifications sont commitées et **poussées directement sur `main`**
  (`git push origin main`), sans branche intermédiaire ni pull request, sauf demande
  contraire explicite.
- Les programmes (dont l'installateur `InterDisk-Setup.exe`) sont publiés dans les
  **Releases GitHub**, jamais commités dans le dépôt. Il n'y a qu'**une seule Release** :
  à chaque nouvelle version, l'ancienne est supprimée et remplacée par la nouvelle. Après toute modification du code :
  augmentez `appVersion` dans `main.go`, lancez `./build.sh`, commitez et poussez sur
  `main`, puis publiez la Release. Le README pointe vers
  `releases/latest/download/InterDisk-Setup.exe` : l'installateur doit garder ce nom.
- Les sessions Claude Code n'ont **pas le droit de créer ou modifier une Release**
  (HTTP 403). C'est le propriétaire qui la publie : soit à la main sur GitHub, en
  envoyant les fichiers de `dist/` que Claude lui transmet, avec le titre et la
  description fournis par Claude.

## Développement

- Tests : `go test ./... && go vet ./...`
- Compilation (Windows + installateur, Linux si WebKitGTK est présent) : `./build.sh`
  (nécessite NSIS : `apt install nsis`). Les fichiers de `dist/` sont ignorés par Git.
- Fenêtre : `desktop.go` (Wails v2). Méthodes appelées par l'interface : `bind.go`
  (`window.go.main.App.*` en JavaScript). L'interface est dans `ui/index.html`.
- Les fichiers envoyés sont lus directement sur le disque par le Go (jamais par
  l'interface) : mémoire constante quelle que soit la taille.
- Reprise (`resume.go`, protocole v2) : le destinataire garde les `.cdpart` et
  `reprises.json` (octets synchronisés sur disque, point tous les 256 Mo) ; un renvoi
  des mêmes fichiers par le même PC reprend. Un PC en protocole v1 reste compatible
  (sans reprise).
- Vitesse d'envoi maximale (`Config.MaxSendMBps`, `pacedWriter`) : contournement
  pour les pilotes réseau qui plantent à pleine charge.
- Test de bout en bout de l'interface sans fenêtre : `go build -tags testhttp` puis
  `-test-http PORT` (pilote l'interface réelle dans un navigateur automatisé).
- Icône et ressources Windows : `assets/` et `rsrc_windows_*.syso`. Pour les régénérer
  (par exemple à un changement de version), utilisez `go-winres simply --arch amd64,arm64
  --icon assets/interdisk.png --manifest gui --product-name "Inter Disk"
  --product-version X.Y.Z --file-version X.Y.Z`.
- Installateur : `installer/InterDisk.nsi` (NSIS, français). Il installe dans
  Program Files, ajoute la règle du pare-feu et crée les raccourcis.
- Textes de l'interface et de la console en français.
- Tests manuels sur une seule machine : `-config-dir`, `-port`, `-ui-port`,
  `-discovery-port`, `-announce-ports`. La variable `CABLEDROP_IFACE_KINDS=eth0=wifi,...`
  force le type d'une carte.
