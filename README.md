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
| Nombre de traductions ≠ nombre de segments | le lot est coupé en deux et réessayé, jusqu'au segment isolé |
| Balisage mal formé | source conservée, passage signalé |
| Balises ajoutées ou perdues | traduction gardée, passage signalé |
| Balisage introduit dans du texte brut | source conservée, passage signalé |
| Traduction anormalement longue | source conservée, passage signalé |
| Erreur 429 / 5xx / réseau | nouvelle tentative, attente doublée à chaque essai |
| Réponse tronquée par la limite de jetons | le lot est coupé en deux et réessayé |

Le fichier de sortie **n'écrase jamais** un fichier existant : un suffixe
numérique est ajouté (`livre.fr.epub`, puis `livre.fr.2.epub`).

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
`~/.cache/tulipe/runs/<empreinte>/`, l'empreinte étant celle du livre, de la
langue cible et du modèle. Relancer une traduction interrompue reprend
exactement là où elle s'est arrêtée, sans repayer un seul chapitre. Le menu
« Reprendre une traduction » liste les travaux en cache ; `--no-resume`
désactive le mécanisme.

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
