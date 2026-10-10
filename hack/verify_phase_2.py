"""The API half of hack/verify-phase-2.sh: an agent creates a project, files two
tickets, asks a question, links the tickets and moves one, then the team's
audit view must show each act with the seeded person, the token, the agent
mark and the token's capabilities. The first failed assertion stops it."""

import json
import sys
import time
import urllib.request
import uuid

base, token, team = sys.argv[1], sys.argv[2], sys.argv[3]
agent = f"verify-phase-2/script/{int(time.time())}"


def call(method, path, body=None, keyed=False, expect=(200, 201, 204)):
    req = urllib.request.Request(base + path, method=method)
    req.add_header("Authorization", "Bearer " + token)
    req.add_header("X-Cowork-Agent", agent)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        req.add_header("Content-Type", "application/json")
    if keyed:
        req.add_header("Idempotency-Key", str(uuid.uuid4()))
    try:
        with urllib.request.urlopen(req, data) as res:
            status, raw = res.status, res.read()
    except urllib.error.HTTPError as err:
        status, raw = err.code, err.read()
    if status not in expect:
        sys.exit(f"FAIL {method} {path}: {status} {raw[:400]!r}")
    print(f"ok   {method} {path} -> {status}")
    return json.loads(raw) if raw else None


def check(condition, message):
    if not condition:
        sys.exit("FAIL " + message)
    print("ok   " + message)


me = call("GET", "/api/v1/me")
tokens = call("GET", "/api/v1/me/tokens")["items"]
check(len(tokens) == 1, "the seeded person holds the one seeded token")
seeded = tokens[0]
check(seeded["agent"], "the seeded token is an agent token")

t = f"/api/v1/teams/{team}"
key = "V" + uuid.uuid4().hex[:6].upper()
call("POST", f"{t}/projects", {"key": key, "name": "Verification"}, keyed=True)
first = call("POST", f"{t}/projects/{key}/tickets",
             {"type": "task", "title": "First", "severity": "low", "security": "none", "effort": "S"}, keyed=True)
second = call("POST", f"{t}/projects/{key}/tickets",
              {"type": "decision", "title": "Second", "severity": "low", "security": "none", "effort": "S"}, keyed=True)
call("POST", f"{t}/projects/{key}/tickets/{first['number']}/questions", {"question": "Ready?"}, keyed=True)
call("PUT", f"{t}/projects/{key}/tickets/{second['number']}/links/blocks/{key}-{first['number']}")
moved = call("POST", f"{t}/projects/{key}/tickets/{first['number']}/transitions", {"from": "filed", "to": "analysed"})
check(moved["state"] == "analysed", "the transition moved the ticket")
check(moved["urgency"] == "icebox", "an open decision that blocks it makes it icebox (rule set v1)")

audit = call("GET", f"{t}/audit?limit=200")["items"]
expected = [("project", "created"), ("ticket", "created"), ("ticket", "created"), ("question", "asked"),
            ("link", "linked"), ("link", "linked"), ("ticket", "transitioned")]
for entity, action in expected:
    rows = [r for r in audit if r["entity_type"] == entity and r["action"] == action]
    check(rows, f"the audit view holds {entity} {action}")
    for row in rows:
        person = (row.get("actor") or {}).get("person") or {}
        check(person.get("id") == me["id"], f"{entity} {action}: the actor is the seeded person")
        check(row.get("token_id") == seeded["id"], f"{entity} {action}: the token is the seeded one")
        check(row.get("agent") == agent, f"{entity} {action}: the agent mark is the header")
        check(sorted(row.get("agent_capabilities") or []) == sorted(seeded["capabilities"]),
              f"{entity} {action}: the capabilities are the token's")
print("phase 2 verified")
