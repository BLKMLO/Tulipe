# CLAUDE.md

Repères pour travailler dans ce dépôt.

## Ce qu'est Tulipe

Un binaire Go unique qui traduit des livres EPUB avec un modèle d'IA
configurable, un document à la fois. Interface : un menu TUI (Bubble Tea) et un
mode non interactif à drapeaux. **Il n'y a pas, et il ne doit pas y avoir,
d'interface web, de serveur HTTP local, ni d'assets embarqués.**

Sortie en EPUB par défaut, texte brut en option (`--format txt`).

## Commandes

```bash
go build ./...                     # compiler
go test ./...                      # tests
go test -race ./...                # tests avec détecteur de course
go vet ./... && gofmt -l ./cmd ./internal   # doit ne rien afficher
go build -o tulipe ./cmd/tulipe    # binaire
```

Tester à la main sans dépenser de jetons : lancer un serveur qui parle le
protocole `/chat/completions` sur `127.0.0.1`, puis

```bash
tulipe translate --provider openai-compatible --base-url http://127.0.0.1:PORT/v1 \
                 --model faux --to français --code fr livre.epub
```

`TULIPE_CONFIG` déplace le fichier de configuration — indispensable pour ne pas
écraser celui de l'utilisateur pendant un essai.

## Architecture

Le flux va toujours dans ce sens, sans retour :

```
cmd/tulipe        drapeaux, aiguillage TUI / sans interface
  └── internal/ui         Bubble Tea + mode sans interface (RunHeadless)
        └── internal/config      réglages persistés, fabrique de fournisseur
        └── internal/translate   pipeline livre → document → lot → requête
              ├── internal/epub  lecture, découpage, réinjection, écriture
              └── internal/llm   fournisseurs (Anthropic, compatible OpenAI)
```

`epub` et `llm` ne connaissent ni `translate` ni `ui`. `translate` ne connaît pas
`ui`. Garder ce sens : c'est ce qui rend le cœur testable sans terminal ni
réseau.

## Le catalogue de services

`internal/llm/presets.go` décrit chaque service joignable : identifiant, nom,
protocole (`Kind`), URL de base, variables d'environnement où chercher la clé.
`config.Provider` stocke un identifiant de preset ; `Preset.Kind` choisit le
client. Les anciennes valeurs `anthropic` et `openai-compatible` restent des
identifiants valides, ce qui garde les fichiers de configuration existants
lisibles.

**Deux règles à ne pas enfreindre dans ce fichier.**

D'abord, **aucun quota, aucun tarif, aucune limite de débit**. Ces chiffres
changent souvent ; inscrits en dur, ils deviennent faux sans prévenir et
induisent l'utilisateur en erreur. Chaque preset porte un lien `Docs` vers la
page du service, qui fait autorité. Le champ `FreeTier` dit seulement qu'une
offre gratuite est annoncée, jamais ce qu'elle contient.

