"""
Builds architecture/excalidraw.json: one frame holding, side by
side, how Envi fits together, every program and package, the data model as a
crow's foot diagram, and every API endpoint.

    make diagram

The endpoint cards are read from architecture/ENDPOINTS.md, so after adding, removing or
changing an endpoint, update ENDPOINTS.md and run this again. The data model is
read from migrations/schema.sql, so after a schema change, update schema.sql
and run this again. The services are read from cmd/ and internal/: each card
takes the package comment's first sentence, and works out the tables, packages
and outside systems from the code. The architecture
half is laid out by hand below; edit it here when the deployment changes.
internal/api/endpoints_test.go fails when excalidraw.json falls behind the
router, the schema or the packages.

Open the result at excalidraw.com (Open, or drag the file onto the canvas).
"""

import json
import os
import random
import re

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Fixed seed, so running this twice on the same input gives the same file and
# the diff only shows real changes.
random.seed(7)

YELLOW = "#f5e73f"
GREY = "#495057"
PURPLE = "#6741d9"
HAND = 5   # Excalifont, the hand-drawn one
MONO = 3   # Cascadia, so the endpoint columns line up

els = []
by_id = {}
# Where each section's elements begin, so the sections can be laid out side
# by side once they are all drawn.
SECTIONS = [0]


def seed():
    return random.randint(1, 2**31)


def base(kind, x, y, w, h, **kw):
    e = dict(id=f"{kind[:3]}{len(els)}", type=kind, x=x, y=y, width=w, height=h, angle=0,
             strokeColor="#1e1e1e", backgroundColor="transparent", fillStyle="solid", strokeWidth=2,
             strokeStyle="solid", roughness=1, opacity=100, groupIds=[], frameId=None, roundness=None,
             seed=seed(), version=1, versionNonce=seed(), isDeleted=False, boundElements=[],
             updated=1, link=None, locked=False)
    e.update(kw)
    els.append(e)
    by_id[e["id"]] = e
    return e


def measure(s, size, family):
    lines = s.split("\n")
    per_char = 0.6 if family == MONO else 0.55
    return max(len(l) for l in lines) * size * per_char, len(lines) * size * 1.25


def text(x, y, s, size=20, color="#1e1e1e", family=HAND, align="left", container=None, group=None):
    w, h = measure(s, size, family)
    if container:
        c = by_id[container]
        x, y = c["x"] + (c["width"] - w) / 2, c["y"] + (c["height"] - h) / 2
    t = base("text", x, y, w, h, text=s, originalText=s, fontSize=size, fontFamily=family,
             textAlign=align, verticalAlign="middle" if container else "top", containerId=container,
             autoResize=True, lineHeight=1.25, strokeColor=color, strokeWidth=1, boundElements=None,
             groupIds=[group] if group else [])
    if container:
        by_id[container]["boundElements"].append({"id": t["id"], "type": "text"})
    return t


def box(x, y, w, h, label, bg="#ffffff", shape="rectangle", size=18, **kw):
    b = base(shape, x, y, w, h, backgroundColor=bg,
             roundness={"type": 3} if shape == "rectangle" else {"type": 2}, **kw)
    if label:
        text(0, 0, label, size, align="center", container=b["id"])
    return b


def frame(x, y, w, h, title, bg, dashed=False, size=22):
    f = base("rectangle", x, y, w, h, backgroundColor=bg, roundness={"type": 3},
             strokeStyle="dashed" if dashed else "solid")
    text(x + 20, y + 14, title, size)
    return f


def arrow(points, label=None, start=None, end=None, both=False, color="#1e1e1e", dashed=False,
          heads=None, width=2):
    x0, y0 = points[0]
    rel = [[px - x0, py - y0] for px, py in points]
    xs, ys = [p[0] for p in rel], [p[1] for p in rel]
    a = base("arrow", x0, y0, max(xs) - min(xs), max(ys) - min(ys), points=rel,
             startArrowhead=heads[0] if heads else ("arrow" if both else None),
             endArrowhead=heads[1] if heads else "arrow", lastCommittedPoint=None, strokeWidth=width,
             startBinding={"elementId": start["id"], "focus": 0, "gap": 4} if start else None,
             endBinding={"elementId": end["id"], "focus": 0, "gap": 4} if end else None,
             strokeColor=color, strokeStyle="dashed" if dashed else "solid", elbowed=False)
    for s in (start, end):
        if s:
            s["boundElements"].append({"id": a["id"], "type": "arrow"})
    if label:
        # On the longest segment, so a label never sits on a corner.
        segs = list(zip(points, points[1:]))
        (ax, ay), (bx, by) = max(segs, key=lambda s: abs(s[1][0] - s[0][0]) + abs(s[1][1] - s[0][1]))
        w, h = measure(label, 16, HAND)
        t = base("text", (ax + bx) / 2 - w / 2, (ay + by) / 2 - h / 2, w, h, text=label,
                 originalText=label, fontSize=16, fontFamily=HAND, textAlign="center",
                 verticalAlign="middle", containerId=a["id"], autoResize=True, lineHeight=1.25,
                 strokeColor=color, strokeWidth=1, boundElements=None)
        a["boundElements"].append({"id": t["id"], "type": "text"})
    return a


