"""Demo data for make dev: the group mapping team-red -> member for the users of
hack/dex/config.yaml, and three projects with tickets in every state, questions, comments,
links, interest, progress and time, so the UI has something to show while it is built.

Usage: dev_demo.py <base-url> <token> <team>. It writes through the API like any client —
every act is an audit row and an event — and adds no project when the team has one
already. Some acts carry an X-Cowork-Agent header, so the activity shows an agent at work.
A mapping is made by a browser session only (docs/adr/0035 D5), so the script logs in as the
local administrator of make dev for it: COWORK_DEV_ADMIN, COWORK_DEV_ADMIN_PASSWORD and
COWORK_BASE_URL, the origin the login and the CSRF check expect (docs/adr/0037)."""

import datetime
import json
import os
import sys
import urllib.error
import urllib.request
import uuid

base, token, team = sys.argv[1], sys.argv[2], sys.argv[3]
AGENT = "claude/opus-5.5/demo"
T = f"/api/v1/teams/{team}"


def call(method, path, body=None, agent=False, etag=None, expect=(200, 201, 204)):
    req = urllib.request.Request(base + path, method=method)
    req.add_header("Authorization", "Bearer " + token)
    if agent:
        req.add_header("X-Cowork-Agent", AGENT)
    if method == "POST":
        req.add_header("Idempotency-Key", str(uuid.uuid4()))
    if etag:
        req.add_header("If-Match", etag)
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, data) as res:
            status, raw, headers = res.status, res.read(), res.headers
    except urllib.error.HTTPError as err:
        status, raw, headers = err.code, err.read(), err.headers
    if status not in expect:
        sys.exit(f"dev-demo: {method} {path} answered {status}: {raw[:400]!r}")
    return (json.loads(raw) if raw else None), headers


def get(path):
    return call("GET", path)[0]


# The mapping comes before the projects, so that a team seeded before the identity provider
# gains it too; a group the team maps already keeps its mapping, whatever the UI made of it
# (docs/adr/0030 D2).
def session_call(method, path, body=None, cookie=None, expect=(200, 201, 204)):
    req = urllib.request.Request(base + path, method=method)
    req.add_header("Origin", os.environ["COWORK_BASE_URL"])
    req.add_header("X-Requested-With", "cowork")
    if cookie:
        req.add_header("Cookie", cookie)
    if method == "POST" and path.startswith("/api/"):
        req.add_header("Idempotency-Key", str(uuid.uuid4()))
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, data) as res:
            status, raw, headers = res.status, res.read(), res.headers
    except urllib.error.HTTPError as err:
        status, raw, headers = err.code, err.read(), err.headers
    if status not in expect:
        sys.exit(f"dev-demo: {method} {path} answered {status}: {raw[:400]!r}")
    return (json.loads(raw) if raw else None), headers


login = {"username": os.environ["COWORK_DEV_ADMIN"], "password": os.environ["COWORK_DEV_ADMIN_PASSWORD"]}
_, headers = session_call("POST", "/auth/local", login)
session = next(c.split(";", 1)[0] for c in headers.get_all("Set-Cookie") if c.startswith("__Host-cowork-session="))
mapping, _ = session_call("POST", f"{T}/group-mappings", {"group": "team-red", "role": "member"}, session, expect=(201, 409))
session_call("POST", "/auth/logout", cookie=session, expect=(200, 204))
if "code" in mapping and mapping["code"] != "mapping_exists":
    sys.exit(f"dev-demo: mapping team-red answered {mapping}")
print("demo data: the group team-red maps to member" + (" (it did already)" if "code" in mapping else ""))

if get(f"{T}/projects")["items"]:
    print("demo data: the team has projects already, no more to add")
    sys.exit(0)

me = get("/api/v1/me")
members = get(f"{T}/members")["items"]
sam = next((m["person"]["id"] for m in members if m["person"].get("username") == "sam"), None)


def project(key, name, description):
    call("POST", f"{T}/projects", {"key": key, "name": name, "description": description})
    return key


def ticket(key, title, type="task", severity="medium", security="none", effort="M", body="",
           assignee=None, parent=None, threat=None, agent=False):
    payload = {"type": type, "title": title, "severity": severity, "security": security,
               "effort": effort, "body": body}
    if assignee:
        payload["assignee"] = assignee
    if parent:
        payload["parent"] = parent
    if threat:
        payload["threat"] = threat
    created, _ = call("POST", f"{T}/projects/{key}/tickets", payload, agent=agent)
    return key, created["number"]


