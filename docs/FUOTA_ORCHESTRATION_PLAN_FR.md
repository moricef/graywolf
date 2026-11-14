# Contrat d’orchestration FUOTA — proposition à valider

Date : 14 novembre 2026. Ce document remplace les consignes précédentes qui
supposaient un profil de service commun. La séquence ci-dessous est un contrat
à démontrer par tests logiciels avant approbation d’un essai sur cartes.
**Une premiere implementation locale est decrite en section 10 ; elle n'est
pas validee sur materiel.** Aucun changement de
carte, de clé, de fréquence permanente ou de firmware déployé n’est autorisé
par ce document.

## 1. Situation retenue et valeurs à confirmer

| Équipement | Rôle de cette opération | Profil de service initial | Statut de la valeur |
| --- | --- | --- | --- |
| F4MLV-2 | Contrôleur USB de Graywolf | 433.775 MHz | Confirmé par l’opérateur |
| F4MLV-10 | Relais radio | 433.775 MHz | Confirmé par l’opérateur |
| GENATX, identité signée F4MLV-15 | Cible | 434.100 MHz | Dernier état vérifié le 14 novembre, inventaire local lignes 1456–1457 ; pas une nouvelle lecture en direct |

Un profil comprend fréquence, BW, SF, CR et puissance. Les BW/SF/CR/puissances
actuels des trois cartes restent à confirmer : une valeur enregistrée dans
Graywolf ou un ancien rapport n’est pas une lecture actuelle de la carte.
Le transfert temporaire proposé reprend 434.500 MHz / BW500 kHz / SF7 / CR4/5 /
9 dBm ; c’est une valeur proposée, pas un changement de profil de service.

L’inventaire local `LoRa_APRS_iGate/docs/DEPLOYMENTS_fr.md`, entrée du
14 novembre, établit l’état final de MLV-15 à 434.100 MHz après confirmation du
firmware B. Il ne s’agit donc pas seulement d’une supposition de l’opérateur.
Une tentative actuelle de GET `/configuration.json` à son adresse de gestion
192.168.1.165 échoue avec « No route to host » ; aucune nouvelle lecture directe
n’est revendiquée, et son port série n’a pas été ouvert.

Les deux profils de service distincts doivent être enregistrés séparément.
Le champ Normal de l’interface installée ne change pas les cartes distantes et
ne peut pas représenter à lui seul cette situation. Aucune migration ne doit
deviner ou remplacer une valeur manquante. Ne plus demander de mettre GENATX
sur 433.775 MHz ni les autres cartes sur 434.100 MHz pour contourner cette limite.

## 2. Invariants du travail demandé

- Graywolf réalise le transfert en Go, sans Python, PySerial ni processus externe.
- Un seul firmware unifié, adapté au modèle matériel, implémente les comportements
  selon le rôle contrôleur/relais/cible. Aucun firmware spécial F4MLV-10.
- Aucun accès Wi-Fi/USB au relais ou à la cible n’est requis lors d’une mise à
  jour courante. Seul le contrôleur local est en USB. Les accès de provisionnement
  initial ne deviennent pas une étape manuelle à répéter à chaque mise à jour.
- Les clés existantes et les compteurs persistants sont conservés ; aucune
  rotation, remise à zéro ou distribution implicite de clé.
- Le destinataire RF GENATX et l’identité signée F4MLV-15 restent distincts.
- Image applicative entière, non compressée, blocs utiles de 128 octets.
  Choix explicite du modèle TTGO, Heltec V3 ou QRP Labs ; bon board-id vérifié
  dans le manifeste. Leur niveau de validation reste affiché.
- Les profils de service ne sont pas écrits ou remplacés pour le rendez-vous.
  Toute sortie doit viser les profils propres à chaque carte, pas une fréquence
  normale globale codée en dur.
- Une réponse APRS ou un ACK ne constitue pas une preuve HMAC. Les futurs états
  de coordination doivent être authentifiés et corrélés à l’opération courante.