# --- endpoints, from ENDPOINTS.md -------------------------------------------

def read_endpoints():
    src = open(os.path.join(ROOT, "architecture", "ENDPOINTS.md")).read()
    groups = []
    for sec in re.split(r"\n## ", src)[1:]:
        title = sec.split("\n", 1)[0].strip()
        rows = []
        for line in sec.splitlines():
            m = re.match(r"\| (GET|POST|PUT|PATCH|DELETE) \| `([^`]+)` \| ([^|]+) \| ([^|]+) \|", line)
            if m:
                given = m.group(4).strip().replace("`", "")
                rows.append((m.group(1), m.group(2), m.group(3).strip(), "" if given == "none" else given))
        if rows:
            source = re.search(r"Source: `([^`]+)`", sec)
            groups.append((title, source.group(1) if source else "", rows))
    return groups


ENDPOINTS = read_endpoints()
TOTAL = sum(len(rows) for _, _, rows in ENDPOINTS)


# --- part 1: architecture ---------------------------------------------------

text(0, -120, "Envi: how it fits together", 36)
text(0, -66, "DNS on Cloudflare: envisecrets.com · docs. · api. · install.", 18, GREY)

text(0, 60, "WHO USES IT", 20, GREY)
browser = box(0, 120, 240, 90, "Browser\ndevelopers, teammates", "#e7f5ff")
ci = box(0, 330, 240, 90, "CI and servers\nenvi run + service token", "#e7f5ff")
term = box(0, 560, 240, 100, "Terminal\nenvi CLI", "#e7f5ff")

frame(340, 40, 960, 860, "Contabo VPS (France) · Coolify", "#f8f9fa", dashed=True)
box(370, 110, 120, 760, None, "#e9ecef")
text(384, 720, "Coolify\nproxy\n(Traefik)\n\nTLS,\nroutes by\ndomain", 16, GREY)

frame(540, 100, 330, 330, "Frontend · Next.js", "#d0ebff", size=20)
docs = box(560, 150, 290, 90, "Docs\ndocs.envisecrets.com :3000")
dash = box(560, 270, 290, 130, "Dashboard + landing\nenvisecrets.com :3000\n/api/envi proxy\nholds the session")

frame(540, 470, 330, 380, "Backend · one container", "#fff9db", size=20)
api = box(560, 520, 290, 140, f"envi-api :8080\napi.envisecrets.com\n{TOTAL} endpoints, see below\nhourly billing job", YELLOW)
inst = box(560, 700, 290, 110, "envi-install :8081\ninstall.envisecrets.com\nserves install.sh / .ps1")

frame(940, 300, 320, 460, "Data", "#d3f9d8", size=20)
pg = box(965, 350, 270, 150, "Postgres :5432\nusers, projects,\nencrypted secrets", shape="ellipse")
rd = box(965, 560, 270, 150, "Redis :6379\nlogin codes,\nrate limits", shape="ellipse")

text(1360, 60, "OUTSIDE SERVICES", 20, GREY)
bachs = box(1360, 120, 260, 100, "Bachs\npayments (USD / NGN)", "#e5dbff")
resend = box(1360, 860, 260, 90, "Resend\nemail", "#e5dbff")
gh = box(0, 1000, 520, 120, "GitHub\nActions: test, then deploy\nReleases: CLI binaries", "#e5dbff")

