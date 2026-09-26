#!/usr/bin/env python3
"""Ask the same questions of Claude Code with and without the demo bundle.

Both runs get the same prompt and the same BigQuery access. The only
difference is that one also has an ochakai MCP server that serves
examples/demo. A second Claude call grades each answer against the
question's rubric; the truth for a numeric question is computed by BigQuery
at run time, because thelook_ecommerce regenerates its history daily.

    OCHAKAI_URL=http://localhost:8080 EVAL_PROJECT=my-project \
        python3 examples/demo-eval/run.py [--model claude-sonnet-5] [--repeat 1]

The instance at OCHAKAI_URL should hold examples/demo and nothing else.
The agent is given read tools only, so a run leaves the base unchanged.
"""

import argparse
import datetime
import json
import os
import pathlib
import subprocess
import tempfile

HERE = pathlib.Path(__file__).resolve().parent

READ_TOOLS = [
    "Bash(bq query:*)", "Bash(bq show:*)", "Bash(bq ls:*)", "Bash(bq head:*)",
]
BASE_TOOLS = [
    "mcp__ochakai__search_concepts", "mcp__ochakai__get_concept",
    "mcp__ochakai__list_concepts", "mcp__ochakai__get_file",
]
# The write tools are denied rather than left unapproved: an agent that
# sees report_outcome tends to spend its last message asking for it.
DENIED = [
    "WebSearch", "WebFetch", "Agent", "Task",
    "mcp__ochakai__put_concept", "mcp__ochakai__report_outcome",
]


def warehouse_reachable(project):
    # An expired gcloud login turns every answer into a request to sign in
    # again, which the judge then grades as a wrong answer. Stop instead.
    done = subprocess.run(
        ["bq", "query", f"--project_id={project}", "--use_legacy_sql=false",
         "--format=json", "SELECT 1"], capture_output=True, text=True)
    return done.returncode == 0


def truth(sql, project):
    out = subprocess.run(
        ["bq", "query", f"--project_id={project}", "--use_legacy_sql=false",
         "--format=json", sql],
        check=True, capture_output=True, text=True).stdout
    row = json.loads(out)[0]
    return next(iter(row.values()))


def claude(prompt, workdir, model, mcp_config=None, tools=()):
    cmd = ["claude", "-p", prompt, "--output-format", "json",
           "--model", model, "--setting-sources", "project",
           "--strict-mcp-config", "--disallowedTools", *DENIED]
    if tools:
        cmd += ["--allowedTools", *tools]
    if mcp_config:
        cmd += ["--mcp-config", mcp_config]
    done = subprocess.run(cmd, cwd=workdir, capture_output=True, text=True,
                          timeout=1800)
    try:
        return json.loads(done.stdout)
    except json.JSONDecodeError:
        return {"result": done.stdout + done.stderr, "is_error": True}


def ask(q, condition, args, workdir, mcp_config):
    today = datetime.date.today().isoformat()
    prompt = (
        "あなたはデータ分析のエージェントです。データは BigQuery の "
        "`bigquery-public-data.thelook_ecommerce` にあり、"
        f"`bq query --project_id={args.project} --use_legacy_sql=false` で"
        f"調べられます。今日は {today} です。次の質問に日本語で答え、"
        "最後に結論を 3 行以内でまとめてください。\n\n" + q["question"])
    if condition == "bundle":
        return claude(prompt, workdir, args.model, mcp_config,
                      READ_TOOLS + BASE_TOOLS)
    return claude(prompt, workdir, args.model, None, READ_TOOLS)


