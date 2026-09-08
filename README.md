# 🌷 Tulipe

Traduction de livres EPUB par un modèle d'IA configurable, **chapitre par chapitre**.

Tulipe est un binaire unique, sans interface web ni dépendance à installer. Il
ouvre un menu interactif dans le terminal, ou s'utilise en ligne de commande
pour traiter des livres en lot.

```
tulipe                          menu interactif
tulipe livre.epub               menu, ouvert sur ce livre
tulipe translate livre.epub     traduction sans interface
tulipe providers                services connus et clé attendue
tulipe models                   liste des modèles, demandée au service
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
| Clé refusée, droit manquant, modèle inexistant, quota DeepL épuisé | arrêt immédiat de la traduction |
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

## Services

`tulipe providers` liste ce que Tulipe sait joindre. Trois protocoles :

| Protocole | Services |
|---|---|
| **Anthropic** | Claude, via le SDK officiel (réglage d'effort disponible) |
| **Compatible OpenAI** | Google AI Studio (Gemini), Mistral, Groq, Cerebras, NVIDIA NIM, Cohere, Cloudflare Workers AI, OpenAI, OpenRouter, Ollama et LM Studio en local, ou n'importe quel service parlant `/chat/completions` |
| **DeepL** | traducteur dédié, protocole propre |

Choisir un service dans les réglages remplit son URL de base ; une URL saisie à
la main n'est jamais écrasée. Plusieurs de ces services annoncent une offre
accessible sans carte bancaire — **les conditions exactes sont sur leur site**,
Tulipe n'en garde aucune copie : ces limites changent trop souvent pour qu'un
chiffre inscrit ici soit encore vrai quand vous le lirez.

### Choisir un modèle

Tulipe ne contient aucune liste de modèles. `tulipe models`, et la touche `m`
dans les réglages, interrogent le service lui-même :

```bash
tulipe models --provider groq
```

C'est la seule façon d'obtenir des noms exacts et à jour. Un service qui
n'expose pas cette liste vous laisse saisir le nom à la main.

### DeepL, un cas à part

DeepL ne se pilote pas par instructions : on lui donne les segments, il rend les
segments. Cela supprime d'un coup toute une classe de pannes — il ne peut ni
répondre à côté, ni rendre du JSON invalide, ni commenter sa traduction — et
son `tag_handling` déplace les balises en ligne avec les mots.

En échange, **le glossaire libre et les consignes de style ne s'appliquent
pas** : ce sont des instructions, et DeepL n'en prend pas. Il exige aussi un
code de langue cible (`--code fr`). Une clé du palier gratuit se termine par
`:fx` ; Tulipe la reconnaît et vise `api-free.deepl.com` sans qu'on ait à le
lui dire.

### Clé d'API

Par ordre de priorité : `TULIPE_API_KEY`, puis la variable propre au service
(`GROQ_API_KEY`, `MISTRAL_API_KEY`, `DEEPL_API_KEY`… — `tulipe providers` les
nomme toutes), puis le fichier de configuration. Une clé
placée dans une variable d'environnement ne touche jamais le disque — c'est la
méthode recommandée. Le fichier de configuration est créé en `0600` et la clé
n'est jamais affichée ni journalisée.

## Formats de sortie

`--format epub` (défaut) rend un livre complet : mise en forme, images,
feuilles de style et table de matières conservées.

`--format txt` rend la prose seule — titre du livre, puis chaque chapitre dans
l'ordre de lecture, paragraphes séparés par une ligne vide, sans balise ni
entité. Utile pour relire, comparer ou passer le texte à un autre outil. C'est
un rendu, pas un aller-retour : on ne peut pas en reconstruire l'EPUB.

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
--from-code      étiquette BCP 47 de la langue source (utile à DeepL)
--format         epub (défaut) ou txt
--style          consignes de style ajoutées aux instructions
--glossary-file  glossaire, une règle « source = cible » par ligne
-o               fichier de sortie
--no-resume      ne pas réutiliser les chapitres déjà traduits
--quiet          n'afficher que le chemin du fichier produit
```

Exemples :

```bash
# Claude, avec un glossaire
tulipe translate --to "español" --code es --effort high \
                 --glossary-file noms-propres.txt livre.epub

# un service gratuit compatible OpenAI
export GROQ_API_KEY=...
tulipe translate --provider groq --model "$(tulipe models --provider groq | head -1)" \
                 --to français --code fr livre.epub

# DeepL, sortie en texte brut
export DEEPL_API_KEY=...:fx
tulipe translate --provider deepl --to français --code fr --format txt livre.epub

# un modèle sur votre machine : rien ne sort du poste
tulipe translate --provider ollama --model qwen2.5:7b --to français --code fr livre.epub
```

## Cohérence sur la longueur d'un livre

Deux mécanismes tiennent le vocabulaire et le ton :

- le **glossaire**, injecté dans les instructions de chaque requête ;
- la **continuité** : la fin de la traduction précédente est montrée au modèle
  à titre de contexte, sans lui être redonnée à traduire.

Aucun des deux ne s'applique à DeepL, qui ne prend pas d'instructions.

## Références

- [EPUB 3.3, W3C Recommendation](https://www.w3.org/TR/epub-33/)
- [Open Container Format](https://www.w3.org/TR/epub-33/#sec-ocf)
- [SDK Go Anthropic](https://github.com/anthropics/anthropic-sdk-go)