arrow([(244, 150), (556, 195)], start=browser, end=docs)
arrow([(244, 185), (556, 335)], "HTTPS", start=browser, end=dash)
arrow([(705, 404), (705, 516)], "server-side,\nbearer token", start=dash, end=api)
arrow([(244, 375), (556, 550)], "service token", start=ci, end=api)
arrow([(244, 595), (556, 605)], "API calls", start=term, end=api)
arrow([(244, 635), (556, 750)], "curl | sh", start=term, end=inst)
arrow([(854, 575), (961, 430)], "SQL", start=api, end=pg, both=True)
arrow([(854, 625), (961, 630)], start=api, end=rd, both=True)
arrow([(854, 535), (905, 535), (905, 170), (1356, 170)], "checkout, charges\nwebhooks back",
      start=api, end=bachs, both=True, color=PURPLE)
arrow([(854, 645), (905, 645), (905, 905), (1356, 905)], "login codes, invites,\nwelcome, announcements",
      start=api, end=resend, color=PURPLE)
arrow([(120, 996), (120, 664)], "downloads\nbinary", start=gh, end=term, color=PURPLE)
arrow([(524, 1040), (700, 1040), (700, 904)], "deploy after\ntests pass", start=gh, color=PURPLE, dashed=True)


# --- part 2: endpoints ------------------------------------------------------

SECTIONS.append(len(els))

TOP = 1260
text(0, TOP, f"API endpoints · {TOTAL} · api.envisecrets.com", 36)
text(0, TOP + 56,
     "none = public    Auth = session or service token    Owner = Auth + owns personal org\n"
     "Operator = ENVI_ADMINS or operators table (404 on refusal)\n"
     "Manage = operator role admin    Announce = admin or announcer    Signature = Bachs HMAC webhook",
     16, GREY, MONO)

COLOURS = ["#a5d8ff", "#b2f2bb", "#ffec99", "#ffc9c9", "#d0bfff", "#99e9f2", "#ffd8a8",
           "#eebefa", "#c3fae8", "#e9ecef", "#fcc2d7", "#d8f5a2", "#bac8ff"]
PAD = 24

cards = []
for i, (title, source, rows) in enumerate(ENDPOINTS):
    mw = max(len(r[0]) for r in rows)
    pw = max(len(r[1]) for r in rows)
    lines = []
    for method, path, guard, given in rows:
        line = f"{method:<{mw}}  {path:<{pw}}  [{guard}]"
        if given:
            line += f"\n{'':<{mw}}  ↳ {given}"
        lines.append(line)
    body = "\n".join(lines)
    w = max(measure(body, 14, MONO)[0], measure(title, 22, HAND)[0], measure(source, 13, MONO)[0]) + 2 * PAD
    h = PAD + 22 * 1.25 + 6 + 13 * 1.25 + 14 + measure(body, 14, MONO)[1] + PAD
    cards.append((title, source, body, w, h, COLOURS[i % len(COLOURS)]))

# Three columns, each card going to the shortest one.
heights, widths, columns = [0, 0, 0], [0, 0, 0], [[], [], []]
for card in cards:
    k = heights.index(min(heights))
    columns[k].append((heights[k], card))
    heights[k] += card[4] + 40
    widths[k] = max(widths[k], card[3])

x = 0
for k in range(3):
    for y, (title, source, body, w, h, colour) in columns[k]:
        y += TOP + 200
        group = f"card{k}{int(y)}"
        base("rectangle", x, y, widths[k], h, backgroundColor=colour, roundness={"type": 3},
             groupIds=[group], roughness=0)
        text(x + PAD, y + PAD, title, 22, group=group)
        text(x + PAD, y + PAD + 22 * 1.25 + 6, source, 13, GREY, MONO, group=group)
        text(x + PAD, y + PAD + 22 * 1.25 + 6 + 13 * 1.25 + 14, body, 14, family=MONO, group=group)
    x += widths[k] + 40


# --- part 3: data model, from migrations/schema.sql -------------------------

SECTIONS.append(len(els))