- ABORT ne ferme jamais un contrôle préalable : il peut supprimer la candidate.
- Un échec, un délai ou une disparition radio ne sont pas une preuve de retour
  au service. Chaque résultat distingue installation et restauration.

## 3. Pourquoi les seules commandes actuelles ne suffisent pas à démontrer la séquence

Code iGate OTA lu au commit 2241dfc ; détails et historique dans
[FUOTA_ORCHESTRATION_AUDIT_FR.md](FUOTA_ORCHESTRATION_AUDIT_FR.md).

- OTA=PROFILE prépare de la RAM, sans changer Config ; OTA=FAST utilise le bail
  RF_OTA (15–60 s). Les échanges relayés renouvellent un autre propriétaire,
  RF_ROUTE, pas ce bail RF_OTA.
- La boucle principale suspend le digipeating APRS ordinaire quand RF_OTA ou
  RF_ROUTE est occupé. Un relais simplement déplacé par OTA=FAST ne peut donc
  plus assurer normalement les commandes de préparation de la cible.
- Le pilote refuse de préparer un autre profil temporaire lorsqu’un profil
  temporaire est déjà actif. Superposer rendez-vous et transfert n’est pas géré.
- RETURN_NORMAL revient au Config de chaque carte. Le relais doit pourtant
  rester momentanément sur le profil de la cible pour activation, confirmation
  et relecture, puis seulement revenir à son propre service.

Ces observations ne prouvent pas l’impossibilité de tout rendez-vous temporaire.
Elles invalident la présentation d’une simple commande FAST comme une solution
rapide, robuste et déjà disponible. Aucun ACK de cible ne sera fabriqué pour
forcer une transition. L’extension concerne le firmware commun et Graywolf ;
les capacités nouvelles doivent être négociées explicitement.

## 4. Durées proposées, à valider

Ces bornes sont des exigences de conception proposées ; elles ne décrivent pas
un mécanisme déjà présent dans le firmware livré.

| Élément | Proposition | Condition impérative |
| --- | --- | --- |
| Bascule de profil | 2 s après fin d’émission de l’accord sur le profil courant | Ne pas compter depuis le début de la requête ; les temps d’émission comptent |
| Bail d’inactivité d’un profil temporaire | 60 s | Un seul propriétaire de radio par carte ; reprise uniquement sur activité authentifiée de la bonne session |
| Maintien actif par Graywolf | Au plus 20 s entre échanges de maintien | Mesurer la marge avec les vrais temps radio ; aucune transaction/burst ne doit occuper tout le bail |
| Durée absolue maximale d’une opération | 2 h proposées | Limite non renouvelée par les maintiens ; interruption sûre et reprise ultérieure conservant les pages |
| Rollback volontaire de validation | 190 s sans confirmation de la cible | Le maintien du relais ne confirme pas la cible et ne prolonge pas sa fenêtre de rollback |
| Repli après perte totale du contrôleur | Bail de 60 s + durée de restauration à mesurer | Le délai seul ne permet pas à Graywolf d’annoncer le service restauré |

La durée globale d’une mise à jour à 128 octets reste non mesurée sur image
entière. Ne pas annoncer 63 ou 71 minutes comme durée garantie de ce nouveau
parcours. L’indisponibilité APRS du relais pendant les phases temporaires doit
être annoncée avant lancement, y compris lors du contrôle préalable.

## 5. Séquence radio à démontrer

Notation : S2 = service F4MLV-2 ; S10 = service F4MLV-10 ; ST = service GENATX ;
X = transfert temporaire. Les noms d’opérations ci-dessous sont fonctionnels,
**pas des commandes qui existeraient déjà**. Leur format radio, les nouveaux
codes et leur authentification devront être spécifiés avant implémentation.

