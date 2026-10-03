#!/usr/bin/env python3
"""compliance-printed.py OUTDIR CODE... -- write compliance/printed/<CODE>.json.

The printed list is every card name in the set's Forge edition file [cards]
section at FORGE_REF, plus each name XMage's manifest lists under the same set
code that the [cards] section lacks (Jumpstart, starter-collection and promo
printings, which carry the set code and are legal wherever it is). A name that
differs from a [cards] name only by accents or punctuation is the same card.
FRA predates the union and keeps its [cards]-only list (it has no such extras).

Usage: python3 scripts/compliance-printed.py compliance/printed WOE FDN ...
"""
import json, os, re, sys, unicodedata, urllib.parse, urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
manifests = os.path.join(ROOT, "compliance", "manifests") + "/"
REF = re.search(r"^FORGE_REF\s*\??=\s*(\w+)", open(os.path.join(ROOT, "Makefile")).read(), re.M).group(1)
tree = json.load(urllib.request.urlopen(
    f"https://api.github.com/repos/Card-Forge/forge/git/trees/{REF}:forge-gui/res/editions"))["tree"]


def fetch(fn):
    u = f"https://raw.githubusercontent.com/Card-Forge/forge/{REF}/forge-gui/res/editions/" + urllib.parse.quote(fn)
    return urllib.request.urlopen(u).read().decode("utf-8")


BASICS = {"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes"}


def fold(s):
    s = unicodedata.normalize("NFKD", s)
    return re.sub(r"[^a-z0-9]", "", "".join(ch for ch in s if not unicodedata.combining(ch)).lower())


def norm(s):
    return re.sub(r"[^a-z0-9]", "", s.lower())


out = sys.argv[1]
for code in sys.argv[2:]:
    name = json.load(open(manifests + code + ".json"))["name"]
    cands = [t["path"] for t in tree if norm(t["path"][:-4]) == norm(name)]
    text = fn = None
    for fn in cands:
        t = fetch(fn)
        if re.search(rf"^Code={code}\s*$", t, re.M):
            text = t
            break
    if text is None:
        print(code, "NO EDITION FILE", name, cands)
        continue
    sec, names = None, set()
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("["):
            sec = line.lower()
            continue
        if sec != "[cards]" or not line:
            continue
        m = re.match(r"^\S+\s+[A-Z]\s+(.+?)(?:\s+@.*)?$", line)
        if not m:
            print(code, "UNPARSED", line)
            continue
        names.add(m.group(1).split(" $")[0].strip())
    src = f"Forge res/editions/{fn} [cards] at FORGE_REF {REF}"
    if code != "FRA":
        folded = {fold(n) for n in names}
        extra = {c["name"] for c in json.load(open(manifests + code + ".json"))["cards"]
                 if fold(c["name"]) not in folded and c["name"] not in BASICS}
        if extra:
            names |= extra
            src += f", plus {len(extra)} cards XMage lists under {code} (jumpstart, starter and promo printings)"
    doc = {"code": code, "source": src, "cards": sorted(names)}
    with open(f"{out}/{code}.json", "w") as f:
        json.dump(doc, f, indent=1, ensure_ascii=False)
        f.write("\n")
    print(code, len(names), fn)
