# 🌷 Tulipe

Traduction de livres EPUB par un modèle d'IA configurable, **chapitre par chapitre**.

Tulipe est un binaire unique, sans interface web ni dépendance à installer. Il
ouvre un menu interactif dans le terminal, ou s'utilise en ligne de commande
pour traiter des livres en lot.

```
tulipe                          menu interactif
tulipe livre.epub               menu, ouvert sur ce livre
tulipe translate livre.epub     traduction sans interface
tulipe config                   configuration courante
```

## Le principe

Un livre n'est jamais envoyé d'un bloc à un modèle. Tulipe procède ainsi :

1. Il lit le conteneur EPUB et suit le **spine** pour trouver les documents de
   contenu dans l'ordre de lecture.
2. Pour chaque document, il repère les passages traduisibles et **note leur
   position exacte, en octets**, dans le fichier d'origine.
3. Il regroupe ces passages en lots bornés (4 000 caractères par défaut) et
   envoie **un lot par requête**. Le contexte du modèle ne porte jamais le
   livre, ni même un chapitre entier.
4. Il réinjecte chaque traduction à sa position d'origine. Tout ce qui n'est pas
   un passage traduisible — prologue XML, DOCTYPE, espaces de noms, entités,
   attributs, feuilles de style, images, code — reste **identique octet pour
   octet**.

Le modèle ne réécrit donc jamais le document : il ne voit que de la prose et le
balisage en ligne qui la traverse.

## Ce qui est traduit, et ce qui ne l'est pas

**Traduit** : les blocs de texte (`p`, `h1`–`h6`, `li`, `blockquote`, `td`, `th`,
`dt`, `dd`, `figcaption`, `caption`), le texte libre hors de ces blocs, le
`<title>` des documents, et les titres de chapitres de la table des matières
(`nav.xhtml` en EPUB 3, `toc.ncx` en EPUB 2).

**Laissé intact** : `script`, `style`, `pre`, `svg`, `math`, les valeurs
d'attributs (donc les `alt` et les `title`), les URL, et tout ce qui n'est pas
du texte.

**Refusé** : un document qui déclare un encodage autre qu'UTF-8 ou ASCII. Le
transcoder décalerait tous les offsets sur lesquels repose la réinjection ; le
document est donc laissé intact et signalé, plutôt que corrompu.

**Métadonnées** : `<dc:language>` et l'attribut `lang` de chaque document sont
mis à jour vers la langue cible. Le titre du livre (`<dc:title>`) et le nom de
l'auteur sont laissés tels quels — les traduire est une décision éditoriale qui
ne revient pas à l'outil.

## Garde-fous

Une traduction n'est acceptée que si elle passe ces contrôles. Sinon, **le texte
source est conservé** et le passage est signalé dans le rapport de fin ; rien
n'est jamais perdu silencieusement.

| Contrôle | Conséquence |
|---|---|
| Nombre de traductions ≠ nombre de segments | un essai immédiat, puis le lot est coupé en deux, jusqu'au segment isolé |
| Réponse illisible (pas du JSON, contenu vide) | idem |
| Balisage mal formé | source conservée, passage signalé |
| Balises ajoutées ou perdues | traduction gardée, passage signalé |
| Balisage introduit dans du texte brut | source conservée, passage signalé |
| Traduction anormalement longue | source conservée, passage signalé |
| Erreur 429 / 5xx / réseau | nouvelle tentative, attente doublée à chaque essai |
| Service muet | l'appel est abandonné après `--timeout`, puis réessayé |
| Clé refusée, droit manquant, modèle inexistant | arrêt immédiat de la traduction |
| Six échecs d'affilée | arrêt de la traduction |
| **Un document dont aucun segment n'a pu être traduit** | **document en échec, il n'est ni remplacé ni mis en cache** |

Deux distinctions comptent ici. Une **panne** (429, 5xx, réseau, silence) se
réessaie en attendant de plus en plus longtemps. Une **réponse mal formée** ne
se réessaie qu'une fois, sans attente : patienter n'y change rien, redécouper
si. Et un service qui échoue six fois de suite, ou qui refuse la clé, fait
arrêter la traduction — plutôt que de parcourir tout le livre à perte.

Le fichier de sortie **n'écrase jamais** un fichier existant : un suffixe
numérique est ajouté (`livre.fr.epub`, puis `livre.fr.2.epub`).