| Étape | Contrôleur F4MLV-2 | Relais F4MLV-10 | Cible GENATX | Accord requis avant de poursuivre |
| --- | --- | --- | --- | --- |
| 0. Préparation locale | S2 = 433.775 | S10 = 433.775 | ST = 434.100, dernier état vérifié | Paramètres complets, identités, clés, fichier et version attendue renseignés ; aucune radio encore modifiée |
| 1. Capacités et état initial | S2 | S10 | ST | Identifier le contrôleur USB ; joindre/authentifier le relais sur S10 ; relire ses capacités, son service et son état. Une ancienne version incompatible bloque explicitement |
| 2. Préparer le rendez-vous | S2, puis ST | S10, puis ST | ST inchangé | Le relais accepte un rendez-vous borné sur ST, répond encore sur S10, puis bascule selon l’accord. Le contrôleur le rejoint et obtient une preuve d’état sur ST |
| 3. Préparer la cible | ST | ST, phase contrôle | ST | Le relais laisse passer les seuls échanges de contrôle nécessaires et leurs réponses. Établir/relire les rôles, authentifier la cible et confirmer son vrai profil de service. Un profil effectif non confirmé lors de la préparation interdit le transfert |
| 4. Contrôle préalable seul | ST | ST | ST | Aucun START/DATA/ACTIVATE. Libérer la session cible sans ABORT et obtenir une preuve de son état inactif sur ST. Conserver le rendez-vous du relais uniquement le temps de vérifier cette sortie, puis appliquer l’étape 8 |
| 5. Transfert demandé | ST, puis X | ST, puis X | ST, puis X | Accord authentifié des nœuds concernés avant la bascule. Le même coordinateur du relais possède rendez-vous et transfert ; aucun bail concurrent. Transfert, réparations et SHA-256 de l’image |
| 6. Retour pour activation | ST | ST, pas encore S10 | ST | Acquitter la fin du transfert puis revenir sur ST. Le relais reste disponible pour les échanges de démarrage et de confirmation de la cible |
| 7. Activation et relecture | ST | ST avec maintien borné | Reboot sur ST | Version/SHA/partitions/pending vérifiés, confirmation puis nouvelle session de relecture. En validation, attendre le rollback, le vérifier, réactiver puis confirmer ; le rendez-vous reste maintenu sans neutraliser le rollback |
| 8. Fin contrôlée | ST, puis S2 | ST, puis S10 | ST inactif | Fermer la cible sans destruction, obtenir sa preuve d’état, ordonner le retour du relais avec accord sur ST, rejoindre S10 puis vérifier son vrai état. Restaurer et relire également le contrôleur USB sur S2 |
| 9. Résultat | S2 vérifié | S10 vérifié | ST vérifié | Afficher séparément firmware installé et profils de service restaurés. Tout nœud non vérifié reste explicitement inconnu |

À partir de l’étape 2, la preuve de capacité de la cible peut nécessiter le
rendez-vous : elle ne doit pas être supposée avant qu’on puisse la joindre.
L’éventuelle compatibilité de transition avec un ancien firmware cible doit être
démontrée ; elle n’est pas déduite de celle du relais. La séquence doit aussi
montrer comment terminer un contrôle préalable sans réarmer aussitôt la cible.

## 6. Propriétaire de radio et preuve d’état : points à spécifier

Un seul coordinateur par carte garde les profils initiaux et gère service,
rendez-vous, transfert et retour. Sur le relais, le retour après transfert est
ST ; le retour final ou après panne est S10. Le rendez-vous peut autoriser les
échanges de préparation strictement nécessaires sans reprendre le trafic APRS
ordinaire ni vider une ancienne file sur une fréquence temporaire.

Les nouvelles preuves d’état doivent porter au minimum : identité et rôle,
identifiant de l’opération/challenge, profil effectivement appliqué, étape,
échéance du bail et état de session. Une réponse signée ne doit pas présenter
Config comme une lecture de matériel si l’application radio a échoué. La
fermeture doit pouvoir prouver un état inactif sans créer une nouvelle session
active ; le protocole actuel ne démontre pas encore ce chemin.

Aucun ancien paquet, ACK perdu, maintien d’une autre session ou réponse APRS
non signée ne doit prolonger un bail ou valider une bascule. Le mécanisme de
maintien pendant les 190 secondes de rollback doit rester indépendant de la
confirmation cible. Le reset d’une carte restaure son propre profil de service.

