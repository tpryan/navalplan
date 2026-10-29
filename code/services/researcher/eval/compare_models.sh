#!/usr/bin/env bash
#
# compare_models.sh -- Compare NavalPlan agent models on the eval suite.
#
# For each model, this starts the researcher service once with
# NAVALPLAN_AGENT_MODEL set, confirms the model the service actually loaded
# (from agent.log), runs the full per-agent eval suite against it while timing
# each `adk eval` call, then tears the service down and moves to the next model.
#
# The agent-under-test model is the ONLY thing that changes between runs: the
# judge model stays whatever each agent's eval/<agent>/test_config.json pins
# (gemini-3.5-flash-lite), so pass/fail and timing deltas are attributable to
# the agent model.
#
# Results (pass/fail + wall-clock timing + the confirmed model name) are written
# to a tagged manifest under eval/results/ in both Markdown and JSON.
#
# Usage (from anywhere; paths resolve to the repo root):
#   bash code/services/researcher/eval/compare_models.sh [model1 model2 ...]
#   make eval-compare                       # defaults to the three models below
#   make eval-compare MODELS="gemini-3.5-flash-lite gemini-3.8-flash gemini-4-argon"
#
# Any number of models (>=1) may be passed; the table scales to all of them and,
# when >=2 are given, adds a "Fastest (x)" column (slowest time / fastest time).
#
# NOTE on timing: each measured interval is one `adk eval` call, which includes
# the agent's model/tool work AND the judge's scoring. The judge is identical
# across models, so the DELTA between models reflects the agent model; absolute
# numbers carry a constant judge overhead. Timing excludes service startup.

set -uo pipefail

# --- Resolve repo root (this script lives at code/services/researcher/eval/) ---
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
cd "$ROOT" || { echo "cannot cd to repo root"; exit 1; }

RES_DIR="code/services/researcher"
HIST="$RES_DIR/.adk/eval_history"
ADK="$([ -f ./venv/bin/adk ] && echo ./venv/bin/adk || echo adk)"
AGENTS="harbourmaster pilot commodore specialist lookout"
MODELS="${*:-gemini-3.5-flash-lite gemini-3.8-flash gemini-4-argon}"

STAMP="$(date +%Y%m%d_%H%M%S)"
OUT_DIR="$RES_DIR/eval/results"
mkdir -p "$OUT_DIR"
RAW="$OUT_DIR/.raw_${STAMP}.tsv"
OUT_MD="$OUT_DIR/compare_${STAMP}.md"
OUT_JSON="$OUT_DIR/compare_${STAMP}.json"
: > "$RAW"

JUDGE="$(grep -o '"judge_model"[^,]*' "$RES_DIR/eval/pilot/test_config.json" 2>/dev/null | head -1 | cut -d'"' -f4)"
[ -z "$JUDGE" ] && JUDGE="(from each test_config.json)"

# --- Parser: count pass/fail + min score for one agent's fresh result files ---
PARSER="$(cat <<'PY'
import sys, json, glob, os
agent, since, hist = sys.argv[1], float(sys.argv[2]), sys.argv[3]
p = f = 0
scores = []
pattern = os.path.join(hist, "researcher_%s_eval_set_*.evalset_result.json" % agent)
for fn in glob.glob(pattern):
    if os.path.getmtime(fn) < since - 2:
        continue
    try:
        d = json.load(open(fn))
    except Exception:
        continue
    for c in d.get("eval_case_results", []):
        st = c.get("final_eval_status")
        if st == 1:
            p += 1
        elif st == 2:
            f += 1
        for m in c.get("overall_eval_metric_results", []):
            s = m.get("score")
            if isinstance(s, (int, float)):
                scores.append(s)
mn = "%.3f" % min(scores) if scores else "NA"
print("%d %d %s" % (p, f, mn))
PY
)"

now_epoch() { python3 -c 'import time; print(time.time())'; }