def path(t):
    return f"{T}/projects/{t[0]}/tickets/{t[1]}"


def patch(t, **fields):
    _, headers = call("GET", path(t))
    call("PATCH", path(t), fields, etag=headers["ETag"])


FORWARD = ["filed", "analysed", "decided", "in-progress", "review", "done"]


def advance(t, to, note="Verified with make test and by hand in the UI.", agent=False):
    state = get(path(t))["state"]
    while state != to:
        nxt = FORWARD[FORWARD.index(state) + 1]
        body = {"from": state, "to": nxt}
        if nxt == "done":
            body["note"] = note
        call("POST", f"{path(t)}/transitions", body, agent=agent)
        state = nxt


def horizon(t, value, reason=None):
    """A ticket's horizon (docs/adr/0010 D3): the board shows now and release by state, next beside them."""
    _, headers = call("GET", path(t))
    body = {"value": value}
    if reason:
        body["reason"] = reason
    call("PUT", f"{path(t)}/horizon", body, etag=headers["ETag"])


def block(t, kind, reason, on=None):
    state = get(path(t))["state"]
    blockset = {"kind": kind}
    if on:
        blockset["ticket"] = on
    call("POST", f"{path(t)}/transitions", {"from": state, "to": "blocked", "reason": reason, "block": blockset})


def drop(t, reason):
    state = get(path(t))["state"]
    call("POST", f"{path(t)}/transitions", {"from": state, "to": "dropped", "reason": reason})


def comment(t, text, agent=False):
    call("POST", f"{path(t)}/comments", {"body": text}, agent=agent)


def question(t, text, options="", recommendation="", asked_of=None, agent=False):
    body = {"question": text, "options": options, "recommendation": recommendation}
    if asked_of:
        body["asked_of"] = asked_of
    created, _ = call("POST", f"{path(t)}/questions", body, agent=agent)
    return created["number"]


def link(src, kind, dst):
    call("PUT", f"{path(src)}/links/{kind}/{dst[0]}-{dst[1]}")


def interest(t, weight, note=""):
    call("PUT", f"{path(t)}/interest", {"weight": weight, "note": note})


def book(t, minutes, days_ago, note):
    day = (datetime.date.today() - datetime.timedelta(days=days_ago)).isoformat()
    call("POST", f"{path(t)}/time-entries", {"day": day, "minutes": minutes, "note": note})


cow = project("COW", "cowork", "The backlog of cowork itself: the first UI release.")
ops = project("OPS", "Operations", "Cluster, releases and the things that keep an installation running.")
web = project("WEB", "Website", "The public site and the documentation portal.")

login = ticket(cow, "The UI cannot log in without a token in the browser", "feature", "high", effort="L",
               body="Sessions in an httpOnly cookie, the local administrator from a Secret, local accounts and CSRF, before the UI ships.",
               assignee=me["id"])
shell = ticket(cow, "An app shell with the logo, the theme and dark mode", "feature", "medium", effort="M",
               body="PrimeNG with the cowork preset, the gradient logo and the three-way theme preference.",
               assignee=me["id"], agent=True)
backlog = ticket(cow, "A ranked backlog with drag order and the score marker", "feature", "high", effort="L",
                 body="Rank is the decision, score is the warning: the backlog orders by rank and marks where the score disagrees.")
board = ticket(cow, "A project board with drag between the states", "feature", "medium", effort="M")
team_board = ticket(cow, "A team board with one swimlane per project", "feature", "medium", effort="M")
dashboard = ticket(cow, "The fixed dashboard of nine tiles", "feature", "low", effort="L")
inbox = ticket(cow, "An in-app inbox per person", "feature", "medium", effort="M", assignee=sam)
search = ticket(cow, "Search across the person's teams", "feature", "low", effort="S")
flicker = ticket(cow, "The board flickers when an event arrives during a drag", "bug", "medium", effort="S",
                 body="Steps: start a drag, let another person move a card. The dragged card jumps back.")