def read_schema():
    """Tables, their columns and their foreign keys, from schema.sql.

    Reads only the shapes this schema uses: one column per line, inline
    REFERENCES, table-level PRIMARY KEY (...), and ALTER TABLE ... FOREIGN KEY.
    """
    src = open(os.path.join(ROOT, "migrations", "schema.sql")).read()
    src = re.sub(r"--[^\n]*", "", src)
    tables, fks = {}, []
    for name, body in re.findall(r"CREATE TABLE (\w+) \((.*?)\n\);", src, re.S):
        cols = []
        pk = set()
        for line in body.split("\n"):
            line = line.strip().rstrip(",")
            if not line:
                continue
            m = re.match(r"PRIMARY KEY \(([^)]+)\)", line)
            if m:
                pk |= {c.strip() for c in m.group(1).split(",")}
                continue
            if re.match(r"(UNIQUE|CHECK)\b", line):
                continue
            col, kind = line.split()[:2]
            ref = re.search(r"REFERENCES (\w+)\(", line)
            cols.append({"name": col, "type": kind, "pk": "PRIMARY KEY" in line, "fk": bool(ref),
                         "unique": " UNIQUE" in line, "null": "NOT NULL" not in line and "PRIMARY KEY" not in line})
            if ref:
                fks.append((name, col, ref.group(1)))
        for c in cols:
            if c["name"] in pk:
                c["pk"], c["null"] = True, False
        tables[name] = {"cols": cols, "solo_pk": pk if len(pk) == 1 else set()}
    for table, col, parent in re.findall(
            r"ALTER TABLE (\w+) ADD CONSTRAINT \w+\s+FOREIGN KEY \((\w+)\) REFERENCES (\w+)\(", src):
        fks.append((table, col, parent))
        for c in tables[table]["cols"]:
            if c["name"] == col:
                c["fk"] = True
    return tables, fks


TABLES, FKS = read_schema()
SHORT = {"timestamptz": "timestamptz", "integer": "int", "boolean": "bool", "bigint": "bigint"}

# Where each table sits: columns left to right, top to bottom within each.
# Arranged by area so most lines stay short. A table missing here still gets
# drawn, at the end of the last column, so a new one never silently vanishes.
LAYOUT = [
    ["device_authorizations", "sessions", "api_tokens"],
    ["users", "memberships", "operators", "announcements", "expenses"],
    ["organizations", "audit_events", "billable_members"],
    ["projects", "invitations", "access_grants", "service_identities"],
    ["environments", "secrets", "secret_versions", "kv_entries"],
    ["billing_accounts", "payg_charges", "bachs_events"],
]
placed = {t for column in LAYOUT for t in column}
LAYOUT[-1] += [t for t in TABLES if t not in placed]

ERD_TOP = TOP + 200 + max(heights) + 160
text(0, ERD_TOP, f"Data model · {len(TABLES)} tables · migrations/schema.sql", 36)
text(0, ERD_TOP + 56,
     "PK primary key   FK foreign key   UQ unique   ? nullable\n"
     "Crow's foot = many, bar = one. Dashed line = the reference is optional (nullable).",
     16, GREY, MONO)

AREA = {"users": "#a5d8ff", "organizations": "#a5d8ff", "memberships": "#a5d8ff",
        "projects": "#b2f2bb", "environments": "#b2f2bb", "secrets": "#b2f2bb",
        "secret_versions": "#b2f2bb", "kv_entries": "#b2f2bb",
        "access_grants": "#ffec99", "invitations": "#ffec99",
        "sessions": "#ffc9c9", "api_tokens": "#ffc9c9", "service_identities": "#ffc9c9",
        "device_authorizations": "#ffc9c9",
        "billing_accounts": "#d0bfff", "billable_members": "#d0bfff", "payg_charges": "#d0bfff",
        "bachs_events": "#d0bfff"}

# Lines run only through the channels between tables, never across one. Each
# column has a gutter on either side; a link between neighbouring columns
# turns once inside the gutter between them, and a link between distant
# columns climbs to a corridor above all the tables, crosses there, and drops
# down the gutter beside its target. Every line gets its own lane, so no two
# lines ever share a stretch.

where = {name: (k, i) for k, column in enumerate(LAYOUT) for i, name in enumerate(column)}

# Size each table first; positions come once the gutters are sized.
size = {}
for name in TABLES:
    cols = TABLES[name]["cols"]
    nw = max(len(c["name"]) for c in cols)
    lines = []
    for c in cols:
        key = "PK" if c["pk"] else ("FK" if c["fk"] else ("UQ" if c["unique"] else ""))
        if c["pk"] and c["fk"]:
            key = "PF"
        lines.append(f"{key:<3}{c['name']:<{nw}}  {SHORT.get(c['type'], c['type'])}{'?' if c['null'] else ''}")
    body = "\n".join(lines)
    bw, bh = measure(body, 14, MONO)
    size[name] = (body, max(bw, measure(name, 18, MONO)[0]) + 32, 44 + bh + 20)
