# Inter-Disk (CableDrop)

Application Go de transfert de fichiers entre deux PC reliés par un câble Ethernet.
Voir README.md pour l'utilisation.

## Règle Git du propriétaire

- Toutes les modifications sont commitées et **poussées directement sur `main`**
  (`git push origin main`), sans branche intermédiaire ni pull request, sauf demande
  contraire explicite.

## Développement

- Tests : `go test ./... && go vet ./...`
- Compilation de toutes les plateformes : `./build.sh` (résultat dans `dist/`, ignoré par Git).
- L'interface web est dans `ui/index.html` (embarquée dans le binaire via `go:embed`).
- Textes de l'interface et de la console en français.
