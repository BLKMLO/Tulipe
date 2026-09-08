# CLAUDE.md

Repères pour travailler dans ce dépôt.

## Ce qu'est Tulipe

Un binaire Go unique qui traduit des livres EPUB avec un modèle d'IA
configurable, un document à la fois. Interface : un menu TUI (Bubble Tea) et un
mode non interactif à drapeaux. **Il n'y a pas, et il ne doit pas y avoir,
d'interface web, de serveur HTTP local, ni d'assets embarqués.**

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

## Règles de conduite du traducteur

Dans `internal/translate` :

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

## Chiffres affichés

`llm.Usage` porte un champ `Reported`. Un fournisseur qui ne renvoie pas de
décompte laisse `Reported` à faux, et l'interface écrit « non communiqués »
plutôt que zéro. **Ne jamais estimer, extrapoler ou convertir un décompte de
jetons en monnaie** : les tarifs changent, un chiffre inventé est pire que pas
de chiffre.

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

## Tests

Les tests ne font aucun appel réseau. `internal/translate/translate_test.go`
utilise un `fakeProvider` scripté ; `internal/ui/ui_test.go` pilote le modèle
Bubble Tea par messages, sans pseudo-terminal. Garder cette propriété : un test
qui exige une clé d'API ne sera jamais exécuté.
