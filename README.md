# Inter Disk

Application de bureau pour envoyer des fichiers d'un PC à un autre, **par câble
Ethernet (RJ45) ou par Wi-Fi**, sans compte, sans mot de passe et sans partage réseau
Windows. L'autre PC reçoit une demande : il clique **Accepter**, et c'est tout.

## ⬇️ Télécharger

**[Télécharger InterDisk-Setup.exe](https://github.com/emerick-marchese/Inter-Disk/releases/latest/download/InterDisk-Setup.exe)**
(Windows 10 / 11, 64 bits)

L'installateur s'occupe de tout : il installe Inter Disk, crée les raccourcis (menu
Démarrer et Bureau) et **règle le pare-feu Windows**. Si l'ancienne version
(CableDrop) est installée, il la remplace. Installez-le **sur les deux PC**.

> Au premier lancement de `setup.exe`, Windows peut afficher « Windows a protégé votre
> ordinateur » parce que le programme n'est pas signé. Cliquez sur *Informations
> complémentaires*, puis sur *Exécuter quand même*.

Pour désinstaller : *Paramètres → Applications → Inter Disk → Désinstaller*.

---

## Utilisation

1. Reliez les deux PC : **câble RJ45** branché directement entre eux, ou bien les deux
   PC connectés **au même Wi-Fi**.
2. Lancez **Inter Disk** sur les deux PC (menu Démarrer ou Bureau).
3. En haut de la fenêtre, choisissez **Transférer par : Câble** ou **Wi-Fi**.
4. L'autre PC apparaît tout seul. Glissez vos fichiers ou dossiers dans la fenêtre,
   ou utilisez **Choisir des fichiers** / **Choisir un dossier**, puis cliquez sur
   **Envoyer**.
5. Sur l'autre PC, la fenêtre passe au premier plan : **Accepter** ou **Refuser**.
   Sans réponse au bout de 2 minutes, la demande est refusée automatiquement.

Les fichiers reçus arrivent dans **Téléchargements\Inter Disk**. Le dossier et le nom
du PC se changent dans **Réglages**. Un fichier existant n'est jamais écrasé :
`photo.jpg` devient `photo (1).jpg`.

Les fichiers sont lus directement sur le disque, par petits morceaux : vous pouvez
envoyer des fichiers de plusieurs dizaines de Go (machines virtuelles, vidéos…)
sans ralentir le PC.

**Transfert coupé ?** (câble débranché, PC en veille, plantage…) Renvoyez simplement
les mêmes fichiers depuis le même PC : Inter Disk **reprend là où il s'était arrêté**,
au lieu de tout recommencer. Le PC qui reçoit affiche « Reprise · X déjà reçus ». Les
fichiers partiels sont gardés 14 jours.

**Vitesse d'envoi maximale** (Réglages) : par défaut, Inter Disk envoie aussi vite que
le câble le permet. Si un PC se fige ou plante pendant les gros envois, baissez-la
(20 Mo/s par exemple) sur ce PC.

---

## Câble ou Wi-Fi ?

| | Câble (recommandé) | Wi-Fi |
|---|---|---|
| Vitesse | environ **110 Mo/s** : 10 Go en ~1 min 30 | souvent **5 à 40 Mo/s** : 10 Go en 5 à 30 min |
| Ce qu'il faut | un câble RJ45 entre les deux PC | les deux PC sur le même réseau (même box) |
| Chemin des données | directement d'un PC à l'autre | par la box ou le point d'accès |

Le mode choisi est mémorisé et toujours respecté : en mode **Câble**, les données ne
passent jamais par le Wi-Fi, même si les deux PC y sont connectés. En mode **Wi-Fi**,
elles ne passent jamais par le câble direct. Si l'autre PC n'est pas joignable dans le
mode choisi, Inter Disk le signale et propose de changer de mode en un clic.

Quand deux PC sont reliés par un câble sans box au milieu, Windows donne
automatiquement à la carte Ethernet une adresse de la forme **169.254.x.x** (cela peut
prendre jusqu'à 1 minute après le branchement). Inter Disk repère l'autre PC sur chaque
carte réseau et utilise la carte qui correspond au mode choisi.

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

Téléchargez `InterDisk-Windows-x64-portable.zip` sur la page
[Releases](https://github.com/emerick-marchese/Inter-Disk/releases/latest),
décompressez-le, puis lancez `InterDisk.exe`. Au premier lancement, le pare-feu Windows
demande une autorisation : cochez **« Réseaux privés » ET « Réseaux publics »** (un câble
direct est vu comme « Réseau non identifié », c'est-à-dire public). Si vous avez cliqué
trop vite, double-cliquez sur `autoriser-pare-feu.bat`.

## Linux

Téléchargez `InterDisk-Linux-x64.tar.gz` sur la page
[Releases](https://github.com/emerick-marchese/Inter-Disk/releases/latest). Il faut la
bibliothèque WebKitGTK 4.1 (`sudo apt install libwebkit2gtk-4.1-0` sous Ubuntu/Debian).
Sur un câble direct, NetworkManager ne donne pas d'adresse automatique : réglez **IPv4**
de la connexion filaire sur **« Lien local uniquement »**.

---

## Dépannage

| Problème | Solution |
|---|---|
| L'autre PC n'apparaît pas | Inter Disk doit être lancé sur les deux PC. Avec la version portable, vérifiez le pare-feu (`autoriser-pare-feu.bat`). |
| « Aucun câble Ethernet actif détecté » | Le câble est débranché ou défectueux, ou Windows attribue encore l'adresse (attendez 1 minute). |
| En Wi-Fi, l'autre PC n'apparaît pas | Les deux PC doivent être sur le même réseau. Les réseaux « invités » ou publics bloquent souvent les échanges entre appareils. |
| Toujours rien | Dans le champ « Ajouter par adresse IP », saisissez l'adresse affichée en haut de la fenêtre Inter Disk de l'autre PC. |
| « Transfert interrompu par l'autre PC » | L'autre PC a annulé, s'est mis en veille, ou la connexion a été coupée. Renvoyez les mêmes fichiers : le transfert reprend là où il s'est arrêté. |
| Un PC se fige ou fait un **écran bleu** pendant un gros envoi | Un écran bleu vient toujours de Windows : un pilote (carte réseau, adaptateur USB-Ethernet, antivirus, disque) ou le matériel. Sur ce PC : 1) mettez à jour le pilote de la carte réseau depuis le site du fabricant (Realtek, Intel…) ; 2) dans Inter Disk, *Réglages → Vitesse d'envoi maximale* : 20 Mo/s ; 3) si le fichier est sur un disque USB, copiez-le d'abord sur le disque interne ; 4) relancez l'envoi, il reprendra où il s'était arrêté. Notez le code affiché sur l'écran bleu (ex. `DRIVER_IRQL_NOT_LESS_OR_EQUAL` + nom de fichier `.sys`) : il indique le pilote fautif. |
| « Aucune donnée depuis… » | L'autre PC ne répond plus (figé, en veille, câble débranché). Le transfert s'arrête seul au bout d'une minute ; renvoyez ensuite pour reprendre. |

En cas de problème, le journal de l'application se trouve dans
`%AppData%\Inter Disk\inter-disk.log`.

---

## Sécurité

- Rien n'est reçu sans votre clic sur **Accepter**.
- Les noms de fichiers reçus sont nettoyés : impossible d'écrire en dehors du
  dossier de réception.
- Les données ne sont **pas chiffrées**. Sur un câble direct, seuls les deux PC sont
  sur la liaison. En Wi-Fi ou sur un réseau partagé, n'acceptez que les demandes
  que vous attendez.

## Détails techniques

| Port | Usage |
|---|---|
| UDP 47820 | Détection automatique des PC (broadcast sur chaque carte réseau) |
| TCP 47821 | Transfert des fichiers |

Application écrite en Go ; la fenêtre utilise [Wails](https://wails.io) (WebView2,
présent sur Windows 10 et 11).

## Compiler soi-même

Installez [Go](https://go.dev/dl/) et [NSIS](https://nsis.sourceforge.io)
(`sudo apt install nsis` sous Linux), puis lancez :

```bash
./build.sh
```

Le script crée dans `dist/` l'installateur Windows, les versions portables Windows et,
sous Linux avec WebKitGTK, la version Linux.
