import urllib.request, json

KEY = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJmNGUzNDdmNy0xNmVhLTRkMzQtYmZmNS0zOWIyM2U4YTBjN2EiLCJpc3MiOiJuNG4iLCJhdWQiOiJwdWJsaWMtYXBpIiwianRpIjoiMmRkYjU2Y2YtZjI0NC00Njk1LTljMTgtODBlNGQ4NjAyYjQ1IiwiaWF0IjoxNzgzNzk2Nzc0fQ.bqrYS0qG6OTlqbZDNnUpdDW8TbdWB91ZJEesdYz2jhE"
BASE = "http://localhost:5678/api/v1"

def get(path):
    headers = dict()
    headers["X-N8N-API-KEY"] = KEY
    headers["accept"] = "application/json"
    req = urllib.request.Request(BASE + path, headers=headers)
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.load(r)

d = get("/workflows?limit=100")
with open(".n8n_inspect/workflows_list.json", "w") as f:
    json.dump(d, f, indent=2)

for w in d.get("data", []):
    print("id=%s  active=%s  name=%s" % (w["id"], w["active"], w["name"]))