col_w = [max(size[n][1] for n in column) for column in LAYOUT]

# Plan every link: which gutters it uses, which side of each table it touches.
plans = []
gutter_lanes = {}
corridor = 0
for child, col, parent in FKS:
    info = next(k for k in TABLES[child]["cols"] if k["name"] == col)
    child_one = info["unique"] or TABLES[child]["solo_pk"] == {col}
    (ck, ci), (pk_, pi) = where[child], where[parent]
    plan = {"child": child, "parent": parent, "col": col, "null": info["null"],
            "heads": ("crowfoot_one" if child_one else "crowfoot_many", "crowfoot_one")}
    if child == parent:
        plan["kind"] = "self"
        plan["gutters"] = [ck + 1]
        plan["sides"] = ("right", "right")
    elif ck == pk_ and abs(ci - pi) == 1:
        plan["kind"] = "stack"
        plan["sides"] = ("top", "bottom") if pi < ci else ("bottom", "top")
        plan["gutters"] = []
    elif ck == pk_:
        plan["kind"] = "side"
        plan["gutters"] = [ck]
        plan["sides"] = ("left", "left")
    else:
        right = pk_ > ck
        gc = ck + 1 if right else ck
        gp = pk_ if right else pk_ + 1
        plan["sides"] = ("right", "left") if right else ("left", "right")
        if gc == gp:
            plan["kind"] = "near"
            plan["gutters"] = [gc]
        else:
            plan["kind"] = "far"
            plan["gutters"] = [gc, gp]
            plan["corridor"] = corridor
            corridor += 1
    plan["lanes"] = []
    for g in plan["gutters"]:
        plan["lanes"].append(gutter_lanes.get(g, 0))
        gutter_lanes[g] = gutter_lanes.get(g, 0) + 1
    plans.append(plan)

LANE = 16
gutter_w = {g: max(70, 40 + n * LANE) for g, n in gutter_lanes.items()}
for g in range(len(LAYOUT) + 1):
    gutter_w.setdefault(g, 70)

col_x = []
x = gutter_w[0]
for k in range(len(LAYOUT)):
    col_x.append(x)
    x += col_w[k] + gutter_w[k + 1]
gutter_x = {g: (col_x[g] - gutter_w[g] if g < len(LAYOUT) else col_x[-1] + col_w[-1]) for g in gutter_w}

TABLES_TOP = ERD_TOP + 170 + corridor * LANE

entity = {}
for k, column in enumerate(LAYOUT):
    y = TABLES_TOP
    for name in column:
        body, _, h = size[name]
        xx, w = col_x[k], col_w[k]
        group = f"t_{name}"
        frame_el = base("rectangle", xx, y, w, h, backgroundColor="#ffffff", roundness={"type": 3},
                        groupIds=[group], roughness=0)
        base("rectangle", xx, y, w, 40, backgroundColor=AREA.get(name, "#e9ecef"), roundness={"type": 3},
             groupIds=[group], roughness=0, boundElements=None)
        text(xx + 16, y + 9, name, 18, family=MONO, group=group)
        text(xx + 16, y + 52, body, 14, family=MONO, group=group)
        entity[name] = frame_el
        y += h + 70


def lane_x(g, lane):
    # Lanes fill a gutter from its middle outwards, so the first lines sit
    # centred and later ones keep clear of both columns.
    mid = gutter_x[g] + gutter_w[g] / 2
    n = gutter_lanes.get(g, 1)
    return mid + (lane - (n - 1) / 2) * LANE


def centre(e):
    return e["x"] + e["width"] / 2, e["y"] + e["height"] / 2


# Where on its side each link touches a table: spread out, ordered by where
# the other end is, so lines leaving one side do not cross each other there.
touches = {}
for i, p in enumerate(plans):
    touches.setdefault((p["child"], p["sides"][0]), []).append((i, 0))
    touches.setdefault((p["parent"], p["sides"][1]), []).append((i, 1))
anchor = {}
for (name, side), uses in touches.items():
    e = entity[name]
    def other(u):
        p = plans[u[0]]
        o = entity[p["parent"] if u[1] == 0 else p["child"]]
        return centre(o)[1 if side in ("left", "right") else 0] + u[1] * 0.1
    uses.sort(key=other)
    n = len(uses)
    for j, u in enumerate(uses):
        if side in ("left", "right"):
            span = e["height"] - 56
            pos = e["y"] + 48 + span * (j + 1) / (n + 1)
            anchor[u] = (e["x"] - 2 if side == "left" else e["x"] + e["width"] + 2, pos)
        else:
            pos = e["x"] + e["width"] * (j + 1) / (n + 1)
            anchor[u] = (pos, e["y"] - 2 if side == "top" else e["y"] + e["height"] + 2)