start_service() {
  local model="$1"
  ln -sf "$ROOT/.env" "$RES_DIR/.env"
  echo "from . import agent" > "$RES_DIR/__init__.py"
  : > "$ROOT/agent.log"
  ( cd "$RES_DIR" && NAVALPLAN_AGENT_MODEL="$model" NAVALPLAN_COMMODORE_MODEL="$model" ./researcher-service ) > "$ROOT/agent.log" 2>&1 &
  for _ in $(seq 1 30); do
    lsof -i :8081 >/dev/null 2>&1 && break
    sleep 1
  done
  if ! lsof -i :8081 >/dev/null 2>&1; then
    echo "  ERROR: service failed to start on :8081 for $model"
    sed -n '1,40p' "$ROOT/agent.log"
    return 1
  fi
  local got
  got="$(grep -o 'config modelName=[^ ]*' "$ROOT/agent.log" | head -1 | cut -d= -f2)"
  [ -z "$got" ] && got="(unknown)"
  printf 'MODELINFO\t%s\t%s\n' "$model" "$got" >> "$RAW"
  echo "  service up; agent.log reports modelName=$got"
  if [ "$got" != "$model" ]; then
    echo "  WARNING: requested '$model' but service reports '$got' -- check .env/env precedence"
  fi
  return 0
}

stop_service() {
  lsof -ti :8081 2>/dev/null | xargs kill -9 2>/dev/null || true
  rm -f "$RES_DIR/.env" "$RES_DIR/__init__.py" "$RES_DIR/agent.py"
  sleep 2   # let the port free before the next model
}

run_agent() {
  local agent="$1"
  local card
  if [ "$agent" = "harbourmaster" ]; then
    card="http://localhost:8081/invoke/agent-card.json"
  else
    card="http://localhost:8081/invoke/$agent/agent-card.json"
  fi
  {
    echo "import os, sys"
    echo "_d = os.path.dirname(os.path.abspath(__file__)); sys.path.insert(0, _d); sys.path.insert(0, os.path.join(_d, os.pardir))"
    echo "from eval_a2a_shim import make_root_agent"
    echo "root_agent = make_root_agent(name='${agent}_agent', agent_card='$card')"
    echo "agent = root_agent"
  } > "$RES_DIR/agent.py"

  local cfg=""
  [ -f "$RES_DIR/eval/$agent/test_config.json" ] && cfg="--config_file_path=$RES_DIR/eval/$agent/test_config.json"

  local t0 t1 elapsed parsed
  t0="$(now_epoch)"
  PYTHONWARNINGS=ignore "$ADK" eval "$RES_DIR" "$RES_DIR/eval/$agent/$agent.test.json" $cfg >/dev/null 2>&1
  t1="$(now_epoch)"
  elapsed="$(python3 -c "print('%.2f' % ($t1 - $t0))")"

  parsed="$(python3 -c "$PARSER" "$agent" "$t0" "$HIST")"
  local pass fail minscore
  pass="$(echo "$parsed" | awk '{print $1}')"
  fail="$(echo "$parsed" | awk '{print $2}')"
  minscore="$(echo "$parsed" | awk '{print $3}')"

  printf 'DATA\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$CURRENT_MODEL" "$agent" "$pass" "$fail" "$elapsed" "$minscore" >> "$RAW"
  printf '    %-14s pass=%s fail=%s time=%ss minScore=%s\n' "$agent" "$pass" "$fail" "$elapsed" "$minscore"
}

echo "=== Building researcher service ==="
(cd "$RES_DIR" && go build -mod=vendor -o researcher-service .) || { echo "build failed"; exit 1; }

for CURRENT_MODEL in $MODELS; do
  echo ""
  echo "=== MODEL: $CURRENT_MODEL ==="
  if ! start_service "$CURRENT_MODEL"; then
    stop_service
    echo "  skipping $CURRENT_MODEL (service did not start)"
    continue
  fi
  for a in $AGENTS; do
    run_agent "$a"
  done
  # Preserve this model's service log (the agent's actual errors/timeouts live
  # here) before the next model truncates it.
  msafe="$(echo "$CURRENT_MODEL" | tr -c 'A-Za-z0-9._-' '_')"
  logdest="$OUT_DIR/agent_${msafe}_${STAMP}.log"
  cp "$ROOT/agent.log" "$logdest" 2>/dev/null && echo "  saved service log -> $logdest"
  errhits="$(grep -icE 'error|timeout|deadline|panic|context canceled|failed' "$logdest" 2>/dev/null || echo 0)"
  [ "${errhits:-0}" -gt 0 ] && echo "  NOTE: $errhits error/timeout-ish line(s) in log -- grep it for detail"
  stop_service