preset = ticket(cow, "Which preset: Aura, Lara or Nora?", "decision", "low", effort="XS")
toasts = ticket(cow, "Problem details as toasts and field errors", "task", "medium", effort="S", parent=f"{shell[0]}-{shell[1]}")
theme = ticket(cow, "The theme follows the system and updates live", "task", "low", effort="XS", parent=f"{shell[0]}-{shell[1]}")
gantt = ticket(cow, "A Gantt view of the plan", "feature", "cosmetic", effort="L")

advance(shell, "in-progress", agent=True)
advance(theme, "done")
advance(toasts, "in-progress")
patch(toasts, progress_refinement=100, progress=25)
advance(login, "decided")
patch(login, progress_refinement=100)
advance(backlog, "analysed")
patch(backlog, progress_refinement=60)
advance(inbox, "review")
patch(inbox, progress_refinement=100, progress=100, progress_review=40)
advance(board, "decided")
patch(board, progress_refinement=100)
block(board, "ticket", "The board needs the rank of the backlog first.", on=f"{backlog[0]}-{backlog[1]}")
advance(flicker, "analysed")
drop(gantt, "Not in the first release (docs/adr/0018 D8).")
advance(search, "done", note="Searched three teams for a word in a comment; every hit named its team.")
for t in (login, shell, toasts, inbox, board):
    horizon(t, "now")
horizon(backlog, "next", "The board needs it first.")
horizon(flicker, "next")
horizon(preset, "next")

link(login, "blocks", backlog)
link(flicker, "found-in", board)
link(dashboard, "relates-to", team_board)
question(preset, "Which PrimeNG preset should cowork build on?",
         options="Aura (the default, crisp), Lara (rounder, more contrast), Nora (dense, square)",
         recommendation="Aura: the default ADR 0052 names, and the cowork palette sits on top of it.",
         asked_of=me["id"], agent=True)
question(backlog, "Does a drag in a filtered backlog move rank among all tickets or only the visible ones?",
         options="Among all tickets; only among the visible ones",
         recommendation="Among all tickets, between the visible neighbours.", agent=True)
comment(shell, "The dark scheme comes first; the light one has to work with the same tokens.")
comment(shell, "Logo and favicon drafted in three variants for review.", agent=True)
comment(flicker, "Reproduced twice in Safari, not in Chromium.")
interest(backlog, "need", "The daily work starts in the backlog.")
interest(dashboard, "watch")
book(shell, 90, 1, "Preset and theme service")
book(shell, 45, 0, "Logo variants")
book(login, 120, 2, "Reading the session and CSRF records")

secrets = ticket(ops, "The session key is logged at debug level", "bug", "high", "live", "S",
                 threat="Anyone who reads the backend log can forge a session cookie; operators rely on the log being harmless.",
                 assignee=me["id"])
rotate = ticket(ops, "Rotate the storage key without downtime", "task", "medium", "hardening", "M",
                threat="A leaked storage key stays usable until someone notices.")
quota = ticket(ops, "A team can fill the bucket", "bug", "medium", "boundary", "M",
               threat="A member of team A uploads until the shared bucket is full and team B cannot attach a file.")
backup = ticket(ops, "Document the restore of a backup", "task", "low", effort="S", assignee=sam)
upgrade = ticket(ops, "Upgrade PostgreSQL to 18.1", "task", "low", effort="XS")
advance(secrets, "in-progress")
patch(secrets, progress_refinement=100, progress=50)
horizon(secrets, "now", "A live finding.")
advance(quota, "decided")
patch(quota, progress_refinement=100)
horizon(quota, "next")
advance(upgrade, "done", note="Ran the integration tier against 18.1: green.")
block(backup, "human", "Waiting for the operations team to name the backup tool.")
question(rotate, "Two keys at once, or a maintenance window?",
         options="Two keys accepted during a grace period; a short maintenance window",
         recommendation="Two keys: no downtime and the same mechanism the session key uses.")
book(secrets, 30, 0, "Found the log line")

hero = ticket(web, "The landing page explains cowork in one screen", "feature", "medium", effort="M")
docs_portal = ticket(web, "Publish the documentation as a portal", "feature", "low", effort="L")
typo = ticket(web, "Typo on the pricing page", "bug", "cosmetic", effort="XS")
advance(hero, "analysed")
horizon(hero, "next")
advance(typo, "done", note="Checked the page in two browsers.")
link(docs_portal, "relates-to", hero)

print(f"demo data: 3 projects and {13 + 5 + 3} tickets in the team {team}")