for i, p in enumerate(plans):
    a, b = anchor[(i, 0)], anchor[(i, 1)]
    style = dict(heads=p["heads"], start=entity[p["child"]], end=entity[p["parent"]], dashed=p["null"],
                 width=1, color=GREY if p["null"] else "#1e1e1e")
    if p["kind"] == "stack":
        pts = [a, b] if abs(a[0] - b[0]) < 1 else [a, (a[0], (a[1] + b[1]) / 2), (b[0], (a[1] + b[1]) / 2), b]
        arrow(pts, None, **style)
    elif p["kind"] == "self":
        lx = lane_x(p["gutters"][0], p["lanes"][0])
        a = (a[0], a[1] - 14)
        b = (b[0], b[1] + 14)
        arrow([a, (lx, a[1]), (lx, b[1]), b], p["col"], **style)
    elif p["kind"] in ("side", "near"):
        lx = lane_x(p["gutters"][0], p["lanes"][0])
        arrow([a, (lx, a[1]), (lx, b[1]), b], None, **style)
    else:
        lc = lane_x(p["gutters"][0], p["lanes"][0])
        lp = lane_x(p["gutters"][1], p["lanes"][1])
        cy = TABLES_TOP - 30 - p["corridor"] * LANE
        arrow([a, (lc, a[1]), (lc, cy), (lp, cy), (lp, b[1]), b], None, **style)

ERD_BOTTOM = max(e["y"] + e["height"] for e in entity.values())

# --- part 4: services, from the Go packages ---------------------------------

SECTIONS.append(len(els))


def go_files(path):
    return sorted(os.path.join(path, f) for f in os.listdir(path)
                  if f.endswith(".go") and not f.endswith("_test.go"))


def doc_of(path, word):
    """The first sentence of the package or command comment."""
    for f in go_files(path):
        src = open(f).read()
        m = re.search(r"((?://[^\n]*\n)+)package ", src)
        if m and re.match(rf"// {word} \w+", m.group(1)):
            # The first paragraph only: a blank comment line ends it.
            para = []
            for l in m.group(1).splitlines():
                if not l[2:].strip():
                    break
                para.append(l[2:].strip())
            first = re.split(r"(?<=[.:])\s", " ".join(para), maxsplit=1)[0]
            return first.rstrip(":") + ("" if first.endswith(".") else ".")
    return "No package comment yet."


def touches_of(path):
    src = "".join(open(f).read() for f in go_files(path))
    tables = sorted({t for t in re.findall(r"\b(?:FROM|JOIN|INTO|UPDATE)\s+(\w+)", src) if t in TABLES})
    uses = sorted(set(re.findall(r'"shellhaki/envi/internal/(\w+)"', src)))
    outside = []
    if "pgx" in src:
        outside.append("Postgres")
    if "go-redis" in src:
        outside.append("Redis")
    if "api.resend.com" in src:
        outside.append("Resend")
    if "bachs.io" in src:
        outside.append("Bachs")
    return tables, uses, outside


def wrap(s, width=44):
    import textwrap
    return "\n".join(textwrap.wrap(s, width))


SERVICE_TOP = 0
text(0, SERVICE_TOP, "Services · cmd/ and internal/", 36)
text(0, SERVICE_TOP + 56, "What each program and package does, the tables it\n"
     "queries, the packages it calls and the outside systems it talks to.", 16, GREY, MONO)

entries = []
for name in sorted(os.listdir(os.path.join(ROOT, "cmd"))):
    path = os.path.join(ROOT, "cmd", name)
    entries.append(("program", f"cmd/{name}", doc_of(path, "Command"), *touches_of(path)))
for name in sorted(os.listdir(os.path.join(ROOT, "internal"))):
    path = os.path.join(ROOT, "internal", name)
    if name == "e2e" or not os.path.isdir(path) or not go_files(path):
        continue
    entries.append(("package", name, doc_of(path, "Package"), *touches_of(path)))
SERVICE_COUNT = len(entries)