## 7. Pannes et reprise

- Perte d’accord avant une bascule : ne pas déduire de l’absence de réponse que
  le nœud n’a pas basculé. Suivre le repli borné puis relire son état sur son
  service avant de reconstruire le rendez-vous.
- Perte de lien pendant le transfert : conserver les pages durables ; après
  repli, partir de S2/S10/ST distincts et renégocier, sans changer les clés.
- Redémarrage de Graywolf : opération marquée interrompue, jamais supposée
  terminée. Ne pas rejouer aveuglément activation ou suppression de candidate.
- Un nœud absent ne peut pas être déclaré restauré après un simple timer.
- Réserver les interfaces et membres de route ; empêcher commandes concurrentes,
  changements de clés et retransmissions différées incompatibles avec l’étape.

## 8. Validation avant toute action sur les cartes

1. Confirmer les profils initiaux complets, surtout ST. Distinguer hypothèse,
   configuration saisie et lecture actuelle.
2. Valider ce contrat : étapes, délais et critères de preuve. Fixer ensuite le
   format radio des opérations nouvelles et la compatibilité des versions.
3. Démontrer la machine d’état du firmware commun avec tests sur hôte : bascules,
   propriétaire unique, baux, pertes d’accord, redémarrage, panne et retour.
4. Comparer les codecs Go/firmware avec des vecteurs indépendants, puis simuler
   le parcours complet à profils différents et le parcours à profil commun.
5. Vérifier contrôle préalable non destructif, reprise des pages, phase de
   rollback de 190 s, confirmation/relecture et retour de chaque carte à son
   service. Tester également les refus de version/capacité/profil inconnus.
6. Compiler pour les modèles concernés et installer Graywolf localement selon
   les règles du dépôt, avec copie de retour arrière. Cela n’autorise aucun flash.
7. Présenter les versions/images/paramètres et obtenir une autorisation distincte
   avant tout essai radio ou flash. Aucun transfert ne démarre à partir du seul
   exemple de ce document.

## 9. État réel du prototype existant

Le moteur Go, les board-id explicites et le contrôle préalable à profil commun
ont été testés logiciellement et installés. Les fixtures indépendantes comportent
19 requêtes et 8 réponses du client de référence, à 128 octets ; elles ne valident
pas le nouveau rendez-vous ni tout le cycle matériel. Le contrôle existant peut
préparer des rôles sur un profil commun et libérer par expiration suivie de
réponses APRS observées ; ce n’est pas encore le contrat de preuves décrit ici.
Les fichiers Python archivés restent des références non livrées et non exécutées.

Le cycle physique historique a utilisé le client Python à 160 octets. Les
variantes Heltec et QRP Labs ont des tests logiciels, pas de validation physique
établie. Des builds locaux de coordination sont maintenant compiles pour
TTGO, Heltec V3 et QRP Labs ; aucun n'a ete flashe ou publie.
L’audit conserve les détails historiques ; cette section et le présent contrat
bornent les conclusions actuelles. Une publication reste soumise aux règles
AGENTS.md, indépendamment de la validation de la séquence.

## 10. Implementation locale du rendez-vous, 14 novembre

Le firmware unifie et le moteur Go possedent maintenant un premier parcours
a deux profils de service. Le controleur USB et le relais doivent partager
le profil `normal` ; `target_normal` fournit explicitement celui de la cible.
L'absence de ce champ conserve le parcours existant a profil commun, sans
migration des frequences ni des cles enregistrees. Trois profils de service
distincts et la reconstruction automatique d'un rendez-vous perdu restent
hors de cette premiere implementation.