Ensuite, **aucune liste de modèles**. Les noms de modèles bougent encore plus
vite que les quotas. `llm.ModelLister` interroge le service (`GET /v1/models`
côté OpenAI, l'endpoint Models côté Anthropic) ; `tulipe models` et la touche
`m` des réglages s'appuient dessus. Un service qui n'expose pas de liste laisse
l'utilisateur saisir le nom à la main — c'est le comportement voulu, pas une
lacune à combler par une liste écrite de mémoire.

Ajouter un service se limite normalement à une entrée dans `presets` ;
`TestPresetCatalogueIsCoherent` et `TestEveryPresetIsUsableOutOfTheBox`
vérifient qu'elle est complète et utilisable.

## Deux façons d'atteindre un service

`llm.Provider` (`Complete`) est le chemin par instructions : on construit un
prompt, le modèle répond en JSON, `parseTranslations` le lit.

`llm.DirectTranslator` (`TranslateSegments`) est le chemin direct : on passe les
segments, le service rend les segments. DeepL l'emprunte. `translator.request`
choisit par assertion de type — un fournisseur qui implémente `DirectTranslator`
court-circuite entièrement la construction de prompt et l'analyse JSON, donc
toutes les pannes qui vont avec.

Conséquence à garder en tête : **le glossaire et les consignes de style ne
s'appliquent pas** sur le chemin direct, puisqu'ils sont des instructions. Ils
restent dans `Recipe` (ils changeraient le résultat sur l'autre chemin), mais
ne sont pas envoyés à DeepL. Ne pas essayer de les simuler.

## L'invariant central

**Un document XHTML n'est jamais re-sérialisé.** `epub.Extract` renvoie des
`Segment` repérés par offset d'octets ; `epub.Apply` recolle les traductions
dans ces plages et copie tout le reste tel quel. C'est ce qui garantit que le
prologue XML, le DOCTYPE, les espaces de noms, les entités et les balises
auto-fermantes survivent.

Conséquence pratique : ne jamais introduire un parseur qui reconstruit le
document (`html.Render`, `xml.Encoder`, une bibliothèque DOM). Toute
modification d'un document se fait par découpe et recollage sur les octets
d'origine — voir `epub.SetLanguage` et `epub.SetDocumentLanguage` pour le
schéma à suivre.

Deux décodeurs cohabitent dans `internal/epub/segment.go`, et il ne faut pas les
confondre :

- `newDecoder` est **tolérant** (`Strict=false`, `AutoClose`, entités HTML). Il
  sert à *lire* des EPUB réels, souvent imparfaits.
- `strictDecoder` est **strict**. Il sert à *valider* ce que renvoie le modèle.
  Relâcher celui-ci laisserait passer du balisage cassé dans le livre.

Les deux utilisent `passthroughCharset`, qui accepte UTF-8 et ASCII en rendant
les octets tels quels et refuse tout le reste. Ne jamais y brancher un vrai
transcodeur : décaler les octets ferait pointer les offsets à côté. Un document
déclarant un autre encodage est refusé par `checkEncoding` avec un message
explicite.

Attention aussi aux balises auto-fermantes : `<dc:language/>` n'a pas de
contenu à remplacer. `elemSpan.rewriteText` reconstruit la paire de balises
dans ce cas ; insérer le texte après la balise produirait `<dc:language/>fr`,
c'est-à-dire une langue toujours vide et un nœud texte parasite.

Enfin, un segment de texte brut est extrait avec ses **entités**, donc une
traduction revient légitimement avec le `&amp;` qu'on lui a donné.
`NormaliseText` laisse passer une référence d'entité déjà valide et n'échappe
que le reste ; un échappement aveugle afficherait `&amp;amp;` au lecteur.

## Le rendu texte

`epub.PlainText` et `Book.PlainText` produisent la sortie `--format txt`. C'est
un **rendu**, pas un aller-retour : rien de ce qu'ils écrivent ne retourne
jamais dans un livre, ce qui est la seule raison pour laquelle ils ont le droit
de reconstruire le texte au lieu de le découper par offsets.

`Book.PlainText` lit le titre de chaque chapitre dans le document *tel qu'il est
à cet instant*, pas dans `Chapter.Title` : après une traduction, ce dernier
porte encore le libellé d'origine, et s'en servir imprimait chaque titre deux
fois, une par langue.

## Règles de conduite du traducteur

Dans `internal/translate` :

- **Deux politiques de réessai, à ne pas confondre.** `call` réessaie les
  pannes (429, 5xx, réseau, dépassement de `RequestTimeout`) avec une attente
  qui double. `request` réessaie une réponse *mal formée* une seule fois, sans
  attente : patienter ne corrige pas un JSON invalide, redécouper si. Appliquer
  l'attente exponentielle au second cas multiplie la durée d'un run par vingt
  sans rien améliorer.
- Un lot dont la réponse est inexploitable est **coupé en deux et réessayé**,
  récursivement, jusqu'au segment isolé (`translateRange`). Un segment qui
  échoue encore garde son texte source et produit une `Note`.
- Une chaîne de traduction vide veut dire « garder la source » : c'est le
  contrat entre `translateRange` et `epub.Apply`.
- Tout rejet produit une `Note`. **Rien n'est jamais écarté en silence** ; le
  rapport de fin les affiche toutes.
- Les contrôles d'acceptation sont dans `accept`. En ajouter un veut dire
  ajouter aussi un test dans `translate_test.go`, sur le modèle de
  `TestBrokenMarkupIsRejected`.

## Reprendre les passages manqués

Un segment dont la traduction est refusée par `accept` garde son texte source
et son indice est ajouté à `DocResult.Pending`. `FileCache` écrit cette liste
dans un fichier `.pending` à côté du document, via l'interface facultative
`PendingStore`.

`RetryPending` retraduit uniquement ces segments et les recolle dans le
document **déjà traduit**, pas dans la source. Deux garde-fous s'y trouvent :

- la source et le document précédent doivent avoir le même nombre de segments,
  sinon les positions ne correspondent plus et la fonction refuse ;
- `translator.numbers` remappe les indices du sous-ensemble vers ceux du
  document entier, faute de quoi les notes et le nouveau `Pending`
  désigneraient les mauvais paragraphes.

Un cache sans `.pending` — écrit avant cette fonctionnalité — répond « inconnu »,
ce qui est délibérément distinct de « rien en attente » : reprendre sur une
supposition retraduirait le livre entier. `pendingOf` porte cette distinction.

Une reprise qui échoue ne perd rien : `Book` remet le document précédent et
note l'échec.

## Ce qui ne doit jamais planter

Trois campagnes tiennent cette propriété, et il faut les garder vertes :

- `internal/ui/robustness_test.go` envoie **toutes** les touches à **tous** les
  écrans, dans tous leurs états — listes vides, indices périmés, livre absent,
  exécution absente — puis rend la vue. Il a trouvé trois plantages réels :
  lancer une traduction sans livre, un indice de liste survivant à un
  raccourcissement, et l'écran de progression sans exécution attachée.
- `internal/epub/fuzz_test.go` passe des octets arbitraires dans
  `Extract` → `Apply` → `Extract`. Six millions et demi d'exécutions sans
  échec après les corrections décrites plus bas.
- `TestValidateNeverPanics` combine des valeurs absurdes dans tous les champs
  de configuration à la fois.

Règle qui en découle : **une liste bornée par un indice se borne au moment de
l'utiliser**, pas au moment de le modifier. `clampIndex` existe pour ça ; s'en
remettre à « toutes les branches pensent à réinitialiser » est ce qui a produit
le plantage de la liste de reprise.

## Ce qu'un modèle peut renvoyer de pire

`epub.Apply` est la dernière fonction avant que des octets deviennent un livre.
Elle ne fait donc confiance à personne, y compris à `translate` :

- `SafeForXML` refuse l'UTF-8 invalide et les caractères que XML interdit. Un
  livre qui en porte un seul ne s'ouvre dans aucune liseuse.
- `entityLength` valide **ce que désigne** une référence numérique : `&#0;`
  ressemble à quatre caractères anodins mais décode vers un point de code
  interdit.
- `RepairAmpersands` échappe les esperluettes qui n'ouvrent pas d'entité
  valide. C'est le seul défaut de balisage dont la correction soit univoque :
  perdre un paragraphe entier pour un « Marks & Spencer » serait dommage.
- Un fragment de bloc mal formé est refusé et la source conservée.
- **`Apply` relit ce qu'elle produit.** Si le document ne se relit plus, elle
  renvoie une erreur au lieu de rendre un livre cassé. C'est la seule garantie
  qui tienne pour une source déjà mal formée, où le rééquilibrage des balises
  est impossible à prévoir.

Le fuzz a trouvé chacun de ces cas. Retirer l'un d'eux le fera réapparaître.

## Documents mal formés

Sur un document imparfait, le lecteur tolérant invente des balises fermantes —
et les octets qu'il a consommés en le faisant appartiennent à ce qui a déclenché
la réparation, pas à l'élément fermé. Se fier à la position rapportée faisait
**avaler la fin du fichier** par le segment, et le remplacer tronquait le
chapitre.

`Extract` borne donc chaque bloc à `lastEnd`, la fin du dernier jeton vu à
l'intérieur, et n'accepte la position rapportée que si `closesTag` confirme que
la source contient bien la balise fermante annoncée.

## Le disjoncteur

`failureCounter` compte les échecs consécutifs, **partagé par tous les
documents d'un livre** via un pointeur dans `Options`. Au-delà de
`StopAfterFailures` (6 par défaut), ou immédiatement sur une erreur que
`llm.Fatal` juge sans espoir (401, 403, 404), `guard` arme `abortErr` et la
traduction s'arrête sur `ErrServiceUnusable` ; `Book` propage et interrompt le
livre entier.

Piège à connaître, couvert par `TestSplittingIsNotMistakenForABrokenService` :
un lot redécoupé produit une chaîne d'échecs qui ressemble à une panne. Une
réponse mal formée **ne compte donc dans le disjoncteur que lorsque le lot ne
peut plus être coupé** (`hi-lo == 1`). Sans cette exception, un chapitre de 40
segments que le redécoupage allait sauver serait abandonné à la sixième
division.

## Ce qui compte comme un échec

Un document dont `Translated == 0` alors que `Segments > 0` est **en échec**,
pas en succès avec des notes. `Book` ne le remplace pas et ne le met pas en
cache. C'est le garde-fou contre le pire scénario possible : rendre un livre
non traduit avec une coche verte devant chaque chapitre — ce qui arrivait avec
une clé d'API invalide avant que ce contrôle n'existe.

Corollaire : `RunHeadless` renvoie une erreur (donc code de sortie 1) dès qu'un
document a échoué, tout en écrivant quand même le fichier. Le contrat est
« code 0 = livre entièrement traduit ».

Trois compteurs, à ne pas mélanger : `Attempted` (tout appel émis),
`Requests` (réponses exploitables), et `Usage` (accumulé dès qu'une réponse
HTTP arrive, exploitable ou non — les jetons sont dépensés dans les deux cas).

## Clé du cache de reprise

`translate.Recipe` liste **tout ce qui change le résultat** : fournisseur,
modèle, effort, langues (noms et codes), glossaire, consignes de style,
phrase de contexte. C'est ce qui donne la
clé du cache. Ajouter un réglage qui influence la traduction sans l'ajouter à
`Recipe` fait resservir en silence une traduction obtenue sous d'autres
réglages — un utilisateur qui corrige son glossaire récupérerait l'ancienne
version. `TestRecipeKeyChangesWithEverySemanticSetting` verrouille cette
propriété.

Le découpage (`ChunkChars`, `MaxSegments`) est délibérément hors de `Recipe` :
le régler ne doit pas jeter le cache.

## Le prompt

Pour le lire tel qu'il part au modèle, sans dépenser un jeton :

```bash
TULIPE_PROMPT=1 go test ./internal/translate/ -run TestDumpPrompt -v
```

Deux choses à ne pas défaire dans `prompt.go` :

- `encodeSegments` désactive l'échappement HTML de Go. Le modèle doit
  reproduire les balises exactement ; lui montrer `<em>` plutôt que
  `\u003cem\u003e` sert cette exigence et coûte moins de jetons. Le résultat
  reste du JSON valide, ce que `TestSegmentsAreSentWithReadableMarkup`
  vérifie.
- `Options.About` est présenté comme du **contexte**, explicitement pas comme
  une consigne. Sans cette précaution, une phrase telle que « traduis
  librement » se substituerait aux règles qui protègent le fichier.

## Chiffres affichés

`llm.Usage` porte un champ `Reported`. Un fournisseur qui ne renvoie pas de
décompte laisse `Reported` à faux, et l'interface écrit « non communiqués »
plutôt que zéro. **Ne jamais estimer, extrapoler ou convertir un décompte de
jetons en monnaie** : les tarifs changent, un chiffre inventé est pire que pas
de chiffre.

## Saisies utilisateur

`config.Validate` refuse tout ce qui ne peut pas marcher, avec un message qui
nomme le champ. Deux familles à ne pas relâcher :

- les **étiquettes de langue** (`--code`, `--from-code`) sont contraintes à une
  forme BCP 47. Elles finissent dans les métadonnées du livre ; un `<` non
  échappé y produirait un fichier illisible. `epub.SetLanguage` échappe en
  plus, par principe de double protection.
- les **champs libres** (langue, contexte, style, glossaire) sont plafonnés en
  longueur. Ils partent dans chaque requête : une valeur absurde doit être
  nommée ici, pas devenir une erreur obscure du service au milieu du livre.

## Clé d'API

Résolue par `config.ResolveAPIKey` : `TULIPE_API_KEY`, puis la variable propre
au fournisseur, puis le fichier. Le fichier est écrit en `0600`. La clé n'est
jamais affichée : `config.KeyStatus` ne décrit que sa provenance, et
`showConfig` s'appuie dessus. Ne pas ajouter de journalisation qui la ferait
transiter.

## Écriture de fichiers

`freeName` garantit qu'aucun fichier existant n'est écrasé. Une traduction est
longue et coûteuse : perdre un résultat par écrasement est inacceptable. Ne pas
contourner cette fonction.

`checkWritable` s'exécute **avant** la traduction, pas après. Découvrir qu'un
chemin de sortie est invalide une fois trois cents pages payées serait le pire
moment possible.

`epub.MaxUncompressedSize` borne ce qu'une archive peut occuper une fois
décompressée. C'est une variable et non une constante pour que le test puisse
éprouver la limite sans construire un demi-gigaoctet.

## Langue des chaînes

Le code, les commentaires et les identifiants sont en anglais. **Tout ce que
l'utilisateur lit — libellés d'interface, messages d'erreur, notes — est en
français**, y compris les erreurs renvoyées par `internal/epub`,
`internal/llm` et `internal/translate`, qui remontent telles quelles à l'écran.

Exception : les prompts envoyés au modèle sont en anglais (`prompt.go`), ce qui
donne de meilleurs résultats de suivi d'instructions ; la langue cible y est
passée en paramètre.

## API Anthropic

`internal/llm/anthropic.go` utilise le SDK Go officiel. Points à ne pas
régresser :

- modèle par défaut `claude-opus-5` ;
- pas de `temperature` ni de `budget_tokens` — retirés sur les modèles
  courants, ils renvoient une 400. Le réglage de dosage est
  `output_config.effort` ;
- requêtes en flux (`NewStreaming` + `Accumulate`) : un chapitre long
  dépasserait sinon le délai HTTP ;
- `stop_reason` est vérifié avant de lire le contenu (`refusal`, `max_tokens`) ;
- la sortie structurée se désactive d'elle-même pour le reste du run si l'API la
  refuse (`rejectsOutputConfig`). Même repli côté compatible OpenAI avec
  `response_format`.

Avant de toucher à ce fichier, consulter la compétence `claude-api` : les
paramètres de l'API ont changé récemment et la mémoire du modèle est périmée.

## Publication

`.github/workflows/release.yml` compile et publie. Il se déclenche sur un tag
`v*` ou à la main depuis l'onglet Actions, où la version est saisie et le tag
créé par le workflow.

La raison d'être de ce fichier : le jeton intégré à GitHub Actions a le droit
de créer tags et releases, ce qu'un jeton d'application externe n'a en général
pas. Publier depuis une session d'agent échoue en 403 sur `refs/tags/*` ; le
workflow est le chemin qui fonctionne.

Trois choses à ne pas défaire :

- Les vérifications (`gofmt`, `go vet`, `go test -race`) tournent **avant** la
  compilation. Une version ne se publie pas sur du code non vérifié.
- Le nom des archives dit `macos`, pas `darwin`. `darwin` est le `GOOS` de Go,
  exact mais illisible pour qui télécharge.
- L'archive contient un binaire nommé simplement `tulipe`, pas le nom long de
  l'archive : c'est ce qui est extrait et mis dans le `PATH`.

Le projet est en Go pur sans cgo, donc un seul runner compile les trois cibles
par compilation croisée ; inutile d'ouvrir une matrice de trois machines.

## Tests

Les tests ne font aucun appel réseau vers l'extérieur ; `internal/llm` utilise
`httptest` pour ses serveurs, ce qui reste local. `internal/translate/translate_test.go`
utilise un `fakeProvider` scripté pour le chemin par instructions et un
`fakeDirect` pour le chemin direct ; `internal/ui/ui_test.go` pilote le modèle
Bubble Tea par messages, sans pseudo-terminal. Garder cette propriété : un test
qui exige une clé d'API ne sera jamais exécuté.