### Codes de sortie

| Code | Signification |
|---|---|
| `0` | tous les documents ont été traduits ; des passages isolés peuvent être signalés |
| `1` | erreur de configuration ou d'ouverture du livre, **ou** au moins un document non traduit |

Quand un document échoue, le fichier est tout de même écrit : le travail
partiel est conservé, les documents en échec y figurent en langue source, et le
message final les nomme.

## Modèles

Deux fournisseurs :

- **`anthropic`** — via le SDK Go officiel. Modèle par défaut `claude-opus-5`.
  Le niveau d'`effort` (`low` → `max`) règle le soin apporté à la traduction et
  ce qu'elle coûte.
- **`openai-compatible`** — tout service parlant le protocole
  `/chat/completions` : Ollama, LM Studio, llama.cpp, ou un agrégateur. Avec un
  service local, aucune donnée ne quitte la machine.

### Clé d'API

Par ordre de priorité : `TULIPE_API_KEY`, puis `ANTHROPIC_API_KEY` ou
`OPENAI_API_KEY` selon le fournisseur, puis le fichier de configuration. Une clé
placée dans une variable d'environnement ne touche jamais le disque — c'est la
méthode recommandée. Le fichier de configuration est créé en `0600` et la clé
n'est jamais affichée ni journalisée.

## Reprise

Chaque document traduit est mis en cache sous
`~/.cache/tulipe/runs/<empreinte>/`. Relancer une traduction interrompue reprend
exactement là où elle s'est arrêtée, sans repayer un seul chapitre. Le menu
« Reprendre une traduction » liste les travaux en cache ; `--no-resume`
désactive le mécanisme.

L'empreinte couvre **tout ce qui change le résultat** : le livre, le
fournisseur, le modèle, l'effort, les langues source et cible, le glossaire et
les consignes de style. Corriger un glossaire et relancer retraduit donc le
livre au lieu de rendre l'ancienne version. Le découpage (`--chunk`,
`--max-segments`) n'entre pas dans l'empreinte : le régler ne jette pas le
cache.

Un document en échec n'est jamais mis en cache — sans quoi l'échec serait figé
et reproduit à chaque reprise.

## Compter les jetons

Tulipe n'affiche que les décomptes **rapportés par le fournisseur**. Quand un
service ne les communique pas, l'interface l'écrit — elle n'estime rien. Aucun
coût en monnaie n'est calculé : les tarifs changent, et un chiffre inventé
serait pire qu'aucun chiffre.

## Installation

```bash
go build -o tulipe ./cmd/tulipe
```

Go 1.24 ou plus récent. Aucune autre dépendance système.

## Options de `translate`

```
--to             langue cible, écrite comme un humain l'écrirait
--code           étiquette BCP 47 inscrite dans les métadonnées
--from           langue source (vide : détectée par le modèle)
--provider       anthropic | openai-compatible
--model          identifiant du modèle
--base-url       URL de base d'un service compatible OpenAI
--effort         low | medium | high | xhigh | max
--chunk          caractères source par requête (défaut 4000)
--max-segments   segments par requête (défaut 40)
--max-tokens     jetons de réponse par requête (défaut 16000)
--attempts       tentatives avant redécoupage d'un lot (défaut 4)
--context        caractères de continuité montrés au modèle (défaut 400)
--timeout        secondes accordées à un appel au modèle (défaut 300)
--style          consignes de style ajoutées aux instructions
--glossary-file  glossaire, une règle « source = cible » par ligne
-o               fichier de sortie
--no-resume      ne pas réutiliser les chapitres déjà traduits
--quiet          n'afficher que le chemin du fichier produit
```

Exemple :

```bash
tulipe translate --to "español" --code es --effort high \
                 --glossary-file noms-propres.txt livre.epub
```

## Cohérence sur la longueur d'un livre

Deux mécanismes tiennent le vocabulaire et le ton :

- le **glossaire**, injecté dans les instructions de chaque requête ;
- la **continuité** : la fin de la traduction précédente est montrée au modèle
  à titre de contexte, sans lui être redonnée à traduire.

## Références

- [EPUB 3.3, W3C Recommendation](https://www.w3.org/TR/epub-33/)
- [Open Container Format](https://www.w3.org/TR/epub-33/#sec-ocf)
- [SDK Go Anthropic](https://github.com/anthropics/anthropic-sdk-go)
