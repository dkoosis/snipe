#!/usr/bin/env python3
"""sn-6wv semantic-search probe runner and scorer.

  run   <snipe-binary> <corpus-dir> <out.json>
        For every row of sn-6wv-probe.tsv, run `snipe sim <query> --threshold 0
        --limit 20 --format json` inside <corpus-dir>/<repo> (an index built with
        `snipe index --force --embed-mode=realtime`) and record the 1-based rank of
        the best-ranked expected symbol (None = miss, i.e. beyond rank 20).
  score <before.json> <after.json>
        MRR@20, hit@1, hit@5 for each side plus a per-query before/after table
        (markdown) and the improved / worse / unchanged counts.

A result matches when its symbol short name (text after the last '.') equals one
of the expected names. Needs VOYAGE_API_KEY in the environment (never
written anywhere) and KEYRING_DISABLE=1.
"""
import json
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
PROBE = os.path.join(HERE, "sn-6wv-probe.tsv")
LIMIT = 20


def load_probe():
    rows = []
    for line in open(PROBE, encoding="utf-8"):
        if line.startswith("#") or not line.strip():
            continue
        repo, query, expected = line.rstrip("\n").split("\t")
        rows.append((repo, query, expected.split(",")))
    return rows


def run(binary, corpus, out):
    env = dict(os.environ, KEYRING_DISABLE="1")
    res = []
    for repo, query, expected in load_probe():
        p = subprocess.run(
            [binary, "sim", query, "--threshold", "0", "--limit", str(LIMIT), "--format", "json"],
            cwd=os.path.join(corpus, repo), env=env, capture_output=True, text=True,
        )
        if p.returncode != 0:
            sys.exit(f"sim failed for {repo!r} {query!r}: exit {p.returncode}: {p.stderr[:200]}")
        names = [r["name"].split(".")[-1] for r in json.loads(p.stdout)["results"]]
        rank = next((i + 1 for i, n in enumerate(names) if n in expected), None)
        res.append({"repo": repo, "query": query, "expected": expected, "rank": rank, "top5": names[:5]})
    json.dump(res, open(out, "w"), indent=1)


def metrics(rows):
    n = len(rows)
    rr = [1.0 / r["rank"] if r["rank"] else 0.0 for r in rows]
    return {
        "mrr": sum(rr) / n,
        "hit1": sum(1 for r in rows if r["rank"] == 1),
        "hit5": sum(1 for r in rows if r["rank"] and r["rank"] <= 5),
        "n": n,
    }


def fmt(rank):
    return str(rank) if rank else "miss"


def score(before, after):
    b, a = json.load(open(before)), json.load(open(after))
    mb, ma = metrics(b), metrics(a)
    print("| | MRR@20 | hit@1 | hit@5 |\n|---|---|---|---|")
    print(f"| before | {mb['mrr']:.3f} | {mb['hit1']}/{mb['n']} | {mb['hit5']}/{mb['n']} |")
    print(f"| after  | {ma['mrr']:.3f} | {ma['hit1']}/{ma['n']} | {ma['hit5']}/{ma['n']} |")
    print()
    for repo in dict.fromkeys(r["repo"] for r in b):
        sb = metrics([r for r in b if r["repo"] == repo])
        sa = metrics([r for r in a if r["repo"] == repo])
        print(f"- {repo}: MRR@20 {sb['mrr']:.3f} -> {sa['mrr']:.3f}, hit@5 {sb['hit5']} -> {sa['hit5']} (of {sb['n']})")
    print("\n| repo | query | expected | before | after | |\n|---|---|---|---|---|---|")
    up = down = same = 0
    for x, y in zip(b, a):
        rb, ra = x["rank"] or 10**6, y["rank"] or 10**6
        if ra < rb:
            up += 1
            tag = "better"
        elif ra > rb:
            down += 1
            tag = "WORSE"
        else:
            same += 1
            tag = "same"
        print(f"| {x['repo']} | {x['query']} | {', '.join(x['expected'])} | {fmt(x['rank'])} | {fmt(y['rank'])} | {tag} |")
    print(f"\nimproved {up}, worse {down}, unchanged {same}")


if __name__ == "__main__":
    if len(sys.argv) == 5 and sys.argv[1] == "run":
        run(*sys.argv[2:])
    elif len(sys.argv) == 4 and sys.argv[1] == "score":
        score(*sys.argv[2:])
    else:
        sys.exit(__doc__)