Graywolf prepare le role du relais, verifie son profil et sa capacite par
HMAC avec sa propre cle de commande, ouvre une session et demande le
rendez-vous. L'accord est emis sur le service initial ; le firmware attend
ensuite 2 secondes avant d'appliquer le profil de la cible. Graywolf rejoint
ce profil, le relit par HMAC, prepare la cible puis verifie son propre
service. Seuls les messages de preparation explicitement routes entre le
controleur et la cible sont retransmis pendant cette phase ; pas le trafic
APRS ordinaire. Les reponses texte ne sont pas des preuves authentifiees.

Le retour du transfert rapide conserve ce rendez-vous pour l'activation,
le rollback eventuel, la confirmation et la relecture finale. Les maintiens
du relais utilisent sa session et des numeros strictement croissants ; ils
ne confirment pas le firmware de la cible. Graywolf les envoie entre ses
transactions, y compris pendant l'attente de rollback. Le bail expire apres
60 secondes sans maintien accepte, avec une borne absolue de 2 heures.
Une perte de session fait echouer l'operation ; elle ne justifie jamais
d'effacer le point de reprise ou d'annoncer une restauration non observee.

Le controle prealable a profils distincts utilise une fermeture authentifiee
sans ABORT. Apres l'operation, le relais revient sur son service et Graywolf
relit son etat inactif par un challenge HMAC. Le controleur revient a son
propre service par BRIDGE OFF, puis son profil applique est relu sur USB.
Un retour non verifie rend l'operation en echec, meme si l'image a ete confirmee.
Les resultats d'installation et de restauration restent separes dans le job.

### Extension binaire experimentale

Extension du protocole OTA2 existant (prefixe `3C FF 7E 02`, HMAC existant,
identites signees, entiers big-endian), pas une extension de LACF/RXT :

| Operation | Corps de requete | Effet |
| --- | --- | --- |
| 32 RADIO_STATE | vide ; nonce = challenge aleatoire | Reponse d'etat sans armer une session, sans renouveler le bail |
| 33 RENDEZVOUS | profil 11 octets + destinataire RF cible sur 8 octets | Session HELLO requise ; accord avant bascule ; aucun changement de Config |
| 34 MAINTAIN | vide ; offset = sequence strictement croissante | Renouvelle le seul bail de rendez-vous ; nonce de session obligatoire |
| 35 RELEASE | vide, offset nul | Suspend la session sans supprimer les pages durables ; retour du relais apres accord |

La reponse comporte les 6 octets habituels (op, statut, written), puis 31
octets : version 1, phase (0 service, 1 attente, 2 controle, 3 retour), armed,
route-busy, profil-connu, profil de service sur 11 octets, profil applique
sur 11 octets et sequence sur 4 octets. Le profil applique est le dernier
profil dont l'application par le pilote a reussi, pas une mesure RF externe.
Un echec du pilote rend la preuve inconnue ; Config n'est pas substituee a
un profil temporaire dont l'application a echoue. Le challenge est repris
dans le nonce de RADIO_STATE ; les mutations utilisent le nonce HELLO.

Les commandes USB RADIOPROFILE et RADIOSERVICE distinguent profil applique
et service configure. Les firmwares ne connaissant pas cette extension sont
refuses avant toute bascule distante. Le firmware de depart ET la candidate
doivent conserver la coordination ; le moteur controle le service de la
candidate avant confirmation. Les anciens binaires OTA/C publies ne doivent
donc pas etre presentes comme compatibles avec ce nouveau parcours.

### Verification et limites

- Tests C++ de `include/ota_rendezvous.h` : attente apres accord, rejeu,
  expiration, limite absolue, reset et debordement de millis.
- Vecteur C++ independant `rendezvous-state.hex` lu par le decodeur Go.
- Simulations Go : controle sans blocs, cycle complet, rollback de la cible
  pendant le maintien du relais, accord perdu et preuve de profil incorrecte.
- Les tests logiciels ne valident ni les temps reels de bascule radio ni la
  retransmission APRS sur les cartes. L'essai physique est encore a faire.
- Le profil de service verifie inclut la puissance transceiver ; les ajustements
  de puissance propres aux cartes S3 doivent etre verifies sur materiel.
- Aucun flash, aucune rotation de cle, aucune publication n'a ete effectue.
