# CableDrop

Envoyez des fichiers d'un PC à un autre **par un câble Ethernet (RJ45)**, sans compte,
sans mot de passe et sans partage réseau Windows. L'autre PC reçoit une demande :
il clique **Accepter**, et c'est tout.

- Un seul programme, rien à installer (Windows, macOS, Linux).
- Glisser-déposer de fichiers **et de dossiers entiers**, de n'importe quelle taille.
- Les données passent **par le câble**, jamais par le Wi-Fi ni par la box.
- Barre de progression, vitesse, temps restant, annulation des deux côtés.

---

## Utilisation

1. **Branchez le câble RJ45** directement entre les deux PC. Pas besoin de box ni
   de câble « croisé » : les cartes réseau actuelles s'adaptent toutes seules.
2. **Lancez `CableDrop.exe` sur les deux PC.** Une fenêtre noire s'ouvre (laissez-la
   ouverte) et l'interface s'affiche dans le navigateur.
3. L'autre PC apparaît tout seul avec le badge vert **« Câble direct »**.
4. Glissez vos fichiers ou dossiers dans la page, puis cliquez **Envoyer**.
5. Sur l'autre PC, une fenêtre s'affiche : **Accepter** ou **Refuser**.
   Sans réponse au bout de 2 minutes, la demande est refusée automatiquement.

Les fichiers reçus arrivent dans **Téléchargements\CableDrop**. Le dossier et le nom
du PC se changent dans **Réglages**. Un fichier existant n'est jamais écrasé :
`photo.jpg` devient `photo (1).jpg`.

Pour quitter CableDrop, fermez la fenêtre noire.

---

## Pourquoi ça passe bien par le câble

Quand deux PC sont reliés par un câble sans box au milieu, Windows (et macOS) donnent
automatiquement à la carte Ethernet une adresse de la forme **169.254.x.x**. C'est un
petit réseau privé à deux PC, limité au câble.

CableDrop repère l'autre PC **sur chaque carte réseau** (Ethernet, Wi-Fi…). Il choisit
toujours la carte Ethernet et y attache la connexion. Même si les deux PC sont aussi
sur le même Wi-Fi, les données passent par le câble.

L'interface indique le chemin utilisé :

| Badge | Signification |
|---|---|
| 🟢 **Câble direct** | Liaison directe par le câble (169.254.x.x). Le mieux. |
| 🟢 **Câble Ethernet** | Par câble, via un switch ou une box. |
| 🟠 **Wi-Fi seulement** | L'autre PC n'est visible que par le Wi-Fi. Vérifiez le câble. |

**Vitesse attendue** : avec deux cartes Gigabit, comptez environ **110 Mo/s**, soit
1 Go en une dizaine de secondes. Si vous plafonnez à environ 11 Mo/s, une des deux
cartes (ou le câble) est en 100 Mb/s : utilisez un câble Cat 5e ou mieux.

---

## Premier lancement sous Windows

1. **SmartScreen** (« Windows a protégé votre ordinateur ») : cliquez sur
   *Informations complémentaires*, puis *Exécuter quand même*. Ce message s'affiche
   parce que le programme n'est pas signé.
2. **Pare-feu Windows** : une fenêtre demande d'autoriser CableDrop.
   Cochez **« Réseaux privés » ET « Réseaux publics »**, puis *Autoriser l'accès*.
   C'est indispensable, car Windows classe un câble direct comme « Réseau non
   identifié », c'est-à-dire un réseau *public*.
   Si vous avez cliqué trop vite, double-cliquez sur **`autoriser-pare-feu.bat`**
   (fourni avec le programme). Il corrige la règle du pare-feu.
3. Juste après avoir branché le câble, **Windows peut mettre jusqu'à 1 minute** à
   attribuer l'adresse 169.254.x.x. L'interface indique quand le câble est prêt.

## macOS

Le Mac attribue lui aussi une adresse 169.254.x.x automatiquement.
Au premier lancement, faites un clic droit sur le programme, puis **Ouvrir**
(sécurité Gatekeeper). Dans un Terminal, vous pouvez aussi lancer :

```bash
chmod +x CableDrop-macos && xattr -d com.apple.quarantine CableDrop-macos
```

## Linux

NetworkManager ne donne pas d'adresse automatique sur un câble direct. Dans les
paramètres de la connexion filaire, réglez **IPv4** sur **« Lien local uniquement »**,
ou bien exécutez :

```bash
nmcli connection modify "Connexion filaire 1" ipv4.method link-local && nmcli connection up "Connexion filaire 1"
```

---

## Dépannage

| Problème | Solution |
|---|---|
| L'autre PC n'apparaît pas | CableDrop doit être lancé sur les deux PC. Vérifiez les voyants de la prise réseau, puis le pare-feu (lancez `autoriser-pare-feu.bat` sur **les deux** PC Windows). |
| « Aucun câble Ethernet actif détecté » | Le câble est débranché ou défectueux, ou Windows attribue encore l'adresse (attendez 1 minute). |
| L'autre PC apparaît en « Wi-Fi seulement » | Le câble ne fonctionne pas entre les deux PC. Le transfert reste possible, mais il sera plus lent. |
| Toujours rien | Dans le champ « Ajouter par adresse IP », saisissez l'adresse affichée en haut de la page CableDrop de l'autre PC. |
| « Transfert interrompu par l'autre PC » | L'autre PC a annulé, s'est mis en veille, ou le câble a été débranché. Relancez l'envoi. |

Pendant un envoi, laissez la page CableDrop ouverte : c'est elle qui lit les fichiers.

---

## Sécurité

- Rien n'est reçu sans votre clic sur **Accepter**.
- Les noms de fichiers reçus sont nettoyés : impossible d'écrire en dehors du
  dossier de réception.
- L'interface n'est accessible que depuis le PC lui-même (`127.0.0.1`). Les autres
  sites ouverts dans le navigateur ne peuvent pas la piloter.
- Les données ne sont **pas chiffrées**. Sur un câble direct, seuls les deux PC sont
  sur la liaison. Sur un réseau partagé (box, Wi-Fi public…), n'acceptez que les
  demandes que vous attendez.

## Détails techniques

| Port | Usage |
|---|---|
| UDP 47820 | Détection automatique des PC (broadcast sur chaque carte réseau) |
| TCP 47821 | Transfert des fichiers |
| TCP 47822 | Interface web, uniquement sur `127.0.0.1` |

Options : `CableDrop --help` (ports, `-no-browser`, `-config-dir`).

## Compiler soi-même

Installez [Go](https://go.dev/dl/) 1.22 ou plus récent, puis lancez :

```bash
./build.sh
```

Les programmes pour Windows, macOS et Linux sont créés dans `dist/`.