done

rm -f "$ROOT/agent.log" "$ROOT/agent.pid"

# --- Format the comparison table (Markdown + JSON) ---
FORMATTER="$(cat <<'PY'
import sys, json
raw, out_md, out_json, models_csv, stamp, judge = sys.argv[1:7]
models = models_csv.split(",")
agents = ["harbourmaster", "pilot", "commodore", "specialist", "lookout"]

data = {}       # (model, agent) -> dict
reported = {}   # model -> reported modelName
for line in open(raw):
    parts = line.rstrip("\n").split("\t")
    if parts[0] == "MODELINFO":
        reported[parts[1]] = parts[2]
    elif parts[0] == "DATA":
        _, model, agent, p, f, t, mn = parts
        data[(model, agent)] = {
            "pass": int(p), "fail": int(f),
            "cases": int(p) + int(f), "time": float(t),
            "min_score": mn,
        }

def cell(model, agent, key, default="-"):
    d = data.get((model, agent))
    return d[key] if d else default

lines = []
lines.append("# Model comparison -- NavalPlan eval suite")
lines.append("")
lines.append("- Run: `%s`" % stamp)
lines.append("- Judge model (fixed): `%s`" % judge)
for m in models:
    lines.append("- Agent model `%s` -> service reported `%s`" % (m, reported.get(m, "?")))
lines.append("")
lines.append("_Timing is wall-clock per `adk eval` call (agent work + fixed judge scoring); "
             "startup excluded. Faster(x) = slower model's time / faster model's time._")
lines.append("")

# header
hdr = ["Agent", "Cases"]
for m in models:
    hdr += ["%s pass" % m, "%s time (s)" % m]
if len(models) >= 2:
    hdr.append("Fastest (x)")
lines.append("| " + " | ".join(hdr) + " |")
lines.append("|" + "|".join(["---"] * len(hdr)) + "|")

def faster_col(times):
    vals = [(m, t) for m, t in times if t is not None]
    if len(vals) < 2:
        return "-"
    vals.sort(key=lambda x: x[1])
    fast_m, fast_t = vals[0]
    slow_m, slow_t = vals[-1]
    if fast_t <= 0:
        return "-"
    return "%s %.2fx" % (fast_m, slow_t / fast_t)

totals = {m: {"pass": 0, "cases": 0, "time": 0.0} for m in models}
for agent in agents:
    cases = next((cell(m, agent, "cases") for m in models if data.get((m, agent))), "-")
    row = [agent, str(cases)]
    times = []
    for m in models:
        d = data.get((m, agent))
        if d:
            row += ["%d/%d" % (d["pass"], d["cases"]), "%.2f" % d["time"]]
            times.append((m, d["time"]))
            totals[m]["pass"] += d["pass"]
            totals[m]["cases"] += d["cases"]
            totals[m]["time"] += d["time"]
        else:
            row += ["-", "-"]
            times.append((m, None))
    if len(models) >= 2:
        row.append(faster_col(times))
    lines.append("| " + " | ".join(row) + " |")

# totals row
trow = ["**TOTAL**", str(max((totals[m]["cases"] for m in models), default=0))]
ttimes = []
for m in models:
    trow += ["%d/%d" % (totals[m]["pass"], totals[m]["cases"]), "%.2f" % totals[m]["time"]]
    ttimes.append((m, totals[m]["time"]))
if len(models) >= 2:
    trow.append(faster_col(ttimes))
lines.append("| " + " | ".join(trow) + " |")

md = "\n".join(lines) + "\n"
open(out_md, "w").write(md)

out = {
    "run": stamp, "judge_model": judge,
    "models": models, "reported_model_names": reported,
    "agents": {},
    "totals": totals,
}
for agent in agents:
    out["agents"][agent] = {
        m: data.get((m, agent)) for m in models
    }
json.dump(out, open(out_json, "w"), indent=2)
print(md)
PY
)"

MODELS_CSV="$(echo "$MODELS" | tr ' ' ',')"
python3 -c "$FORMATTER" "$RAW" "$OUT_MD" "$OUT_JSON" "$MODELS_CSV" "$STAMP" "$JUDGE"

echo ""
echo "Wrote:"
echo "  $OUT_MD"
echo "  $OUT_JSON"
