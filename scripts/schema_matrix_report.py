#!/usr/bin/env python3
"""Summarise a synthetic schema matrix run; never equate collection with success."""
import argparse
import collections
import json
from pathlib import Path


def classify(row):
    if row.get("CollectionError"):
        return "collection-error"
    if row.get("LocalError") and row["Mode"] != "probe":
        return "local-block"
    if row.get("CallError"):
        statuses = row.get("Statuses") or []
        if statuses and statuses[-1] >= 400:
            return "http-" + str(statuses[-1])
        return "call-error"
    if row.get("ResponseErrors"):
        return "response-error"
    if "MAX_TOKENS" in (row.get("FinishReasons") or []):
        return "truncated"
    args = row.get("Arguments") or []
    if len(args) != 1:
        return "no-call" if not args else "multiple-calls"
    if "ToolNames" not in row:
        return "evidence-incomplete"
    if row["ToolNames"] != ["schema_probe"]:
        return "wrong-tool"
    if not all(row.get("OriginalValid") or [False]):
        return "nonconforming"
    if row["Prompt"] == "valid" and not all(row.get("RequestedMatch") or [False]):
        return "valid-but-changed"
    return "conforming"


def summarise(directory, allow_incomplete=False):
    rows = []
    for path in sorted(directory.glob("*.json")):
        row = json.loads(path.read_text())
        if "Route" in row and "Case" in row:
            rows.append(row)
    if not rows:
        raise ValueError("no matrix result files")
    rows.sort(key=lambda r: (r["Route"], r["Case"], r["Mode"], r["Prompt"]))
    keys = [(r["Route"], r["Case"], r["Mode"], r["Prompt"]) for r in rows]
    if len(set(keys)) != len(keys):
        raise ValueError("duplicate case identities")
    counters = collections.defaultdict(collections.Counter)
    observations = []
    for row in rows:
        outcome = classify(row)
        counters[row["Route"]][outcome] += 1
        observations.append({"route": row["Route"], "model": row["Model"],
            "case": row["Case"], "mode": row["Mode"], "prompt": row["Prompt"],
            "outcome": outcome, "statuses": row.get("Statuses") or [],
            "original_valid": row.get("OriginalValid") or [],
            "prepared_valid": row.get("PreparedValid") or [],
            "requested_match": row.get("RequestedMatch") or [],
            "warnings": sorted(set(w["Keyword"] for w in row.get("Warnings", []))),
            "tool_names":row.get("ToolNames"), "finish_reasons":row.get("FinishReasons",[]),
            "response_errors":row.get("ResponseErrors",[]),
            "collection_error":row.get("CollectionError",""),
            "error": row.get("CallError", "") or row.get("LocalError", "")})
    manifest_path = directory / "manifest.json"
    manifest=json.loads(manifest_path.read_text()) if manifest_path.exists() else None
    complete=False
    if manifest and manifest.get("format_version")==2:
        expected=set(manifest["planned"])
        actual={"/".join(k) for k in keys}
        complete=expected==actual and len(keys)==len(manifest["planned"]) and not manifest.get("collection_failed") and not any(r.get("CollectionError") for r in rows)
    if not complete and not allow_incomplete:
        raise ValueError("incomplete results or missing current manifest; use --allow-incomplete for exploration only")
    return {"manifest": manifest, "complete":complete,
        "records": len(rows), "http_attempts": sum(len(r.get("Statuses") or []) for r in rows),
        "counts_by_route": dict(counters), "observations": observations}


def markdown(summary):
    lines = ["# Live tool schema matrix", "",
        "These are observations from synthetic tool calls. A conforming sample is not proof of guaranteed provider enforcement.",
        "Totals mix default, fallback and unsupported provider probes. They are not production success rates.",
        "Valid and conflicting prompts each request exact arguments. No tools execute. `probe` bypasses the local catalogue at the HTTP boundary; it is test-only.", "",
        f"Records: {summary['records']}. HTTP attempts: {summary['http_attempts']}.", "",
        "| Route | Conforming | Nonconforming | Valid but changed | Local block | HTTP errors | Other errors/no call |",
        "|---|---:|---:|---:|---:|---:|---:|"]
    for route, c in sorted(summary["counts_by_route"].items()):
        http = sum(v for k,v in c.items() if k.startswith("http-"))
        other = sum(v for k,v in c.items() if k in ("call-error", "no-call", "multiple-calls", "response-error", "truncated", "wrong-tool", "evidence-incomplete", "collection-error"))
        lines.append(f"| {route} | {c.get('conforming',0)} | {c.get('nonconforming',0)} | {c.get('valid-but-changed',0)} | {c.get('local-block',0)} | {http} | {other} |")
    routes = sorted(summary["counts_by_route"])
    cases = sorted(set(r["case"] for r in summary["observations"]))
    index = {(r['route'],r['case'],r['mode'],r['prompt']):r for r in summary['observations']}
    codes = {'conforming':'C','nonconforming':'X','valid-but-changed':'D','local-block':'L','no-call':'N','call-error':'E','multiple-calls':'M','wrong-tool':'W','response-error':'E','truncated':'T','evidence-incomplete':'?','collection-error':'H' }
    lines += ["", "Cells show valid/conflicting prompt outcomes: C conforming, X nonconforming, D valid but changed, L local block, N no call, E call error, M multiple calls, W wrong tool, T truncated, ? incomplete evidence, HTTP status for provider rejection. A dash means the mode was not run."]
    for mode in ('default','fallback','probe'):
        lines += ["", "## " + mode, "", "| Rule | " + " | ".join(routes) + " |", "|---|" + "---|"*len(routes)]
        for case in cases:
            cells=[]
            for route in routes:
                pair=[]
                for prompt in ('valid','conflict'):
                    r=index.get((route,case,mode,prompt))
                    pair.append('-' if r is None else codes.get(r['outcome'],r['outcome'].replace('http-','')))
                cells.append('/'.join(pair))
            lines.append('| '+case+' | '+' | '.join(cells)+' |')
    return '\n'.join(lines)+'\n'


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory',type=Path)
    parser.add_argument('--output',type=Path,required=True,help='Output basename for .json and .md summaries')
    parser.add_argument("--allow-incomplete",action="store_true")
    args=parser.parse_args()
    summary=summarise(args.directory,args.allow_incomplete)
    args.output.parent.mkdir(parents=True,exist_ok=True)
    args.output.with_suffix('.json').write_text(json.dumps(summary,indent=2)+'\n')
    args.output.with_suffix('.md').write_text(markdown(summary))
    print(f"{summary['records']} records, {summary['http_attempts']} HTTP attempts")

if __name__=='__main__':
    main()