KIND_COLOUR = {"program": YELLOW, "package": "#e7f5ff"}
cards = []
for kind, name, doc, tables, uses, outside in entries:
    lines = [wrap(doc)]
    if tables:
        lines.append(wrap("tables: " + ", ".join(tables)))
    if uses:
        lines.append(wrap("uses: " + ", ".join(uses)))
    if outside:
        lines.append("talks to: " + ", ".join(outside))
    body = "\n".join(lines)
    w = 44 * 13 * 0.6 + 40
    h = 20 + 27 + 8 + measure(body, 13, MONO)[1] + 20
    cards.append((kind, name, body, w, h))

heights4 = [0, 0, 0]
for kind, name, body, w, h in cards:
    k = heights4.index(min(heights4))
    x, y = k * (w + 30), SERVICE_TOP + 140 + heights4[k]
    heights4[k] += h + 30
    group = f"svc_{name}"
    base("rectangle", x, y, w, h, backgroundColor=KIND_COLOUR[kind], roundness={"type": 3},
         groupIds=[group], roughness=0)
    text(x + 20, y + 18, name, 20, group=group)
    text(x + 20, y + 18 + 27 + 8, body, 13, family=MONO, group=group)


# --- side by side, inside one frame -----------------------------------------

def bounds(group):
    xs, ys = [], []
    for e in group:
        if e["type"] in ("arrow", "line"):
            for px, py in e["points"]:
                xs.append(e["x"] + px)
                ys.append(e["y"] + py)
        else:
            xs += [e["x"], e["x"] + e["width"]]
            ys += [e["y"], e["y"] + e["height"]]
    return min(xs), min(ys), max(xs), max(ys)


# Drawn order is architecture, endpoints, data model, services; shown left to
# right as architecture, services, data model, endpoints, which reads from
# the outside in.
SECTIONS.append(len(els))
parts = [els[a:b] for a, b in zip(SECTIONS, SECTIONS[1:])]
order = [parts[0], parts[3], parts[2], parts[1]]
GAP = 240
cursor = 0
spans = []
for group in order:
    x0, y0, x1, y1 = bounds(group)
    for e in group:
        e["x"] += cursor - x0
        e["y"] -= y0
    spans.append((cursor, cursor + x1 - x0, y1 - y0))
    cursor += x1 - x0 + GAP

PAD = 140
height = max(h for _, _, h in spans)
right = spans[-1][1]
outer = []


def line(points, **kw):
    x0, y0 = points[0]
    rel = [[px - x0, py - y0] for px, py in points]
    e = dict(id=f"lin{len(els) + len(outer)}", type="line", x=x0, y=y0,
             width=max(p[0] for p in rel) - min(p[0] for p in rel),
             height=max(p[1] for p in rel) - min(p[1] for p in rel), angle=0,
             strokeColor="#adb5bd", backgroundColor="transparent", fillStyle="solid", strokeWidth=2,
             strokeStyle="dashed", roughness=0, opacity=100, groupIds=[], frameId=None, roundness=None,
             seed=seed(), version=1, versionNonce=seed(), isDeleted=False, boundElements=None, updated=1,
             link=None, locked=False, points=rel, lastCommittedPoint=None, startBinding=None,
             endBinding=None, startArrowhead=None, endArrowhead=None, polygon=False)
    e.update(kw)
    outer.append(e)


els_before = len(els)
frame_box = base("rectangle", -PAD, -PAD - 120, right + 2 * PAD, height + 2 * PAD + 120,
                 backgroundColor="#ffffff", roundness={"type": 3}, roughness=0, strokeWidth=4)
title = text(-PAD + 60, -PAD - 70, "Envi · system map · generated by make diagram", 44)
for (_, end, _), (start, _, _) in zip(spans, spans[1:]):
    mid = (end + start) / 2
    line([(mid, -40), (mid, height + 40)])
# The frame goes underneath everything else.
els[:] = [frame_box] + outer + els[:els_before] + [title]

out = {"type": "excalidraw", "version": 2, "source": "https://excalidraw.com",
       "elements": els, "appState": {"viewBackgroundColor": "#ffffff", "gridSize": None}, "files": {}}
with open(os.path.join(ROOT, "architecture", "excalidraw.json"), "w") as f:
    json.dump(out, f, indent=1, ensure_ascii=False)
    f.write("\n")
print(f"excalidraw.json: {TOTAL} endpoints, {len(TABLES)} tables, {len(FKS)} relationships, {SERVICE_COUNT} services, {len(els)} elements")
