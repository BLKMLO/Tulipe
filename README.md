<div align="center">

# 🌷 Tulipe

**Traduisez un livre entier dans une autre langue — sans l'abîmer.**

Tulipe prend un EPUB, le fait traduire chapitre par chapitre par le modèle d'IA
de votre choix, et vous rend un livre qui s'ouvre exactement comme l'original :
même mise en page, mêmes images, même table des matières. Seule la langue a
changé.

[Télécharger](https://github.com/BLKMLO/Tulipe/releases/latest) ·
[Premiers pas](#premiers-pas) ·
[Services acceptés](#choisir-un-service)

</div>

---

```
🌷 Tulipe  ·  traduction en cours

███████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  40%  2/5 documents

  ✓ I. Le retour au pays      47/47 segments en 52s
  ✓ II. La lettre             61/62 segments en 1m11s  ⚑ 1
  ⣾ III. Sous les tilleuls    23/58 segments
  · IV. L'hiver
  · V. Le départ

│  écoulé  3m34s
│  jetons  48 210 entrants · 19 844 sortants

échap annuler (le travail déjà fait est conservé)
```

## Pourquoi Tulipe

**Votre livre ressort intact.** Tulipe ne demande jamais au modèle de réécrire
vos fichiers : il repère les passages de prose, les fait traduire, et les
remet exactement à leur place. Tout le reste — mise en forme, images, notes de
bas de page, liens — n'est même pas touché.

**Vous choisissez qui traduit.** Treize services, dont plusieurs proposent une
offre gratuite : Google AI Studio, Mistral, Groq, Cerebras, NVIDIA, Cohere,
Cloudflare. Ou Claude, ou DeepL. Ou un modèle qui tourne sur votre machine,
auquel cas votre livre ne quitte jamais votre ordinateur.

**Une interruption ne coûte rien.** Coupure réseau, fenêtre fermée, quota
atteint : relancez, Tulipe reprend au chapitre suivant. Vous ne repayez jamais
un chapitre déjà traduit.

**Aucun échec silencieux.** Si un passage n'a pas pu être traduit, il reste en
langue d'origine, Tulipe vous le dit et vous propose de le reprendre — sans
repayer le chapitre. Vous ne découvrirez pas au chapitre 12 qu'une clé
invalide vous a rendu une copie de l'original.

## Installation

Téléchargez l'archive de votre système depuis la
[dernière version](https://github.com/BLKMLO/Tulipe/releases/latest), extrayez
le fichier `tulipe` qu'elle contient, et placez-le où vous voulez.

| Votre système | Fichier à prendre |
|---|---|
| Linux | `tulipe-…-linux-amd64.tar.gz` |
| macOS | `tulipe-…-macos-amd64.tar.gz` |
| Windows | `tulipe-…-windows-amd64.zip` |

Un fichier `SHA256SUMS` accompagne les archives si vous voulez vérifier ce que
vous avez téléchargé :

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

Ou compilez depuis les sources — il suffit de Go 1.24, rien d'autre :

```bash
go build -o tulipe ./cmd/tulipe
```

## Premiers pas

**1. Obtenez une clé.** Chez le service de votre choix — `tulipe providers`
liste les treize, avec un lien vers la page où l'on récupère une clé et le nom
de la variable où la ranger. Plusieurs n'exigent pas de carte bancaire.

```bash
export GROQ_API_KEY=votre_clé
```

**2. Lancez Tulipe.**

```bash
tulipe
```

Un menu s'ouvre. « Réglages » pour choisir le service, la langue et le modèle
(la touche `m` demande au service la liste de ses modèles). « Tester la
connexion » vérifie que tout répond. Puis « Traduire un EPUB ».

```
🌷 Tulipe  ·  traduction d'EPUB, chapitre par chapitre

› Traduire un EPUB          choisir un fichier et lancer la traduction
  Reprendre une traduction  réutiliser les chapitres déjà traduits
  Réglages                  modèle, langue, découpage, glossaire
  Tester la connexion       une requête minuscule pour vérifier le modèle
  Quitter

│  modèle  claude-opus-5 via anthropic
│  langue  français
│  clé API définie (ANTHROPIC_API_KEY)

↑/↓ naviguer  •  entrée choisir  •  q quitter
```

**3. Récupérez votre livre.** Il apparaît à côté de l'original, avec la langue
dans son nom : `mon-livre.fr.epub`. L'original n'est jamais modifié, et un
fichier existant n'est jamais écrasé.

## En ligne de commande

Pour traiter plusieurs livres, ou automatiser :

```bash
# le cas courant
tulipe translate --to français --code fr mon-livre.epub

# avec un service gratuit, et un glossaire pour tenir les noms propres
tulipe models --provider groq          # pour connaître les modèles proposés
tulipe translate --provider groq --model «le modèle choisi» \
                 --to français --code fr \
                 --glossary-file noms-propres.txt mon-livre.epub

# en texte brut plutôt qu'en EPUB
tulipe translate --to français --code fr --format txt mon-livre.epub

# sur votre machine : rien ne sort de l'ordinateur
tulipe translate --provider ollama --model «votre modèle local» \
                 --to français --code fr mon-livre.epub
```

Le programme rend `0` quand le livre est entièrement traduit, `1` sinon — et
dans ce cas il écrit quand même le fichier, en vous nommant ce qui manque.

## Choisir un service

`tulipe providers` affiche la liste complète avec, pour chacun, l'adresse, la
variable d'environnement attendue et le lien vers sa page d'inscription.

| | Services |
|---|---|
| **Offre gratuite annoncée** | Google AI Studio (Gemini), Mistral, Groq, Cerebras, NVIDIA NIM, Cohere, Cloudflare Workers AI |
| **Sur votre machine** | Ollama, LM Studio |
| **Autres** | Claude, OpenAI, OpenRouter, DeepL |

Les conditions de chaque offre gratuite sont sur le site du service. Tulipe
n'en garde aucune copie : ces limites changent trop souvent pour qu'un chiffre
inscrit ici soit encore vrai quand vous le lirez.

Même remarque pour les modèles : Tulipe ne contient aucune liste. `tulipe
models` interroge le service, ce qui vous donne des noms exacts et à jour.

```bash
tulipe models --provider groq
```

**DeepL** fonctionne un peu différemment des autres. Ce n'est pas un modèle
qu'on instruit, mais un traducteur : on lui passe le texte, il rend le texte.
En pratique c'est plus fiable, mais le glossaire et les consignes de style ne
s'y appliquent pas — ce sont des instructions, et DeepL n'en prend pas.

### Votre clé reste chez vous

Tulipe cherche la clé dans les variables d'environnement avant de regarder son
fichier de configuration. Une clé rangée dans une variable ne touche donc
jamais le disque. Si vous préférez la stocker, le fichier est créé en `0600` et
la clé n'est jamais affichée ni écrite dans un journal.

## Deux formats de sortie

**EPUB** (par défaut) : un vrai livre, identique à l'original hormis la langue.

**Texte brut** (`--format txt`) : la prose seule, chapitre après chapitre, sans
balise. Pratique pour relire, comparer deux traductions, ou passer le texte à
un autre outil.

## Reprendre les passages manqués

Il arrive qu'un modèle bute sur un paragraphe : réponse vide, balisage abîmé,
sortie aberrante. Tulipe garde alors le texte d'origine plutôt que d'insérer
quelque chose de douteux, et marque le passage d'un drapeau.

À la fin de la traduction, il vous propose de les reprendre :

```
✗ 3 passage(s) laissés en langue source

p reprendre les passages non traduits  •  entrée retour au menu
```

Seuls ces passages sont renvoyés au modèle — trois paragraphes coûtent trois
paragraphes, pas trois chapitres. La proposition réapparaît quand vous rouvrez
le livre, et dans la liste « Reprendre une traduction ».

En ligne de commande :

```bash
tulipe translate --retry --to français --code fr mon-livre.epub
```

## Dire au modèle de quoi parle le livre

Une phrase suffit à caler le registre et à lever les ambiguïtés — « bar » n'a
pas le même sens dans un roman noir et dans un manuel de physique.

```bash
tulipe translate --about "un roman noir new-yorkais des années 1950" \
                 --to français --code fr mon-livre.epub
```

Dans l'interface, c'est le champ **Contexte du livre** des réglages. Laissé
vide, rien n'est ajouté au prompt.

Cette phrase est présentée au modèle comme du contexte, jamais comme une
consigne : elle ne peut pas se substituer aux règles qui protègent votre
fichier.

## Une traduction cohérente sur 300 pages

Un livre découpé en centaines de requêtes risque de dériver : le même
personnage rebaptisé au chapitre 8, le tutoiement qui devient vouvoiement.
Deux mécanismes l'évitent.

Le **glossaire** fixe le vocabulaire. Une règle par ligne, rappelée à chaque
requête :

```
Victory Mansions = Maison de la Victoire
Newspeak = novlangue
```

La **continuité** montre au modèle la fin du passage précédent, à titre de
contexte seulement, pour qu'il retrouve le ton et le rythme.

## Quand ça se passe mal

Une traduction n'est retenue que si elle tient debout. Sinon le texte d'origine
est conservé, et le passage est signalé dans le rapport de fin.

| Ce qui arrive | Ce que fait Tulipe |
|---|---|
| Le modèle répond de travers | il réessaie, puis découpe le lot en deux, jusqu'au paragraphe isolé |
| Un paragraphe reste intraduisible | le texte d'origine est gardé, et proposé à la reprise |
| Le modèle renvoie du balisage abîmé | une esperluette isolée est réparée ; le reste fait garder la source |
| La traduction contient des caractères interdits | la source est gardée : un tel livre ne s'ouvrirait pas |
| Le balisage revient cassé | le passage d'origine est gardé et signalé |
| Le service est surchargé | nouvelle tentative, en attendant de plus en plus longtemps |
| Le service ne répond plus | l'appel est abandonné après un délai, puis réessayé |
| Clé refusée, quota épuisé | arrêt immédiat, plutôt que de parcourir le livre à perte |
| Un chapitre n'a rien donné | il est marqué en échec, jamais présenté comme traduit |

Le chemin de sortie est vérifié **avant** la traduction : découvrir un dossier
inexistant après trois cents pages serait le pire moment. Et le fichier produit
est relu avant d'être écrit — s'il ne s'ouvrait pas, Tulipe préfère signaler le
document plutôt que rendre un livre cassé.

Les décomptes de jetons affichés sont ceux que le service communique. Quand il
n'en communique pas, Tulipe l'écrit — il n'estime rien, et ne convertit jamais
en euros : les tarifs changent, un chiffre inventé serait pire qu'aucun.

## Comment votre livre reste intact

C'est le cœur de Tulipe, et cela tient en une idée.

Un EPUB est un ensemble de fichiers XHTML. L'approche naïve consiste à donner
un chapitre entier au modèle et à lui demander de renvoyer le même fichier
traduit. Elle casse tôt ou tard : une balise oubliée, une entité mal recopiée,
un attribut réécrit — et la liseuse refuse le livre.

Tulipe ne fait jamais cela. Il note **la position exacte, en octets**, de
chaque passage de prose dans le fichier d'origine. Le modèle ne voit que ces
passages. Les traductions sont ensuite réinsérées à ces positions précises, et
tout le reste du fichier est recopié sans être relu : déclaration XML,
DOCTYPE, feuilles de style, images, scripts, attributs.

Le livre traduit est donc, littéralement, votre livre d'origine avec d'autres
mots dedans.

## Contribuer

```bash
go test ./...        # les tests
go test -race ./...  # avec détecteur de course
go vet ./...
```

Aucun test n'appelle le réseau ni n'exige de clé : tout tourne hors ligne.
[`CLAUDE.md`](CLAUDE.md) décrit l'architecture et les invariants à respecter.

Pour publier une version : onglet **Actions** → **Release** → **Run workflow**,
saisir le numéro (`v0.2.0`). Le workflow vérifie le code, compile les trois
binaires et publie. Pousser un tag `v*` produit le même résultat.

## Référence

### Ce qui est traduit

Le texte des chapitres, les titres de la table des matières, et le titre de
chaque document.

Ne sont pas envoyés au modèle : les blocs de code préformatés, les formules,
les graphiques vectoriels, et le contenu des attributs — donc les descriptions
d'images. Le titre du livre et le nom de l'auteur restent eux aussi tels quels :
les traduire est une décision éditoriale, qui ne revient pas à un outil.

Un document déclarant un encodage autre qu'UTF-8 est laissé intact et signalé,
plutôt que d'être converti au risque de l'abîmer.

### Reprendre une traduction

Les chapitres traduits sont conservés dans le dossier de cache de votre système
(`~/.cache/tulipe/` sous Linux). Relancer le même livre reprend là où il s'était arrêté ; l'entrée « Reprendre une traduction »
du menu liste les travaux en attente, et `--no-resume` ignore le cache.

Changer de modèle, de langue, de glossaire, de contexte ou de consignes relance
une traduction neuve : le cache tient compte de tout ce qui modifie le résultat.
Régler la taille des lots, en revanche, ne le jette pas.

### Options de `translate`

```
--to             langue cible, écrite comme un humain l'écrirait
--code           étiquette BCP 47 inscrite dans le livre (fr, es, pt-BR…)
--from           langue source (vide : détectée)
--from-code      étiquette BCP 47 de la source (utile à DeepL)
--provider       service à utiliser (voir « tulipe providers »)
--model          identifiant du modèle (voir « tulipe models »)
--base-url       adresse d'un service compatible OpenAI
--effort         low, medium, high, xhigh, max (Claude uniquement)
--format         epub (défaut) ou txt
-o               fichier de sortie
--glossary-file  glossaire, une règle « source = cible » par ligne
--style          consignes de style ajoutées aux instructions
--about          le livre en une phrase, pour caler le registre
--retry          reprendre les passages laissés en langue source
--no-resume      repartir de zéro, sans réutiliser le cache
--quiet          n'afficher que le chemin du fichier produit
```

Réglages plus fins, à ne toucher qu'en cas de besoin : `--chunk` (caractères
par requête, 4000), `--max-segments` (40), `--max-tokens` (16000),
`--attempts` (4), `--timeout` (300 s), `--context` (400).

### Autres commandes

```
tulipe                    ouvre le menu
tulipe livre.epub         ouvre le menu sur ce livre
tulipe providers          les services connus et la clé attendue
tulipe models             les modèles, demandés au service
tulipe config             la configuration courante
tulipe version
```

## Références

- [EPUB 3.3](https://www.w3.org/TR/epub-33/) — la spécification du format
- [Open Container Format](https://www.w3.org/TR/epub-33/#sec-ocf) — la structure de l'archive
