# Audit préalable de l’orchestration FUOTA

Historique des constats et incréments. Le [contrat actuel](FUOTA_ORCHESTRATION_PLAN_FR.md)
remplace les anciennes consignes supposant un profil commun. Les étapes
ci-dessous ne prouvent pas le rendez-vous à profils différents.

Lecture de code du 14 novembre 2026. Aucun essai radio réalisé par cet audit.
Périmètre : [plan d’orchestration](FUOTA_ORCHESTRATION_PLAN_FR.md).

## Rendez-vous et fermeture de session

Sources du dépôt voisin `LoRa_APRS_iGate-ota`, HEAD observé
`2241dfc6eeb6e19145989dde114ca37f41417d48` :

- `include/ota_aprs_control.h`, `AprsOtaCommand::parse` : commandes OTA
  de statut, profil, FAST/NORMAL et rôles de route ; bail FAST de 15 à 60 s.
- `src/rf_ota.cpp`, `handleAprs` : PROFILE prépare seulement la RAM ; FAST
  répond sur le profil courant avant la bascule programmée deux secondes après.
- `tools/lora_ota/include/profile_lease.h` : bail volatil renouvelé par
  activité ; un redémarrage revient au profil configuré.
- `src/lora_utils.cpp`, `setOtaFastProfile(false)` : retour au profil de
  service de `Config`, pas au dernier profil temporaire sélectionné.
- `src/rf_ota.cpp` et `src/rf_route.cpp` : deux instances de ProfileLease,
  mais une seule radio et une seule configuration de profil temporaire.

Aucune commande OTA examinée ne modifie durablement le profil de service.
Cela ne prouve pas qu’un rendez-vous temporaire soit impossible. Le premier
périmètre exige simplement un même profil de service établi sur le trajet ;
un profil inconnu ou différent interdit le lancement. Ce contrôle reste à
implémenter : le présent audit ne fournit pas une lecture radio authentifiée
du profil de service de chaque carte.

`ABORT` appelle `pages.discard()` avant `cancel()` : il peut supprimer le
point de reprise de la candidate inactive. Ne pas l’utiliser comme fermeture
non destructive d’un contrôle préalable. L’expiration de session appelle
`cancel()`, qui suspend les pages et conserve le point durable ; elle ne
constitue toutefois pas une preuve de restauration observée à distance.

## Différences du worker embarqué avant cet incrément

Comparaison directe des cinq modules avec `tools/lora_ota/` au HEAD ci-dessus :

| Fichier | Différence | Décision et vérification attendue |
| --- | --- | --- |
| `send_integrated.py` | Graywolf ferme le port lors de toute exception de négociation ; le client de référence le ferme explicitement sur timeout seulement. | Conserver la protection Graywolf ; couvrir timeout et exceptions d’entrée/sortie. |
| `send.py` | Graywolf demande l’exclusivité série sur POSIX. | Conserver ; vérifier le refus de partage et le comportement sur plateformes non POSIX. |
| `send_integrated.py` | Le client de référence accepte `--board-id` (TTGO, Heltec V3, QRP Labs), contrairement à l’embarqué. | Ne pas importer implicitement un support de cartes : le superviseur Graywolf valide actuellement une application ESP32 TTGO. Tester une sélection explicite avant tout élargissement. |
| `window_transfer.py` | Le client de référence construit le manifeste avec `client.board_id`, TTGO par défaut ; Graywolf utilise TTGO en dur. | Pour le périmètre TTGO actuel, même identifiant `0x54474732`. Vérifier le manifeste et le refus des images incompatibles. |

`radio_profiles.py` et `route_transport.py` sont identiques lors de cette
comparaison. Les fichiers ne sont pas écrasés. Cet inventaire ne signifie pas
que les tests ont été relancés ni que le client embarqué a été validé sur radio.
Le cycle physique du 14 novembre et ses scripts archivés restent la référence
historique ; le worktree courant n’est pas assimilé à cette archive.

## Informations avant lancement

Le récapitulatif devra indiquer l’indisponibilité APRS du relais pendant le
transfert et une estimation de durée avec ses hypothèses. Les 1 h 11 pour
1,78 Mo sont une référence de laboratoire comprenant le rollback volontaire,
avec des blocs de 160 octets. Ce n’est pas une durée garantie pour une mise
à jour normale ni une mesure du parcours Graywolf à 128 octets. Aucune
extrapolation proportionnelle aux seuls octets ne doit être présentée comme
une mesure : débit radio, réparations, délais et scénario influent sur la durée.


Précision chronologique : selon la clarification de l’opérateur, --board-id
a été ajouté après l’essai physique du 14 novembre pour préparer Heltec V3
et QRP Labs. Ces modèles ont été testés logiciellement, pas sur ces cartes.
Le client courant ne doit donc pas être appelé « client validé sur cartes »
pour cette fonction. Cette constatation historique ne restreint pas le périmètre demandé : le choix
explicite des trois modèles est requis, en conservant les protections série.

