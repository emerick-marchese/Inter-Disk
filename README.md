# CableDrop

Envoyez des fichiers d'un PC à un autre **par câble Ethernet (RJ45) ou par Wi-Fi**,
sans compte, sans mot de passe et sans partage réseau Windows. L'autre PC reçoit
une demande : il clique **Accepter**, et c'est tout.

## ⬇️ Télécharger

**[Télécharger CableDrop-Setup.exe](https://github.com/emerick-marchese/Inter-Disk/raw/main/CableDrop-Setup.exe)**
(Windows 10 / 11, 64 bits)

L'installateur s'occupe de tout : il installe CableDrop, crée les raccourcis (menu
Démarrer et Bureau) et **règle le pare-feu Windows**. Installez-le **sur les deux PC**.

> Au premier lancement de `setup.exe`, Windows peut afficher « Windows a protégé votre
> ordinateur » parce que le programme n'est pas signé. Cliquez sur *Informations
> complémentaires*, puis sur *Exécuter quand même*.

Pour désinstaller : *Paramètres → Applications → CableDrop → Désinstaller*.

---

## Utilisation

1. Reliez les deux PC : **câble RJ45** branché directement entre eux, ou bien les deux
   PC connectés **au même Wi-Fi**.
2. Lancez **CableDrop** sur les deux PC. Une fenêtre noire s'ouvre (laissez-la
   ouverte) et l'interface s'affiche dans le navigateur.
3. En haut de la page, choisissez **Transférer par : Câble** ou **Wi-Fi**.
4. L'autre PC apparaît tout seul. Glissez vos fichiers ou dossiers dans la page,
   puis cliquez sur **Envoyer**.
5. Sur l'autre PC, une fenêtre s'affiche : **Accepter** ou **Refuser**.
   Sans réponse au bout de 2 minutes, la demande est refusée automatiquement.

Les fichiers reçus arrivent dans **Téléchargements\CableDrop**. Le dossier et le nom
du PC se changent dans **Réglages**. Un fichier existant n'est jamais écrasé :
`photo.jpg` devient `photo (1).jpg`.

Pour quitter CableDrop, fermez la fenêtre noire.

---

## Câble ou Wi-Fi ?

| | Câble (recommandé) | Wi-Fi |
|---|---|---|
| Vitesse | environ **110 Mo/s** : 1 Go en ~10 s | souvent **5 à 40 Mo/s** : 1 Go en 30 s à 3 min |
| Ce qu'il faut | un câble RJ45 entre les deux PC | les deux PC sur le même réseau (même box) |
| Chemin des données | directement d'un PC à l'autre | par la box ou le point d'accès |

Le mode choisi est mémorisé. CableDrop respecte toujours ce choix : en mode **Câble**,
les données ne passent jamais par le Wi-Fi, même si les deux PC y sont connectés. En
mode **Wi-Fi**, elles ne passent jamais par le câble direct.

Si l'autre PC n'est pas joignable dans le mode choisi, CableDrop le signale et propose
de changer de mode en un clic.

### Comment le câble est utilisé

Quand deux PC sont reliés par un câble sans box au milieu, Windows (et macOS) donnent
automatiquement à la carte Ethernet une adresse de la forme **169.254.x.x**. C'est un
petit réseau privé à deux PC. CableDrop repère l'autre PC sur chaque carte réseau
(Ethernet, Wi-Fi…) et attache la connexion à la carte correspondant au mode choisi.

| Badge | Signification |
|---|---|
| 🟢 **Câble direct** | Liaison directe par le câble (169.254.x.x). Le plus rapide. |
| 🟢 **Câble Ethernet** | Par câble, via un switch ou une box. |
| 🔵 **Wi-Fi** | Par le Wi-Fi (mode Wi-Fi). |
| 🟠 **Wi-Fi seulement** | En mode Câble, l'autre PC n'est visible qu'en Wi-Fi : vérifiez le câble. |

Avec deux cartes Gigabit, comptez environ 110 Mo/s. Si vous plafonnez à environ
11 Mo/s, une des deux cartes (ou le câble) est en 100 Mb/s : utilisez un câble
Cat 5e ou mieux.

---

## Version portable (sans installation)

Si vous ne voulez pas installer CableDrop, compilez la version portable (voir
« Compiler soi-même » plus bas), puis lancez directement `CableDrop.exe`. Au
premier lancement, le pare-feu Windows demande une autorisation : cochez
**« Réseaux privés » ET « Réseaux publics »** (un câble direct est vu comme
« Réseau non identifié », c'est-à-dire public). Si vous avez cliqué trop vite,
double-cliquez sur `autoriser-pare-feu.bat`.

## macOS

Le Mac attribue lui aussi une adresse 169.254.x.x automatiquement sur un câble direct.
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
| L'autre PC n'apparaît pas | CableDrop doit être lancé sur les deux PC. Avec la version portable, vérifiez le pare-feu (`autoriser-pare-feu.bat`). |
| « Aucun câble Ethernet actif détecté » | Le câble est débranché ou défectueux, ou Windows attribue encore l'adresse : après le branchement, cela peut prendre jusqu'à 1 minute. |
| En Wi-Fi, l'autre PC n'apparaît pas | Les deux PC doivent être sur le même réseau. Les réseaux « invités » ou publics (hôtel, gare…) bloquent souvent les échanges entre appareils. |
| Toujours rien | Dans le champ « Ajouter par adresse IP », saisissez l'adresse affichée en haut de la page CableDrop de l'autre PC. |
| « Transfert interrompu par l'autre PC » | L'autre PC a annulé, s'est mis en veille, ou la connexion a été coupée. Relancez l'envoi. |

Pendant un envoi, laissez la page CableDrop ouverte : c'est elle qui lit les fichiers.

---

## Sécurité

- Rien n'est reçu sans votre clic sur **Accepter**.
- Les noms de fichiers reçus sont nettoyés : impossible d'écrire en dehors du
  dossier de réception.
- L'interface n'est accessible que depuis le PC lui-même (`127.0.0.1`). Les autres
  sites ouverts dans le navigateur ne peuvent pas la piloter.
- Les données ne sont **pas chiffrées**. Sur un câble direct, seuls les deux PC sont
  sur la liaison. En Wi-Fi ou sur un réseau partagé, n'acceptez que les demandes
  que vous attendez.

## Détails techniques

| Port | Usage |
|---|---|
| UDP 47820 | Détection automatique des PC (broadcast sur chaque carte réseau) |
| TCP 47821 | Transfert des fichiers |
| TCP 47822 | Interface web, uniquement sur `127.0.0.1` |

Options : `CableDrop --help` (ports, `-no-browser`, `-config-dir`).

## Compiler soi-même

Installez [Go](https://go.dev/dl/) 1.22 ou plus récent, et [NSIS](https://nsis.sourceforge.io)
pour l'installateur (`sudo apt install nsis` sous Linux). Ensuite, lancez :

```bash
./build.sh
```

Le script crée dans `dist/` les programmes pour Windows (installateur et versions
portables), macOS et Linux. Il met aussi à jour `CableDrop-Setup.exe` à la racine du
dépôt.