def grade(q, answer, t, args, workdir):
    rubric = q["rubric"].replace("{truth}", str(t))
    prompt = (
        "あなたは採点者です。次の質問への回答を、採点基準だけで採点して"
        "ください。回答の文章の上手さは問いません。\n\n"
        f"# 質問\n{q['question']}\n\n# 採点基準\n{rubric}\n\n"
        f"# 回答\n{answer}\n\n"
        '出力は JSON 一つだけ: {"pass": true か false, "reason": "一文"}')
    out = claude(prompt, workdir, args.judge_model)
    if out.get("is_error"):
        return {"error": out.get("result", "")[:200]}
    # A judge that reconsiders writes a second object; the last one stands.
    text, decoder, last = out.get("result", ""), json.JSONDecoder(), None
    for i, ch in enumerate(text):
        if ch == "{":
            try:
                obj, _ = decoder.raw_decode(text[i:])
            except ValueError:
                continue
            if isinstance(obj, dict) and "pass" in obj:
                last = obj
    return last or {"error": "unparseable grade: " + text[:200]}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", default="claude-sonnet-5")
    p.add_argument("--judge-model", default="claude-sonnet-5")
    p.add_argument("--repeat", type=int, default=1)
    p.add_argument("--only", help="comma-separated question ids")
    p.add_argument("--resume", help="a results .jsonl to continue: runs it "
                   "already holds without an error are not asked again")
    args = p.parse_args()
    args.project = os.environ["EVAL_PROJECT"]
    url = os.environ.get("OCHAKAI_URL", "http://localhost:8080").rstrip("/")

    questions = json.loads((HERE / "questions.json").read_text())
    if args.only:
        keep = set(args.only.split(","))
        questions = [q for q in questions if q["id"] in keep]

    if args.resume:
        out = pathlib.Path(args.resume)
        rows = [json.loads(line) for line in out.read_text().splitlines() if line]
        rows = [r for r in rows if not r.get("error")]
        out.write_text("".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows))
    else:
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        out = HERE / "results" / f"{stamp}-{args.model}.jsonl"
        out.parent.mkdir(exist_ok=True)
        rows = []
    done_runs = {(r["id"], r["condition"], r["run"]) for r in rows}

    with tempfile.TemporaryDirectory() as workdir:
        mcp_config = pathlib.Path(workdir) / "mcp.json"
        mcp_config.write_text(json.dumps({"mcpServers": {"ochakai": {
            "type": "http", "url": url + "/mcp",
            "headers": {"Ochakai-On-Behalf-Of": "process:demo-eval"}}}}))
        for q in questions:
            todo = [(n, c) for n in range(args.repeat) for c in ("bare", "bundle")
                    if (q["id"], c, n) not in done_runs]
            if not todo:
                continue
            if not warehouse_reachable(args.project):
                raise SystemExit("BigQuery is not reachable (is the gcloud login "
                                 f"current?); continue with --resume {out}")
            t = truth(q["truth_sql"], args.project) if "truth_sql" in q else None
            for n, condition in todo:
                res = ask(q, condition, args, workdir, str(mcp_config))
                answer = res.get("result", "")
                g = ({"error": answer[:200]} if res.get("is_error")
                     else grade(q, answer, t, args, workdir))
                row = {
                    "id": q["id"], "condition": condition, "run": n,
                    "pass": bool(g.get("pass")), "error": g.get("error"),
                    "reason": g.get("reason") or g.get("error"),
                    "truth": t, "model": args.model,
                    "cost_usd": res.get("total_cost_usd"),
                    "seconds": (res.get("duration_ms") or 0) / 1000,
                    "turns": res.get("num_turns"), "answer": answer,
                }
                rows.append(row)
                with out.open("a") as f:
                    f.write(json.dumps(row, ensure_ascii=False) + "\n")
                print(f"{q['id']:26} {condition:6} "
                      f"{'ERR ' if row['error'] else 'PASS' if row['pass'] else 'fail'}  "
                      f"{row['seconds']:6.0f}s  {row['reason']}", flush=True)

    for condition in ("bare", "bundle"):
        # A run that errored (a usage limit, a crash) answered nothing and
        # is left out of the count rather than counted as a failure.
        mine = [r for r in rows if r["condition"] == condition and not r["error"]]
        passed = sum(r["pass"] for r in mine)
        secs = sum(r["seconds"] for r in mine) / max(len(mine), 1)
        cost = sum(r["cost_usd"] or 0 for r in mine)
        errored = sum(1 for r in rows if r["condition"] == condition and r["error"])
        print(f"{condition:6} {passed}/{len(mine)} passed, "
              f"{secs:.0f}s per answer, ${cost:.2f}"
              + (f", {errored} errored" if errored else ""))
    print(f"wrote {out}")


if __name__ == "__main__":
    main()