Le récapitulatif doit distinguer environ 63 minutes mesurées pour transférer
et vérifier 1,78 Mo à 160 octets (3 762,66 s) d’environ 1 h 11 pour le cycle
comprenant le rollback volontaire. La durée à 128 octets n’est pas mesurée sur
image entière ; le relais est indisponible pour le trafic APRS pendant
l’opération. Ces valeurs ne constituent pas une durée garantie de mise à jour.


## Incrément de sélection du modèle

Le worker Graywolf accepte désormais --board-id sans valeur par défaut et
l’utilise dans les deux manifestes. Le serveur exige target_board dans les
réglages ; il distingue ESP32 (TTGO) et ESP32-S3 (Heltec/QRP) dans l’en-tête
image. Ce contrôle ne distingue pas les deux cartes S3 : sélectionner la
variante exacte reste obligatoire et la cible vérifie le manifeste.
Le modèle et son identifiant sont conservés dans job.json ; l’événement image
journalise aussi board_id. Un refus authentifié journalise op, status, offset
et payload_bytes ; FORMAT est expliqué selon l’opération sans diagnostic unique.

Précision de publication fournie par l’opérateur : 2241dfc a remplacé les
binaires et empreintes dans les trois dossiers existants ; il ne les a pas
créés. La publication de ces variantes n’établit pas leur validation physique.


## Remplacement du transport par Go, exigence opérateur

La consigne explicite « en Go, sans Python » remplace le choix précédent du
plan. Le superviseur appelle désormais le moteur Go en processus : aucun
exec.Command, aucune recherche d’interpréteur, aucun fichier .py embarqué ou
extrait. Les anciens fichiers Python restent des références de développement.

Le test de récupération initial ne provoquait pas réellement la perte de
liaison attendue : le délai simulé intervenait avant la création du délai de
réception du moteur. Le scénario corrigé supprime les deux réponses QUERY,
ce qui épuise les tentatives et déclenche le retour au profil normal, le bail,
le nouveau HELLO et la reprise. L’assertion link_recovered reste obligatoire.

Les vecteurs indépendants sont dans pkg/radioota/testdata/reference-vectors.json :
19 requêtes et 8 réponses, clé/nonce/image déterministes et manifestes à 128
octets pour les trois modèles. Ils proviennent des codecs du client iGate,
avec SHA-256 des sources et commit enregistrés. Les tests Go comparent aussi
les trames réellement émises jusqu’à l’activation, en direct et via relais.
Ces preuves logicielles ne valent pas validation physique du moteur Go.


## Fréquences de service différentes : complément d’audit

La demande ultérieure conserve F4MLV-2/F4MLV-10 à 433.775 MHz et GENATX sur
son propre profil. L’ancienne restriction au profil commun ne répond donc
plus à l’objectif. La section correspondante du plan décrit le contrat de
l’extension et les obstacles vérifiés : suspension du digipeating APRS pendant
FAST, baux RF_OTA/RF_ROUTE distincts et profil temporaire unique dans le pilote,
retour Config trop tôt pour le relais lors des confirmations côté cible.
Une simple automatisation des commandes FAST ne constitue pas une solution
validée. Le firmware n’a pas été modifié ; une validation du développement
coordonné, distincte de tout flash, a été demandée à l’opérateur.


## Contrat à valider : profils et retours propres à chaque carte

Le contrat a été réécrit pour supprimer les consignes contradictoires :
F4MLV-2/F4MLV-10 à 433.775 MHz, GENATX distinct. L’opérateur indique
434.100 MHz « je suppose » : cette valeur reste une hypothèse, pas une lecture.
Les durées 60 s de bail, 20 s entre maintiens, 2 s après accord et 2 h absolues
sont des propositions explicites à valider, non des fonctions déjà livrées.
Le retour après transfert garde le relais sur le profil de la cible pour la
confirmation ; seul le retour final le ramène à son service propre. Une preuve
authentifiée d’état et une fermeture sans réarmement restent à spécifier.
Aucune opération sur carte ni modification de firmware n’a été effectuée.


### Vérification documentaire de la fréquence de GENATX

L’entrée du 14 novembre dans l’inventaire local, lignes 1456–1457, donne
l’état final vérifié de MLV-15 après activation/confirmation de B : profil
configuré 434.100 MHz. Cette trace remplace la qualification de simple
supposition dans le contrat. Une nouvelle lecture HTTP à 192.168.1.165 a été
tentée et échoue avec No route to host ; aucune valeur actuelle obtenue par
HTTP n’est affirmée. Aucun port série, reset, émission ou réglage radio n’a
été effectué pour cette vérification.
